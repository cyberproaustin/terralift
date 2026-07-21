package vultr

// tfTypeMap maps a native Vultr resource key ("vultr:<kind>") to its Terraform type.
// NB: current (v2) provider names — Terraformer emits the retired v1 names
// (vultr_server, vultr_network); do not copy those. The node-pool resource is PLURAL.
var tfTypeMap = map[string]string{
	"vultr:instance":             "vultr_instance",
	"vultr:bare_metal_server":    "vultr_bare_metal_server",
	"vultr:dns_domain":           "vultr_dns_domain",
	"vultr:dns_record":           "vultr_dns_record",
	"vultr:firewall_group":       "vultr_firewall_group",
	"vultr:firewall_rule":        "vultr_firewall_rule",
	"vultr:block_storage":        "vultr_block_storage",
	"vultr:load_balancer":        "vultr_load_balancer",
	"vultr:vpc":                  "vultr_vpc",
	"vultr:vpc2":                 "vultr_vpc2",
	"vultr:ssh_key":              "vultr_ssh_key",
	"vultr:reserved_ip":          "vultr_reserved_ip",
	"vultr:startup_script":       "vultr_startup_script",
	"vultr:kubernetes":           "vultr_kubernetes",
	"vultr:kubernetes_node_pool": "vultr_kubernetes_node_pools",
	"vultr:database":             "vultr_database",
	"vultr:object_storage":       "vultr_object_storage",
}

func tfType(native string) string { return tfTypeMap[native] }
