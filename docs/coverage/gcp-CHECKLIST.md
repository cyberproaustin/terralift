# GCP native-resource coverage — master checklist

Status of TerraLift's coverage of GCP native resource types. Built from the
per-family sweeps in `gcp-1..4-*.md` (276 resource rows researched across ~35
service families against the hashicorp/google provider docs + the GCP Cloud
Asset Inventory supported-asset-types list). See `PROCESS.md` for methodology.

## Summary

| Metric | Count |
|---|---|
| Resource rows researched (incl. sub-resources) | 276 |
| **Cloud Asset Inventory-enumerable types mapped** (`assetTypeToTF` + `assetTypeToTFExtra`) | **188** |
| — new from this sweep (`coverage.go` `assetTypeToTFExtra`) | 169 |
| — hand-curated core (`types.go` `assetTypeToTF`) | 19 |
| **No-import gaps** (enumerable, but Terraform provides no import) | 9 |

## Status legend
- **mapped** — CAI asset type is in `assetTypeToTF`/`assetTypeToTFExtra`; enumerated + exported.
- **excluded** — GCP-managed default (default network + auto subnets/firewalls/routes, google-managed
  service accounts, the operating project itself — handled by `excludedReason`/`notImportable`).
- **sub-resource** — not CAI-enumerable on its own; captured via the parent's `generate-config-out`.
- **iam-triad** — IAM has three resources per scope (`_iam_policy` / `_iam_binding` / `_iam_member`);
  TerraLift authors `_iam_member` for user bindings (see `internal/providers/gcp/iam.go`).
- **no-import** — enumerable but the provider has no import support (below).

## No-import gaps (the honest limitations)

Enumerable but hashicorp/google provides no `terraform import`:
- `google_bigtable_gc_policy` — a policy on a table column family; not independently importable.
- `google_dataflow_flex_template_job` / `google_dataproc_job` — job runs, not importable.
- `google_dataproc_cluster` — no import support in the provider.
- `google_endpoints_service` — config push, not importable.
- `google_service_account_key` / `google_sql_ssl_cert` — data-plane credential material (also
  control-plane-excluded); not importable and must never be captured.
- `google_vertex_ai_dataset` / `google_workflows_workflow` — no import support.

## Import-ID note

GCP import IDs are path-shaped (`projects/{{project}}/locations/{{loc}}/{{kind}}/{{name}}`), which
TerraLift derives from the CAI asset name. Unlike the AWS ARN table, most GCP imports follow the
asset-name path, so the default derivation covers the majority; the per-type formats are recorded
in the batch tables and any exceptions are confirmed during live spot-testing.

## Detail

- `gcp-1-compute-network-storage.md` (81 rows)
- `gcp-2-data-analytics.md` (58 rows)
- `gcp-3-serverless-containers-devops.md` (31 rows)
- `gcp-4-security-iam-ops-ml.md` (106 rows)

## Remaining verification

Live spot-test (needs a throwaway GCP project — the old lab was deleted; recreate per PROCESS.md §2)
to confirm the CAI-name → import-ID derivation for the new path-shaped types and to curate any
generate-config-out over-emits.
