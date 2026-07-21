package digitalocean

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

// rawImportID builds the un-escaped import ID per the digitalocean/digitalocean docs.
// Two quirks are load-bearing: DO composite ids are COMMA-joined (not slash), and a
// few resources import by NAME (certificate, container_registry) not id.
func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "digitalocean_droplet":
		return p("droplet_id")
	case "digitalocean_domain":
		return p("name")
	case "digitalocean_record":
		return p("domain") + "," + p("record_id") // COMMA composite
	case "digitalocean_firewall":
		return p("firewall_id")
	case "digitalocean_vpc":
		return p("vpc_id")
	case "digitalocean_ssh_key":
		return p("ssh_key_id")
	case "digitalocean_project":
		return p("project_id")
	case "digitalocean_loadbalancer":
		return p("lb_id")
	case "digitalocean_reserved_ip", "digitalocean_reserved_ipv6":
		return p("ip")
	case "digitalocean_certificate":
		return p("name") // by NAME, not id (Let's Encrypt UUIDs rotate on renewal)
	case "digitalocean_cdn":
		return p("cdn_id")
	case "digitalocean_container_registry":
		return p("name")
	case "digitalocean_kubernetes_cluster":
		return p("cluster_id")
	case "digitalocean_kubernetes_node_pool":
		return p("pool_id") // bare pool id, no cluster/ prefix
	case "digitalocean_database_cluster":
		return p("cluster_id")
	case "digitalocean_database_db", "digitalocean_database_user",
		"digitalocean_database_connection_pool", "digitalocean_database_replica":
		return p("cluster_id") + "," + p("name") // COMMA composite
	case "digitalocean_volume":
		return p("volume_id")
	case "digitalocean_tag":
		return p("name")
	default:
		return r.ID
	}
}
