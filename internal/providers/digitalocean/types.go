package digitalocean

// tfTypeMap maps a native DigitalOcean resource key ("digitalocean:<kind>") to its
// Terraform type in the digitalocean/digitalocean provider.
var tfTypeMap = map[string]string{
	"digitalocean:droplet":                  "digitalocean_droplet",
	"digitalocean:domain":                   "digitalocean_domain",
	"digitalocean:record":                   "digitalocean_record",
	"digitalocean:firewall":                 "digitalocean_firewall",
	"digitalocean:vpc":                      "digitalocean_vpc",
	"digitalocean:ssh_key":                  "digitalocean_ssh_key",
	"digitalocean:project":                  "digitalocean_project",
	"digitalocean:loadbalancer":             "digitalocean_loadbalancer",
	"digitalocean:reserved_ip":              "digitalocean_reserved_ip",
	"digitalocean:reserved_ipv6":            "digitalocean_reserved_ipv6",
	"digitalocean:certificate":              "digitalocean_certificate",
	"digitalocean:cdn":                      "digitalocean_cdn",
	"digitalocean:container_registry":       "digitalocean_container_registry",
	"digitalocean:kubernetes_cluster":       "digitalocean_kubernetes_cluster",
	"digitalocean:kubernetes_node_pool":     "digitalocean_kubernetes_node_pool",
	"digitalocean:database_cluster":         "digitalocean_database_cluster",
	"digitalocean:database_db":              "digitalocean_database_db",
	"digitalocean:database_user":            "digitalocean_database_user",
	"digitalocean:database_connection_pool": "digitalocean_database_connection_pool",
	"digitalocean:database_replica":         "digitalocean_database_replica",
	"digitalocean:volume":                   "digitalocean_volume",
	"digitalocean:tag":                      "digitalocean_tag",
}

func tfType(native string) string { return tfTypeMap[native] }
