package honeycomb

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Honeycomb's import
// composite mirrors Fastly's `<service_id>/<sub_id>`: dataset-scoped resources import by
// `<dataset>/<token>`, team/environment-wide resources import by a bare token, and — the
// subtle fork — the environment-wide "__all__" variants of derived_columns / multi-dataset
// triggers/SLOs/burn_alerts DROP the prefix and import BARE. Escaping is load-bearing:
// derived-column aliases / column key_names are free text that can carry ${…}/%{…}, and
// hcl.ImportBlock renders with %q, which does not neutralize template markers.
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "honeycombio_dataset",
		"honeycombio_flexible_board",
		"honeycombio_email_recipient",
		"honeycombio_pagerduty_recipient",
		"honeycombio_slack_recipient",
		"honeycombio_webhook_recipient",
		"honeycombio_msteams_recipient",
		"honeycombio_msteams_workflow_recipient":
		// team/environment-wide → bare token (slug / board id / recipient id).
		return p("token")
	default:
		// dataset-scoped: column, derived_column, query_annotation, trigger, slo, burn_alert.
		// `<dataset>/<token>` for a real dataset; BARE `<token>` when the resource is
		// environment-wide (the API pseudo-dataset "__all__", whose prefix is dropped).
		ds := p("dataset")
		if ds == "" || ds == "__all__" {
			return p("token")
		}
		return ds + "/" + p("token")
	}
}
