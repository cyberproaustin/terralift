package grafana

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Grafana is the inverse
// of Datadog: almost every import ID is an org-scoped COMPOSITE, `{{orgID}}:{{token}}`, built
// from the org id (resolved once in Connect and stored as r.Container) + a per-resource
// token. Escaping the FINISHED composite is load-bearing: rule-group titles and contact-point
// names are free text that can carry ${…}/%{…}, and hcl.ImportBlock renders with %q, which
// does not neutralize template markers.
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

func rawImportID(r *model.Resource) string {
	org := r.Container // the numeric org id, resolved in Connect
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "grafana_rule_group":
		// the lone THREE-part composite: {{orgID}}:{{folderUID}}:{{title}}.
		return org + ":" + p("folder_uid") + ":" + p("title")
	case "grafana_notification_policy":
		// singleton — the token after the org is arbitrary (one policy per org).
		return org + ":policy"
	case "grafana_contact_point", "grafana_message_template", "grafana_mute_timing":
		// import by NAME (free text).
		return org + ":" + p("name")
	case "grafana_organization":
		// the LONE bare import (no org prefix) — instance-scoped; the id it imports by IS an
		// org id. Deferred in Phase A; guarded here so wiring it later can't silently fall into
		// the org-prefixed default case.
		return p("token")
	default:
		// {{orgID}}:{{token}} where token is a uid (dashboard/folder/data_source/playlist/
		// library_panel/role) or a stringified numeric id (team/service_account/report).
		return org + ":" + p("token")
	}
}
