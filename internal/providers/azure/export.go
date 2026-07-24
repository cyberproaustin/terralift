package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/hcl"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/naming"
	"github.com/cyberproaustin/terralift/internal/provider"
	"github.com/cyberproaustin/terralift/internal/util"
)

// azureSecretAttrs are attribute names that are UNAMBIGUOUSLY a single leaked
// secret value and are computed/optional — safe to drop entirely (their live
// value stays unmanaged rather than being overwritten). App configuration
// (app_settings, connection_string blocks, env vars) is NOT here: it ships and is
// flagged in reports/secrets-review.md instead.
var azureSecretAttrs = []string{
	"primary_access_key", "secondary_access_key",
	"primary_connection_string", "secondary_connection_string",
	"primary_blob_connection_string", "secondary_blob_connection_string",
	"primary_key", "secondary_key",
	"sas_token", "certificate_password",
}

// azureRedactRules blank required-settable secret attributes to "" and protect
// them with ignore_changes, so a later apply won't overwrite the real value with
// the blank (removing a required attribute would instead break `terraform plan`).
var azureRedactRules = []hcl.Rule{
	{Type: "azurerm_mssql_server", Attr: "administrator_login_password", Kind: hcl.Scalar},
	{Type: "azurerm_postgresql_server", Attr: "administrator_login_password", Kind: hcl.Scalar},
	{Type: "azurerm_postgresql_flexible_server", Attr: "administrator_password", Kind: hcl.Scalar},
	{Type: "azurerm_mysql_server", Attr: "administrator_login_password", Kind: hcl.Scalar},
	{Type: "azurerm_mysql_flexible_server", Attr: "administrator_password", Kind: hcl.Scalar},
}

// redactGeneratedHCL removes leaked single secrets from aztfexport's main.tf and
// returns the scrubbed-secret list for the operator-facing redactions report.
func redactGeneratedHCL(dir string) []hcl.Redaction {
	p := filepath.Join(dir, "main.tf")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	out, events := hcl.Redact(string(data), azureSecretAttrs, azureRedactRules)
	if len(events) > 0 {
		_ = os.WriteFile(p, []byte(out), 0o644)
	}
	return events
}

// mappingEntry is one row of aztfexport's aztfexportResourceMapping.json:
// the key is the Azure resource id; resource_id is the import id (may alias a
// parent for property sub-resources); resource_name is the TF label we rewrite.
type mappingEntry struct {
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
	ResourceName string `json:"resource_name"`
}

// export drives aztfexport per resource group with born-correct naming:
//  1. `aztfexport rg -g` generates the resource mapping WITHOUT importing (a
//     read-only Azure listing that also discovers child/property resources our
//     Resource Graph floor doesn't project),
//  2. classify each entry: exclude data-plane secret material / storage content
//     (control-plane-only mandate), keep the rest,
//  3. rewrite resource_name to a born-correct, de-collided label — WE control the
//     address before any state exists,
//  4. `aztfexport map -k` imports/authors HCL from the rewritten mapping,
//     continuing past resources the caller can't read (brownfield degradation),
//  5. reconcile generated HCL against the kept set to separate mapped vs gap.
func export(ctx context.Context, run *core.Run, inv *model.Inventory) (*provider.ExportResult, error) {
	if _, err := exec.LookPath(aztfexportBin()); err != nil {
		return nil, fmt.Errorf("aztfexport not found on PATH: %w", err)
	}
	mode := "import"
	if run.Config.HCLOnly {
		mode = "hcl-only"
	}

	rgs := sortedContainers(inv)
	results := make([]*provider.ContainerExport, len(rgs))
	errs := make([]error, len(rgs))
	doRG := func(i int) { results[i], errs[i] = exportRG(ctx, run, inv, rgs[i]) }

	// Export groups with a bounded worker pool (default: serial — see exportParallelism).
	// There is deliberately NO shared provider plugin cache: aztfexport runs terraform
	// init in ~parallelism worker dirs concurrently, and a shared TF_PLUGIN_CACHE_DIR is
	// not concurrency-safe — on Windows the racing installs truncate the provider binary,
	// which then crashes every import. Each aztfexport invocation manages its own provider
	// (proven correct by running aztfexport standalone); exportRG is parallel-safe because
	// each writes only its own dir.
	if len(rgs) > 0 {
		sem := make(chan struct{}, exportParallelism())
		var wg sync.WaitGroup
		for i := range rgs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				doRG(i)
			}(i)
		}
		wg.Wait()
	}

	// Assemble in the original sorted order so the export is deterministic regardless
	// of completion order.
	var containers []provider.ContainerExport
	for i, rg := range rgs {
		if errs[i] != nil {
			run.Log.Warn("Export", "resource group %q: %v (skipping)", rg, errs[i])
			continue
		}
		if results[i] != nil {
			containers = append(containers, *results[i])
		}
	}
	if len(containers) == 0 {
		return nil, errors.New("export produced no resource groups")
	}
	return &provider.ExportResult{Mode: mode, Containers: containers}, nil
}

