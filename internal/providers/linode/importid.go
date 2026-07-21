package linode

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

// rawImportID builds the un-escaped import ID per the linode/linode docs. Quirks:
// composites are COMMA-joined with load-bearing arity (record 2, nb_config 2, nb_node
// 3, vpc_subnet 2); object_storage_bucket is the sole COLON composite (<region>:<label>);
// image ids carry a "private/" prefix; rdns imports by IP; everything else is a bare id.
func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "linode_domain_record":
		return p("domain_id") + "," + p("record_id")
	case "linode_nodebalancer_config":
		return p("nodebalancer_id") + "," + p("config_id")
	case "linode_nodebalancer_node":
		return p("nodebalancer_id") + "," + p("config_id") + "," + p("node_id")
	case "linode_vpc_subnet":
		return p("vpc_id") + "," + p("subnet_id")
	case "linode_object_storage_bucket":
		return p("region") + ":" + p("label") // the one COLON composite
	case "linode_image":
		return p("id") // string incl. "private/" prefix
	case "linode_rdns":
		return p("address")
	default:
		// instance/domain/firewall/nodebalancer/volume/stackscript/lke_cluster/vpc/
		// sshkey/database_* — the bare numeric id.
		return p("id")
	}
}
