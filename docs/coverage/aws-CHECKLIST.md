# AWS native-resource coverage — master checklist

Status of TerraLift's coverage of AWS native resource types. Built from the
per-family sweeps in `aws-1..4-*.md` (427 resource rows researched across ~40
service families against the hashicorp/aws provider docs + the AWS Resource
Explorer supported-types list). See `PROCESS.md` for methodology.

## Summary

| Metric | Count |
|---|---|
| Resource rows researched (incl. sub-resources) | 427 |
| **Resource Explorer-enumerable types mapped** (`awsTypeToTF` + `awsTypeToTFExtra`) | **251** |
| — new from this sweep (`coverage.go` `awsTypeToTFExtra`) | 193 |
| — hand-curated core (`types.go` `awsTypeToTF`) | 58 |
| Full-ARN import overrides added (`importIDOverrideExtra`) | 38 |
| Slashed/composite import overrides (`importIDOverride`) | 11 |
| AWS-managed defaults excluded (`awsManagedDefault` + default-VPC detector) | singleton-safe |
| **No-import gaps** (enumerable, but Terraform provides no import) | 7 |

## Status legend
- **mapped** — native type is in `awsTypeToTF`/`awsTypeToTFExtra`; enumerated + exported.
- **override** — non-default import ID handled (full ARN / slashed / composite / URL).
- **excluded** — AWS-managed default/singleton (default VPC + children, RE infra, default
  event bus, Athena `primary`, X-Ray `Default`, MemoryDB/ElastiCache defaults, service-linked roles).
- **sub-resource** — not RE-enumerable on its own; captured via `generate-config-out` of its parent.
- **no-import** — enumerable but the provider has NO import support (documented gap below).

## No-import gaps (the honest limitations)

These types can be enumerated but hashicorp/aws provides no `terraform import`, so TerraLift
surfaces them in the coverage report but cannot bring them into state. Documented, not silent:

- `aws_ebs_snapshot_copy` — a copy action, not an importable resource (use `aws_ebs_snapshot`).
- `aws_kinesis_firehose_delivery_stream` — import unsupported for `s3` destination type (use `extended_s3`).
- `aws_lakeformation_permissions` / `aws_lakeformation_resource` / `aws_lakeformation_resource_lf_tags` — permission grants, not importable.
- `aws_spot_instance_request` — request action, not importable.
- `aws_vpn_connection_route` — a route within a VPN connection, not independently importable.

## Detail

Full per-type tables (terraform type · Resource Explorer type · import ID · notes) are in:
- `aws-1-net-compute-storage.md` (99 rows)
- `aws-2-data-analytics.md` (97 rows)
- `aws-3-serverless-containers-integration.md` (89 rows)
- `aws-4-security-ops-edge.md` (142 rows)

## Remaining verification (live spot-test — see PROCESS.md §6)

The map + import-ID data is from provider-docs research; over-emit curation and any import-ID
mis-classification are confirmed only by touching a type. Spot-test the tricky ones (full-ARN,
slashed, composite import IDs) via `testdata/aws-stress` and expand `overEmitAttr` as new
`terraform validate` failures surface. Coverage of the common + tricky types is validated;
the long tail is mapped and will be confirmed as encountered.
