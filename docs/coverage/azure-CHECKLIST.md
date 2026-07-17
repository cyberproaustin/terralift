# Azure native-resource coverage — master checklist

Status of TerraLift's coverage of Azure native resource types. Built from the
per-family sweeps in `azure-1..4-*.md` (510 resource rows across ~45 service
families against the hashicorp/azurerm provider docs + Azure resource-provider
docs). See `PROCESS.md` for methodology.

## How Azure coverage works (important)

Unlike AWS/GCP, TerraLift's Azure **export delegates the ARM→azurerm type mapping
and import-ID derivation to aztfexport** (v0.19+), which drives the azurerm
provider schema and covers essentially the full provider surface. So for Azure,
"does TerraLift handle type X" ≈ "does aztfexport + azurerm support X" — which is
comprehensive. Our own `azureTypeToTF`/`azureTypeToTFExtra` map is used ONLY for
the enumerate-phase coverage-CLASSIFICATION display (mapped vs unmapped), and for
routing hygiene/exposure signals. Expanding it just makes that display honest.

## Summary

| Metric | Count |
|---|---|
| Resource rows researched | 510 |
| Distinct ARM types classified (`azureTypeToTF` + `azureTypeToTFExtra`) | 342 |
| — new from this sweep (`coverage.go`) | 310 |
| — hand-curated core (`types.go`) | 32 |
| azurerm types covered by aztfexport export | full provider surface |

## Many-azurerm-per-ARM-type

Several azurerm resources can share one ARM type (e.g. `Microsoft.Web/sites` →
`azurerm_linux_web_app` / `azurerm_windows_web_app` / `azurerm_linux_function_app`
/ `azurerm_windows_function_app`, distinguished by `kind`). aztfexport picks the
correct azurerm type from the resource's `kind`/properties during export; our
classification map records one representative type per ARM type.

## Data-plane exclusions (control-plane-only mandate)

These carry secret material / data content and MUST NOT be captured — TerraLift's
Azure export `excludedReason` drops Key Vault secrets/keys/certificates and storage
content; aztfexport's `-k` skips anything requiring data-plane keys. Flagged in the
sweep (non-exhaustive): `azurerm_key_vault_key`, `azurerm_key_vault_secret`,
`azurerm_key_vault_certificate*`, `azurerm_storage_blob/container/share/queue/table`,
`azurerm_automation_credential`, `azurerm_app_configuration_key`,
`azurerm_iothub_*_certificate` / `*_shared_access_policy`.

## Detail

- `azure-1-compute-network-storage.md` (78 rows)
- `azure-2-data-analytics.md` (130 rows)
- `azure-3-app-integration-containers.md` (128 rows)
- `azure-4-security-identity-ops-ai.md` (174 rows)

## Remaining verification

Live spot-test on the Eutaxia sub (`81106197-4fec-452c-8cef-69328e602e8a`, user-
authorized) with a cheap seed of representative + data-plane types, to confirm the
aztfexport-driven export + our control-plane exclusions + hygiene classification.
