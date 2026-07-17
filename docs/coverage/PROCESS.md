# TerraLift native-resource coverage campaign — runbook

**Purpose.** Verify TerraLift handles *every native (first-party) resource type* on AWS, GCP,
and Azure before public release. "Native" = the cloud's own resources (Azure Firewall = yes,
a third-party Sophos appliance = no). This doc is the repeatable process; follow it exactly so
the work survives interruptions and stays token- and cost-efficient.

---

## 1. What "coverage" actually means (we do NOT create every resource)

Creating every type is impossible (thousands of types; many cost real money). Whether TerraLift
handles a type is decided by **three data facts**, none of which require creating the resource:

1. **Enumeration → TF type mapping.** The enumerator's native-type → Terraform-type map must
   include the type and map it correctly.
   - AWS: `internal/providers/aws/types.go` `awsTypeToTF` (Resource Explorer `service:resource` → `aws_*`).
   - GCP: `internal/providers/gcp/types.go` (CAI asset type `svc.googleapis.com/Kind` → `google_*`).
   - Azure: **export rides on aztfexport's own ARM→azurerm mapping** (comprehensive, not ours).
     Our `azureTypeToTF` is only for the coverage-classification display, so Azure is a
     *verification + classification* pass, not a from-scratch map build.
2. **Import-ID rule** per type is correct (verifiable against provider docs, no resource needed).
   - AWS: `internal/providers/aws/importid.go` (`deriveImportID` + `importIDOverride`).
   - GCP: `internal/providers/gcp/importid.go`.
   - Azure: aztfexport derives it.
3. **generate-config-out over-emit curation** for that type — only knowable by *touching* it.
   This is the ONLY part that needs live testing, so we cheap-spot-test representative + tricky
   types (see §6).

So the deliverable per cloud = a **verified coverage checklist** (every native enumerable type +
its TF mapping + import rule + status) → drives updates to the Go maps → then cheap live
spot-testing. Full map coverage + spot-tested tricky types = "we're confident it works."

---

## 2. Environments (cost-conscious; tear down immediately)

- **AWS**: account `521595302924` (IAM user `terralift`, us-east-1, RE LOCAL index). Free-tier VM = `t3.micro`.
- **GCP**: the throwaway lab was deleted — **recreate a fresh throwaway project** (see the terralift-gcp-lab memory for how; do NOT touch `bank-vault-academy` or `quick-rarity-*`). Cheapest VM = `e2-micro`.
- **Azure**: **`Eutaxia - Production` sub `81106197-4fec-452c-8cef-69328e602e8a`** (user-authorized). Cheapest VM = `B1s`/`B1ls`. NEVER touch Bank Vault Academy Production.

**Cost rules (non-negotiable):** definition-only free/near-free resources; cheapest compute SKUs
ONLY and only when a type must be tested live; `terraform plan` and eyeball the resource list
before any `apply`; tear down immediately after confirmation; `df -h /tmp` and clean scratchpad
between runs (the aws/google providers are ~600MB each — use `TF_PLUGIN_CACHE_DIR`, but kill
stray `terraform-provider-*` processes if you hit "text file busy").

---

## 3. The agent wave process (how the lists get built)

**Model:** bounded `sonnet` `general-purpose` agents, **one cloud at a time**, run in small waves
(≤2 concurrent). Each agent covers a fixed batch of service families and **writes its result
straight to a file** — that file IS the persistence, so a usage limit can never lose completed
batches. Verify each wave's files landed BEFORE launching the next wave. Never fan out dozens at once.

**Batches** (files land in `docs/coverage/<cloud>-<n>-<families>.md`):
- AWS-1 net/compute/storage · AWS-2 data/analytics · AWS-3 serverless/containers/integration · AWS-4 security/identity/ops/edge/other
- GCP-1 compute/network/storage · GCP-2 data/analytics/bigdata · GCP-3 serverless/containers/devops · GCP-4 security/iam/ops/ml/other
- AZURE-1 compute/network/storage · AZURE-2 data/analytics · AZURE-3 app/integration/containers · AZURE-4 security/identity/ops/ai/other

