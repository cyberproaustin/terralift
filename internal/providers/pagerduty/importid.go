package pagerduty

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. PagerDuty mixes bare
// P-prefixed ids with composites, and the composites use DIFFERENT separators — DOT for
// service_integration / ruleset_rule, COLON for the team_membership / user_* composites — with
// a NON-uniform parent-id order (team_membership and the user_* composites are user-first).
// The separator is chosen by an explicit per-TF-type switch (never inferred); enumerate.go
// stores left/right already in import order. Escaping keeps parity with the other providers.
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "pagerduty_service_integration", "pagerduty_ruleset_rule":
		// DOT composite, parent-first: <service_id>.<integration_id> / <ruleset_id>.<rule_id>.
		return p("left") + "." + p("right")
	case "pagerduty_team_membership", "pagerduty_user_contact_method", "pagerduty_user_notification_rule":
		// COLON composite, USER-first: <user_id>:<team_id> / <user_id>:<child_id>.
		return p("left") + ":" + p("right")
	default:
		// bare P-prefixed id (service, escalation_policy, schedule, team, user,
		// business_service, maintenance_window, extension[_servicenow], webhook_subscription,
		// tag, response_play, ruleset).
		return p("token")
	}
}
