package okta

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Okta mixes bare opaque
// ids with SLASH composites of varying DEPTH — the #1 hazard. Most resources are bare; the
// auth-server sub-resources and the top-level policy rules are 2-part; and the lone
// okta_auth_server_policy_rule (two fan-out levels down) is 3-part, outermost-first. The depth
// is chosen by an explicit per-TF-type switch (never inferred); enumerate.go stores the parts
// in import order. Escaping keeps parity with the other providers (and Okta EL strings can
// appear in the app/claim/rule config, though ids themselves are opaque).
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "okta_auth_server_policy_rule":
		// 3-part, outermost-first: <auth_server_id>/<policy_id>/<rule_id>.
		return p("a") + "/" + p("b") + "/" + p("c")
	case "okta_auth_server_scope",
		"okta_auth_server_claim",
		"okta_auth_server_policy",
		"okta_policy_rule_signon",
		"okta_policy_rule_password",
		"okta_policy_rule_mfa":
		// 2-part: <parent_id>/<child_id>.
		return p("left") + "/" + p("right")
	default:
		// bare opaque id (users, groups, group rules, user types, all app types, trusted
		// origins, zones, auth servers, top-level policies, hooks, IdPs).
		return p("token")
	}
}
