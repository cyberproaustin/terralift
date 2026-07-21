package datadog

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID (hcl.ImportBlock
// renders with %q which does not neutralize ${ } / %{ }; import IDs here can be free-text
// names, e.g. a logs index or metric name, so escaping is load-bearing).
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

// rawImportID builds the un-escaped import ID per the DataDog/datadog docs. Unlike Fastly
// (slash composites) and DigitalOcean (comma composites), EVERY Datadog resource imports
// by a single BARE token — the variety is only in WHICH token: synthetics uses public_id,
// logs_index uses the index name, everything else uses the id (numeric ids are already
// stringified at enumeration).
func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "datadog_synthetics_test":
		return p("public_id")
	case "datadog_logs_index":
		return p("name")
	default:
		// monitor, dashboard, dashboard_list, service_level_objective,
		// logs_custom_pipeline, logs_metric (id = metric name), notebook,
		// security_monitoring_rule, downtime_schedule, role, user.
		return p("id")
	}
}
