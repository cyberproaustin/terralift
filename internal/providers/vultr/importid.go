package vultr

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID (escaping keeps
// parity with the other providers; hcl.ImportBlock renders with %q which does not
// neutralize ${ } / %{ }).
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

// rawImportID builds the un-escaped import ID per the vultr/vultr docs. Quirks:
// dns_domain imports by NAME; dns_record and firewall_rule are COMMA composites (the
// firewall rule id is an INTEGER); kubernetes_node_pools is the sole SPACE composite
// "<cluster_id> <pool_id>"; everything else is a bare UUID.
func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "vultr_dns_domain":
		return p("domain")
	case "vultr_dns_record":
		return p("domain") + "," + p("record_id")
	case "vultr_firewall_rule":
		return p("firewall_group_id") + "," + p("rule_id")
	case "vultr_kubernetes_node_pools":
		return p("cluster_id") + " " + p("pool_id") // SPACE composite
	default:
		return p("id") // bare UUID
	}
}
