package opsgenie

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Opsgenie mixes bare
// uuids/names with SLASH composites — but the composites do NOT agree on the parent
// identifier: schedule_rotation/team_routing_rule/service_incident_rule use the parent id,
// notification_rule uses the user id, while user_contact uses the USERNAME. And the two policy
// resources flip: alert_policy is bare when global / <team_id>/<policy_id> when team-attached,
// notification_policy is always <team_id>/<policy_id>. The separator + parent key are chosen
// by an explicit per-TF-type switch (never inferred); enumerate.go stores left/right (and the
// alert-policy team) already in import order. Escaping keeps parity with the other providers.
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "opsgenie_alert_policy":
		// bare <policy_id> when global; <team_id>/<policy_id> when team-attached.
		if team := p("team"); team != "" {
			return team + "/" + p("token")
		}
		return p("token")
	case "opsgenie_team_routing_rule",
		"opsgenie_schedule_rotation",
		"opsgenie_service_incident_rule",
		"opsgenie_notification_rule",
		"opsgenie_user_contact",
		"opsgenie_notification_policy":
		// SLASH composite; left/right already stored in the right order + with the right
		// parent identifier (user_contact's left is the username, notification_rule's is the id).
		return p("left") + "/" + p("right")
	default:
		// bare uuid/name: team, user, schedule, escalation, service, api_integration,
		// email_integration, maintenance, heartbeat (heartbeat's token is its name).
		return p("token")
	}
}
