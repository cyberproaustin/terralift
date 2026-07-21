package launchdarkly

import "github.com/cyberproaustin/terralift/internal/hcl"

// Curation for LaunchDarkly is a Phase-B task confirmed against real `terraform plan
// -generate-config-out` output (docs/v2-specs/launchdarkly.md). LaunchDarkly has NO single
// monster resource; the weight is by VOLUME (the flag×env plane is tens of thousands of
// resources on a big account), and the recurring hazards are the environment SDK keys, the
// destination config credentials, and the webhook secret. Phase-B work, by resource:
//   - launchdarkly_environment — SECRET (the defining scrub). api_key (server-side SDK key),
//     mobile_key, and client_side_id are computed credentials returned on read → SCRUB the
//     values, keep the environment block. The SDK key reads/streams all flag data.
//   - launchdarkly_webhook — SECRET. `secret` (HMAC signing secret) → SCRUB; the url is not
//     itself secret. Import by the bare _id.
//   - launchdarkly_destination — SECRET (Enterprise). the `config` map carries per-kind sink
//     credentials (mParticle api_key/secret, Segment write_key, Azure policy_key, …) → SCRUB
//     the credential fields, keep the block. 3-part <proj>/<env>/<_id> import.
//   - launchdarkly_feature_flag — light per-resource. Terraformer quirk: IgnoreKeys
//     "include_in_snippet" (deprecated, conflicts with client_side_availability) — prune. JSON
//     variation `value` is a raw JSON string → keep LITERAL. Prune computed _id/timestamps.
//   - launchdarkly_feature_flag_environment — the volume driver; targeting tree (on/fallthrough/
//     off_variation/targets/rules/prerequisites). Rule ordering significant. targets/rules carry
//     user/context keys (identifiers, not secrets — adopt). Prune computed _id.
//   - launchdarkly_segment — targeting (included/excluded/rules). Prune computed _id/creation_date.
//   - launchdarkly_project — light shell; emit WITHOUT inline environments {} (the standalone
//     environment resources own the envs — the two conflict). Prune the deprecated
//     include_in_snippet + computed _id.
//   - launchdarkly_metric / launchdarkly_team / launchdarkly_custom_role — light, no secret. Team
//     carries the inline member roster (self-adoption caution). Custom-role policy_statements use
//     the proj/*:env/*:flag/* RBAC grammar → keep literal.
//
// Until Phase B these are no-ops, so a LaunchDarkly export is a breadth scaffold, not yet
// plan-clean (the pipeline's repo-wide secret scan is the backstop for the environment SDK keys,
// destination config, and webhook secret that generate-config-out emits before the scrub rules
// land).

func pruneGeneratedHCL(path string) int { return 0 }

func scrubGeneratedHCL(path string) []hcl.Redaction { return nil }
