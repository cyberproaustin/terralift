package ns1

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

// rawImportID builds the un-escaped import ID per the ns1/ns1 docs. Quirks: ns1_zone
// imports by the FQDN zone name; ns1_record by the three-part "<zone>/<domain>/<type>"
// (slash, FQDN domain); ns1_datafeed by "<datasource_id>/<datafeed_id>"; user/tsigkey
// by name; the rest by a bare hex id.
func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "ns1_zone":
		return p("zone")
	case "ns1_record":
		return p("zone") + "/" + p("domain") + "/" + p("type")
	case "ns1_datafeed":
		return p("datasource_id") + "/" + p("datafeed_id")
	case "ns1_user":
		return p("username")
	case "ns1_tsigkey":
		return p("name")
	case "ns1_monitoringjob", "ns1_datasource", "ns1_notifylist", "ns1_team", "ns1_apikey":
		return p("id")
	default:
		return r.ID
	}
}
