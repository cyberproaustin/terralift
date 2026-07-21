package newrelic

import (
	"strings"

	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID (hcl.ImportBlock
// renders with %q which does not neutralize ${ } / %{ }; import IDs embed free-text names
// and entity GUIDs, so escaping is load-bearing).
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

// rawImportID builds the un-escaped import ID per the newrelic/newrelic docs. New Relic
// mixes bare GUIDs, bare ids, and 2-/3-part composites whose ordering is INCONSISTENT — the
// #1 hazard is newrelic_alert_policy, which puts the account SECOND (<policy_id>:<account_id>),
// the reverse of the muting-rule / workload composites. Every case is pinned to the verified
// registry format.
func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "newrelic_alert_policy":
		// account SECOND — the odd one out.
		return p("policy_id") + ":" + p("account_id")
	case "newrelic_nrql_alert_condition":
		// <policy_id>:<condition_id>:<static|baseline> — the type is lower-cased and load-bearing.
		return p("policy_id") + ":" + p("condition_id") + ":" + strings.ToLower(p("condition_type"))
	case "newrelic_alert_muting_rule":
		// account FIRST.
		return p("account_id") + ":" + p("id")
	case "newrelic_workload":
		// 3-part, account FIRST.
		return p("account_id") + ":" + p("workload_id") + ":" + p("guid")
	case "newrelic_one_dashboard",
		"newrelic_synthetics_monitor",
		"newrelic_synthetics_script_monitor",
		"newrelic_synthetics_cert_check_monitor",
		"newrelic_synthetics_broken_links_monitor",
		"newrelic_synthetics_step_monitor",
		"newrelic_key_transaction":
		return p("guid")
	default:
		// bare id: workflow, notification_destination, notification_channel,
		// obfuscation_rule, obfuscation_expression.
		return p("id")
	}
}