// exportParallelism bounds how many resource groups export concurrently. It defaults
// to 1 (serial): with no shared provider cache, each concurrent group's aztfexport
// downloads its own copy of the (very large) azurerm provider across ~10 worker dirs,
// so running several groups at once multiplies concurrent-download pressure for no
// benefit. Serial matches the standalone-aztfexport behavior we verified as correct.
// TERRALIFT_EXPORT_PARALLELISM raises it for operators who want the wall-clock trade.
func exportParallelism() int {
	if v := strings.TrimSpace(os.Getenv("TERRALIFT_EXPORT_PARALLELISM")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

func exportRG(ctx context.Context, run *core.Run, inv *model.Inventory, rg string) (*provider.ContainerExport, error) {
	san := naming.Sanitize(rg)
	dir := filepath.Join(run.Paths.Export, san)
	// Absolutize before handing to aztfexport: we set the child's working dir to `dir` AND pass it
	// as `-o dir`. If `dir` is relative (the default artifact root is `artifacts/…`), aztfexport
	// re-roots the relative `-o` against its own cwd (== dir) and writes the mapping to a NESTED
	// dir/dir/… path, which we then can't read back ("cannot find the file"). An absolute path is
	// resolved the same regardless of cwd.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	mapPath := filepath.Join(dir, "aztfexportResourceMapping.json")

	// 1. generate the mapping (read-only against Azure). This also installs the
	// azurerm provider into dir/.terraform, which the `map` step below reuses —
	// so provider download happens once per group.
	if out, err := runAztfexport(ctx, dir, "rg", "-g", "-n", "--plain-ui", "-o", dir, rg); err != nil {
		if strings.Contains(out, "no resource found") {
			return nil, nil // benign: RG has no aztfexport-importable resources
		}
		run.Log.Verbose("Export", "%s", hcl.Tail(out, 20))
		return nil, fmt.Errorf("generate mapping: %w", err)
	}
	raw, err := os.ReadFile(mapPath)
	if err != nil {
		return nil, fmt.Errorf("read mapping: %w", err)
	}
	var m map[string]mappingEntry
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse mapping: %w", err)
	}
	if len(m) == 0 {
		return nil, nil // empty RG
	}

	// 2+3. classify and born-correct-rewrite in deterministic key order.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var kept []string
	var addrsIn []naming.Address
	var excluded []string
	for _, k := range keys {
		e := m[k]
		if reason := excludedReason(e.ResourceType, k); reason != "" {
			excluded = append(excluded, k)
			delete(m, k) // aztfexport map only touches what remains in the mapping
			continue
		}
		kept = append(kept, k)
		addrsIn = append(addrsIn, naming.Address{Type: e.ResourceType, Base: bornName(k)})
	}
	if len(kept) == 0 {
		run.Log.Info("Export", "%s: all %d resource(s) excluded (data-plane)", rg, len(excluded))
		return &provider.ContainerExport{Container: rg, Dir: dir, ExcludedIDs: excluded}, nil
	}
	names := naming.Dedupe(addrsIn)

	keptAddr := make([]string, len(kept))
	for i, k := range kept {
		e := m[k]
		e.ResourceName = names[i]
		m[k] = e
		keptAddr[i] = e.ResourceType + "." + names[i]
	}
	// Overwrite the in-dir mapping with the born-correct one; `map` reads the file
	// we pass. -f lets `map` proceed in the non-empty dir that `rg -g` scaffolded.
	if err := writeMapping(mapPath, m); err != nil {
		return nil, fmt.Errorf("write rewritten mapping: %w", err)
	}

	// 4. author HCL from the born-correct mapping, reusing the provider install
	// from step 1. -k continues past resources the principal can't read (e.g.
	// Storage listKeys), leaving them out of the HCL rather than aborting.
	args := []string{"map", "-n", "--plain-ui", "-k", "-f", "-o", dir}
	if run.Config.HCLOnly {
		args = append(args, "--hcl-only")
	}
	args = append(args, mapPath)
	mapOut, mapErr := runAztfexport(ctx, dir, args...)
	if mapErr != nil {
		// Non-fatal: -k means partial success is expected; parse whatever HCL landed.
		run.Log.Warn("Export", "%s: aztfexport map reported errors (continuing): %v", rg, mapErr)
		run.Log.Verbose("Export", "%s", hcl.Tail(mapOut, 20))
	}

	// 4b. scrub leaked single secrets from main.tf (app config is left intact — it
	// ships and is flagged in the secrets review instead).
	redactions := redactGeneratedHCL(dir)
	if len(redactions) > 0 {
		run.Log.Info("Export", "%s: scrubbed %d leaked secret value(s) from main.tf", rg, len(redactions))
	}

	// The addresses actually authored in the generated HCL. Computed here (before the
	// import blocks) so import.tf can be intersected against it: aztfexport runs with
	// -k (tolerate partial failures), so a resource could land in state but not in the
	// HCL — an import block for it would fail plan with "import target does not exist".
	generated := hcl.ScanAddrsDir(dir)

	// 4c. born-correct import blocks from aztfexport's state. aztfexport adopts by
	// importing into a state file, which TerraLift never ships (it holds data-plane
	// secrets), so without this the shipped repo would CREATE rather than adopt. The
	// import blocks make `terraform plan` on the repo a clean adoption, matching the
	// AWS/GCP model. IDs stay literal — import.tf is excluded from reference rewiring.
	if n, err := writeImportBlocksFromState(dir, generated); err != nil {
		run.Log.Warn("Export", "%s: import-block generation failed: %v", rg, err)
	} else if n > 0 {
		run.Log.Info("Export", "%s: %d born-correct import block(s) -> import.tf", rg, n)
	}

	// 5. reconcile: an address present in the generated HCL was captured; a kept
	// resource whose address is absent (skipped on read error) is a coverage gap.
	// AddressByID is built from MAPPED resources only, so reference rewire never
	// points a literal at a gapped resource that has no block (broken HCL).
	var mapped, gaps []string
	// addrByID (bare "azurerm_x.y") is for RBAC scope resolution, which appends its own
	// ".id". refByID (full reference expression) is for cross-reference rewiring, whose
	// contract is the complete expression — Azure cross-refs are all resource ids -> .id.
	addrByID := make(map[string]string, len(kept)*2)
	refByID := make(map[string]string, len(kept)*2)
	for i, k := range kept {
		if !generated[keptAddr[i]] {
			gaps = append(gaps, k)
			continue
		}
		mapped = append(mapped, k)
		addrByID[k] = keptAddr[i]        // the Azure resource id as it appears in generated HCL
		refByID[k] = keptAddr[i] + ".id" // full reference for Rewire
		// The import id may differ from the key (property sub-resources alias a
		// parent); map it too, but never clobber a primary resource's own address.
		if rid := m[k].ResourceID; rid != k {
			if _, taken := addrByID[rid]; !taken {
				addrByID[rid] = keptAddr[i]
				refByID[rid] = keptAddr[i] + ".id"
			}
		}
	}
	run.Log.Info("Export", "%s: %d mapped, %d excluded, %d gap -> %s", rg, len(mapped), len(excluded), len(gaps), dir)

	// When a group has gaps, persist aztfexport's full `map` output so the operator can
	// see EXACTLY why each resource was dropped — a permission 403, an import the azurerm
	// provider could not perform, or a transient ARM throttle — instead of guessing from
	// the coverage report. It stays in the per-group export dir (never shipped: the
	// packager only copies .tf/.zip, and this can echo resource detail), alongside the
	// terraform state aztfexport already writes there. 0600 for the same reason.
	if len(gaps) > 0 && strings.TrimSpace(mapOut) != "" {
		logPath := filepath.Join(dir, "aztfexport-map.log")
		if err := os.WriteFile(logPath, []byte(mapOut), 0o600); err == nil {
			run.Log.Info("Export", "%s: %d gap(s) — aztfexport's per-resource reason is in %s", rg, len(gaps), logPath)
		}
	}

	// Replace aztfexport's local-dev provider.tf with a clean, pipeline-ready one
	// (OIDC-capable via env; no use_cli/use_msi pinning that would block CI).
	writeProviderTF(dir, run.Scope.ID)

	// Author Azure RBAC scoped to this group as azurerm_role_assignment resources
	// (+ import blocks), so access control is captured as code alongside the infra.
	// Best-effort: a write failure must not discard the successful infra export.
	if hcl, n := generateRoleAssignments(inv, rg, addrByID); n > 0 {
		if err := os.WriteFile(filepath.Join(dir, "roleassignments.tf"), []byte(hcl), 0o644); err != nil {
			run.Log.Warn("Export", "%s: write roleassignments.tf: %v", rg, err)
		} else {
			run.Log.Info("Export", "%s: %d role assignment(s) -> roleassignments.tf", rg, n)
		}
	}

	return &provider.ContainerExport{
		Container: rg, Dir: dir,
		MappedIDs: mapped, ExcludedIDs: excluded, GapIDs: gaps,
		AddressByID: refByID, ConfigFiles: []string{"main.tf"},
		Redactions: redactions,
	}, nil
}

// writeImportBlocksFromState reads the aztfexport-produced terraform.tfstate and
// emits one born-correct import block per managed resource instance into import.tf,
// so the shipped repo adopts on `terraform plan` (aztfexport's state is never shipped).
// Only addresses present in `generated` (the authored HCL) get a block — a state
// address with no resource block would fail plan with "import target does not exist"
// (possible under aztfexport's -k partial-failure mode). Returns the count written.
// import.tf is excluded from rewiring, so its ids stay literal.
func writeImportBlocksFromState(dir string, generated map[string]bool) (int, error) {
	data, err := os.ReadFile(filepath.Join(dir, "terraform.tfstate"))
	if err != nil {
		return 0, nil // no state (e.g. aztfexport produced nothing) — not an error
	}
	var st struct {
		Resources []struct {
			Mode      string `json:"mode"`
			Type      string `json:"type"`
			Name      string `json:"name"`
			Instances []struct {
				IndexKey   any            `json:"index_key"`
				Attributes map[string]any `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return 0, err
	}
	var b strings.Builder
	b.WriteString("# Generated by TerraLift — born-correct import blocks (from aztfexport state).\n")
	b.WriteString("# `terraform plan` adopts these existing resources; delete import.tf once adopted.\n\n")
	n := 0
	for _, r := range st.Resources {
		if r.Mode != "managed" {
			continue
		}
		for _, inst := range r.Instances {
			id, _ := inst.Attributes["id"].(string)
			if id == "" {
				continue
			}
			addr := r.Type + "." + r.Name
			if generated != nil && !generated[addr] {
				continue // no resource block authored for this address — don't orphan an import
			}
			to := addr
			if inst.IndexKey != nil {
				switch k := inst.IndexKey.(type) {
				case string:
					to = fmt.Sprintf("%s[%q]", to, k)
				case float64:
					to = fmt.Sprintf("%s[%d]", to, int(k))
				}
			}
			b.WriteString(hcl.ImportBlock(to, util.EscapeHCLTemplate(id)))
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, os.WriteFile(filepath.Join(dir, "import.tf"), []byte(b.String()), 0o644)
}

// excludedReason returns a non-empty reason when a resource type must NOT be
// onboarded: Key Vault secret material and storage data-plane content. This is a
// SECURITY control for the control-plane-only mandate (aztfexport would otherwise
// read secret values / require data-plane keys), reported as ExcludedIDs (an
// intentional skip) rather than a coverage gap.
func excludedReason(tfType, resourceID string) string {
	// The "$Default" consumer group is auto-created with every Event Hub and can't
	// be created (its name is reserved), so importing it produces HCL that fails
	// validation. User consumer groups (any other name) are kept.
	if tfType == "azurerm_eventhub_consumer_group" &&
		strings.Contains(strings.ToLower(resourceID), "/consumergroups/$default") {
		return "Azure built-in $Default consumer group"
	}
	// `master` is the SQL Server system database: it exists on every server, is not
	// user-created and cannot be managed as an azurerm_mssql_database. Excluding it
	// keeps it out of the coverage GAP list, where it was being reported as an
	// unsupported type it never was.
	if tfType == "azurerm_mssql_database" &&
		strings.HasSuffix(strings.ToLower(resourceID), "/databases/master") {
		return "Azure built-in master system database"
	}
	switch tfType {
	case "azurerm_key_vault_secret",
		"azurerm_key_vault_key",
		"azurerm_key_vault_certificate",
		"azurerm_key_vault_certificate_data",
		"azurerm_key_vault_managed_storage_account_sas_token_definition":
		return "data-plane: key vault secret material"
	case "azurerm_storage_blob",
		"azurerm_storage_container",
		"azurerm_storage_share",
		"azurerm_storage_share_file",
		"azurerm_storage_share_directory",
		"azurerm_storage_queue",
		"azurerm_storage_table",
		"azurerm_storage_table_entity",
		"azurerm_storage_data_lake_gen2_filesystem",
		"azurerm_storage_data_lake_gen2_path":
		return "data-plane: storage content"
	case "azurerm_role_assignment", "azurerm_role_definition":
		// RBAC is authored authoritatively by generateRoleAssignments -> roleassignments.tf
		// (with its own import blocks). Onboarding it here too would double-manage the same
		// ARM id at two addresses and fail plan. Keep iam.go the single source of truth.
		return "RBAC (authored in roleassignments.tf)"
	case // Azure-provisioned built-in child resources aztfexport over-discovers
		// (hundreds ship with the parent) — never user onboarding targets.
		"azurerm_automation_module",
		"azurerm_automation_powershell72_module",
		"azurerm_automation_connection_type",
		"azurerm_automation_runtime_environment",
		"azurerm_log_analytics_workspace_table_custom_log",
		"azurerm_log_analytics_saved_search",
		"azurerm_container_registry_scope_map":
		return "Azure-managed built-in child resource"
	}
	return ""
}

// writeProviderTF overwrites aztfexport's local-dev provider.tf with a clean,
// pipeline-ready block. subscription_id is an identifier (not a secret) kept for
// azurerm v4 (override via ARM_SUBSCRIPTION_ID); dropping use_cli/use_msi/
// use_oidc lets auth flow from the environment so OIDC (ARM_USE_OIDC + Workload
// Identity Federation) works in CI. required_providers stays in terraform.tf.
func writeProviderTF(dir, subscriptionID string) {
	tf := fmt.Sprintf(`provider "azurerm" {
  features {}

  # Auth flows from the environment: ARM_USE_OIDC=true + Workload Identity
  # Federation in CI, or the az CLI locally. Override the subscription in CI
  # with ARM_SUBSCRIPTION_ID.
  subscription_id                 = %q
  resource_provider_registrations = "none"
}
`, subscriptionID)
	_ = os.WriteFile(filepath.Join(dir, "provider.tf"), []byte(tf), 0o644)
}

// bornName derives a born-correct TF label from the last segment of an Azure
// resource id (its own name), sanitized; de-collision is handled by naming.Dedupe.
func bornName(azureID string) string {
	seg := azureID
	if i := strings.LastIndex(seg, "/"); i >= 0 {
		seg = seg[i+1:]
	}
	return naming.Sanitize(seg)
}

// sortedContainers returns the distinct resource groups in the inventory, sorted.
func sortedContainers(inv *model.Inventory) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range inv.Resources {
		if r.Container != "" && !seen[r.Container] {
			seen[r.Container] = true
			out = append(out, r.Container)
		}
	}
	sort.Strings(out)
	return out
}

func writeMapping(path string, m map[string]mappingEntry) error {
	data, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func aztfexportBin() string {
	if runtime.GOOS == "windows" {
		return "aztfexport.exe"
	}
	return "aztfexport"
}

// runAztfexport runs aztfexport in dir and returns combined output. aztfexport
// emits progress on stderr, so capture both streams for diagnostics.
func runAztfexport(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, aztfexportBin(), args...)
	cmd.Dir = dir
	cmd.Env = aztfexportEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// aztfexportEnv builds the child environment for aztfexport. It STRIPS
// TF_PLUGIN_CACHE_DIR unconditionally: aztfexport runs `terraform init` in
// ~parallelism worker directories concurrently, and a shared plugin cache is not
// concurrency-safe — on Windows the racing installs truncate the provider binary,
// which then crashes every import ("%1 is not a valid Win32 application", plugin
// handshake failures, provider segfaults). Letting each aztfexport invocation manage
// its own provider is slower (it re-downloads) but correct — this is exactly how
// aztfexport behaves when run standalone, which we verified works.
func aztfexportEnv() []string {
	return withoutEnv(os.Environ(), "TF_PLUGIN_CACHE_DIR")
}

func withoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return out
}
