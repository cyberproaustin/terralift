package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/naming"
	"github.com/cyberproaustin/terralift/internal/provider"
)

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

	var containers []provider.ContainerExport
	for _, rg := range sortedContainers(inv) {
		ce, err := exportRG(ctx, run, inv, rg)
		if err != nil {
			run.Log.Warn("Export", "resource group %q: %v (skipping)", rg, err)
			continue
		}
		if ce != nil {
			containers = append(containers, *ce)
		}
	}
	if len(containers) == 0 {
		return nil, errors.New("export produced no resource groups")
	}
	return &provider.ExportResult{Mode: mode, Containers: containers}, nil
}

func exportRG(ctx context.Context, run *core.Run, inv *model.Inventory, rg string) (*provider.ContainerExport, error) {
	san := naming.Sanitize(rg)
	dir := filepath.Join(run.Paths.Export, san)
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
		run.Log.Verbose("Export", "%s", tail(out, 20))
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
		if reason := excludedReason(e.ResourceType); reason != "" {
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
	if out, err := runAztfexport(ctx, dir, args...); err != nil {
		// Non-fatal: -k means partial success is expected; parse whatever HCL landed.
		run.Log.Warn("Export", "%s: aztfexport map reported errors (continuing): %v", rg, err)
		run.Log.Verbose("Export", "%s", tail(out, 20))
	}

	// 5. reconcile: an address present in the generated HCL was captured; a kept
	// resource whose address is absent (skipped on read error) is a coverage gap.
	// AddressByID is built from MAPPED resources only, so reference rewire never
	// points a literal at a gapped resource that has no block (broken HCL).
	generated := scanResourceAddrs(dir)
	var mapped, gaps []string
	addrByID := make(map[string]string, len(kept)*2)
	for i, k := range kept {
		if !generated[keptAddr[i]] {
			gaps = append(gaps, k)
			continue
		}
		mapped = append(mapped, k)
		addrByID[k] = keptAddr[i] // the Azure resource id as it appears in generated HCL
		// The import id may differ from the key (property sub-resources alias a
		// parent); map it too, but never clobber a primary resource's own address.
		if rid := m[k].ResourceID; rid != k {
			if _, taken := addrByID[rid]; !taken {
				addrByID[rid] = keptAddr[i]
			}
		}
	}
	run.Log.Info("Export", "%s: %d mapped, %d excluded, %d gap -> %s", rg, len(mapped), len(excluded), len(gaps), dir)

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
		AddressByID: addrByID, ConfigFiles: []string{"main.tf"}, Renames: len(kept),
	}, nil
}

// excludedReason returns a non-empty reason when a resource type must NOT be
// onboarded: Key Vault secret material and storage data-plane content. This is a
// SECURITY control for the control-plane-only mandate (aztfexport would otherwise
// read secret values / require data-plane keys), reported as ExcludedIDs (an
// intentional skip) rather than a coverage gap.
func excludedReason(tfType string) string {
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

var resourceLabel = regexp.MustCompile(`(?m)^resource\s+"([^"]+)"\s+"([^"]+)"`)

// scanResourceAddrs returns the set of "type.name" addresses declared across all
// .tf files in dir (aztfexport's generated HCL).
func scanResourceAddrs(dir string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, m := range resourceLabel.FindAllStringSubmatch(string(data), -1) {
			out[m[1]+"."+m[2]] = true
		}
	}
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
//
// TF_PLUGIN_CACHE_DIR is stripped from the child environment: aztfexport runs a
// terraform init then per-resource `terraform import`, and a shared plugin cache
// leaves the working dir's lock file inconsistent with the cached provider,
// which makes every import fail with "Required plugins are not installed".
func runAztfexport(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, aztfexportBin(), args...)
	cmd.Dir = dir
	cmd.Env = withoutEnv(os.Environ(), "TF_PLUGIN_CACHE_DIR")
	out, err := cmd.CombinedOutput()
	return string(out), err
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

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}
