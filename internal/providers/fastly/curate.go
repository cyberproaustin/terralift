package fastly

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for Fastly is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/fastly.md). fastly_service_vcl is the
// HEAVIEST curation surface of any provider — one resource emits the entire nested block
// tree (dozens of domains/backends/headers/conditions/logging_* endpoints). Phase-B work:
//   - fastly_service_vcl: prune computed active_version/cloned_version/force_refresh;
//     tolerate set-block reordering (backend/header/condition); keep the activate/reuse
//     control attrs.
//   - Nested SECRETS (write-only) MUST be scrubbed if generate-config-out emits them:
//     logging_s3.secret_key, logging_gcs/bigquery service-account keys, logging_splunk/
//     https/datadog/newrelic tokens, logging_kafka/kinesis creds, backend.ssl_client_key.
//     Their values are re-supplied out-of-band at apply time (flag in the export note).
//   - fastly_service_compute: the package{} Wasm artifact is not returned by the API →
//     the service shell/config is adoptable but the package is a manual re-supply.
//   - fastly_tls_subscription/_activation/_certificate/_service_authorization/_user:
//     prune the documented computed attrs (created_at/updated_at/state/...); user is
//     secret-free.
//   - fastly_service_dictionary_items/_acl_entries/_dynamic_snippet_content: author
//     manage_* = true so TF owns the entries; tolerate over-emit + ordering.
//
// Until Phase B these are no-ops, so a Fastly export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for the nested
// logging/backend secrets above).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
