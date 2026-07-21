package gitlab

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for GitLab is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/gitlab.md). GitLab has NO single monster resource;
// the recurring hazard is FIELD-LEVEL write-only secrets — and, uniquely, the CI/CD variables API
// RETURNS the secret value on read, so that scrub is the paramount one. Phase-B work, by resource:
//   - gitlab_project_variable / gitlab_group_variable — the #1 secret surface. The `value` is a live
//     secret the list API returns in cleartext → SCRUB the value (keep the shell: key, scope,
//     protected/masked flags). enumerate never decodes `value`; the leak risk is only in the
//     generate-config-out draft.
//   - gitlab_project_hook / gitlab_group_hook — `token`, `custom_headers`, `url_variables`, and any
//     `signing_token` are write-only → SCRUB. Adopt the shell (url, enabled events).
//   - gitlab_project — `runners_token` and `import_url` embedded credentials → SCRUB; keep the repo
//     config. gitlab_group — no secret.
//   - gitlab_deploy_key — `key` is the PUBLIC key (safe to keep); no private material is exposed.
//   - gitlab_*_label / *_milestone / *_membership / branch_protection / tag_protection /
//     project_share_group / group_ldap_link — no secret; adopt as-is. Keep any label color / ldap
//     cn/filter LITERAL.
//
// HARD-EXCLUDE (never adopted, never enumerated): the access-token resources
// (gitlab_personal_access_token, gitlab_project_access_token, gitlab_group_access_token,
// gitlab_deploy_token, gitlab_pipeline_schedule_variable secrets) — they MINT/return a live
// credential on create/read. Admin-only planes (system hooks, GET /users) are deferred, not adopted.
//
// Until Phase B these are no-ops, so a GitLab export is a breadth scaffold, not yet plan-clean (the
// pipeline's repo-wide secret scan is the backstop for the variable values / hook tokens /
// runners_token that generate-config-out leaves in generated.tf before the scrub rules land). The
// GITLAB_TOKEN is never inlined — providers.tf is env-auth only.

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