**Agent task template (keep it terse — this controls token cost):**
- Give ONE fixed scope: an explicit list of service families. No open-ended "everything."
- Output = ONLY a markdown table, columns: `| terraform type | native enumerator type | import ID format | notes (gotchas only) |`.
  - AWS native type = Resource Explorer `service:resource`; GCP = CAI `svc.googleapis.com/Kind`; Azure = ARM `Microsoft.X/y`.
  - Import ID = the exact provider-docs "Import" syntax; flag full-ARN, slashed-name, `bus/rule`, URL-reconstructed, composite.
  - Notes ONLY when there's a gotcha: slashed name, full-ARN import, sub-resource of a parent, not-enumerable-separately, no-import-support, AWS/cloud-managed default/singleton.
- Sources: the Terraform provider registry docs (per-resource Import section) + the cloud's own
  "enumerable resource types" doc (AWS RE supported types / GCP CAI supported asset types / Azure ARG tables).
- Agent WRITES the table to its assigned file (start with an `# <cloud> coverage: <families>` H1), then
  replies with ONLY: row count + a short list of types it was UNSURE about. No prose.

---

## 4. Per-wave checklist (do this every time)

1. Launch the wave's ≤2 agents (each with its file path).
2. On completion, **verify the file exists and looks sane**: `wc -l docs/coverage/<file>.md`, skim the header + a few rows, check the "unsure" list in the agent's reply.
3. If a file is missing/empty → relaunch just that batch (others are safe).
4. Record progress in the coverage README / this campaign's memory so a fresh session knows what's done.
5. Only then launch the next wave.

---

## 5. Aggregation (turn the tables into code)

After a cloud's 4 files exist:
1. Build the master checklist `docs/coverage/<cloud>-CHECKLIST.md`: merge the batch tables; add a
   **Status** column — `mapped` (in the Go map), `add` (to add), `excluded` (cloud-managed default),
   `no-import` (provider can't import — document as a known gap), `sub-resource` (handled via parent).
2. Update the Go maps from the `add` rows:
   - AWS: extend `awsTypeToTF`; add `importIDOverride` entries for any non-default import ID; extend
     `awsManagedDefault` for new cloud-managed singletons.
   - GCP: extend `gcpTypeToTF` + `importIDOverride`.
   - Azure: extend `azureTypeToTF` (classification only).
3. `gofmt` + `go build` + `go vet` + `go test ./...` after each cloud's map update.
4. Add/adjust unit tests for any new import-ID override with a non-trivial rule.

---

## 6. Live spot-testing (the only part that needs real resources)

Don't create everything. Create a **broad, cheap, representative sample + every "tricky" type**
(anything with a gotcha note: slashed import id, full-ARN, sub-resource, composite id, known
over-emit). Reuse/extend the stress seeds (`testdata/<cloud>-stress/`). Per sample:
1. `terraform plan` the seed, eyeball resources for cost, then `apply`.
2. Wait for enumeration indexing (RE / CAI / ARG lag ~1-3 min; poll).
3. Run TerraLift phases 2,3,4 `--hcl-only`; confirm coverage (0 gap), `terraform validate` each stack,
   verify redaction (no seed secrets in generated HCL).
4. Curate any new over-emit fields into `overEmitAttr` (add field name; re-validate; iterate).
5. Round-trip the *cheap* subset (destroy seed → rebuild from generated HCL → `plan` = "No changes")
   at least once per cloud; for VMs use import-plan-clean or immediate destroy to avoid double cost.
6. Tear down; verify via direct API (not the lagging enumerator); force-delete secrets (recovery windows).

---

## 7. Resume / recovery (if a session dies)

- `ls docs/coverage/` shows which batch files exist = which batches are done.
- The `<cloud>-CHECKLIST.md` shows aggregation status per type.
- `git log`/`git status` shows which map updates are committed.
- The terralift-aws/gcp/azure-provider memories track the campaign state.
- Pick up at the next missing batch file or the next un-aggregated cloud. Never redo a persisted batch.

---

## 8. Definition of done (per cloud)

- [ ] All 4 batch coverage files exist and are verified.
- [ ] `<cloud>-CHECKLIST.md` complete; every native enumerable type has a Status.
- [ ] Go maps updated so no user-resource type is an unexplained gap (`excluded`/`no-import` are documented, not silent).
- [ ] `go build`/`vet`/`test` green.
- [ ] Live spot-test: representative + all tricky types → 0 gap, validate clean, redaction verified, one round-trip.
- [ ] Torn down; cost confirmed negligible.

When all three clouds hit "done": update the memories, commit, and it's ready to PR / release.
