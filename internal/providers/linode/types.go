package linode

// tfTypeMap maps a native Linode resource key ("linode:<kind>") to its Terraform type.
// NB: linode_database_mysql/_postgresql are deprecated in favor of the _v2 resources;
// they share the same import id, so flip only these two strings to adopt v2.
var tfTypeMap = map[string]string{
	"linode:instance":              "linode_instance",
	"linode:domain":                "linode_domain",
	"linode:domain_record":         "linode_domain_record",
	"linode:firewall":              "linode_firewall",
	"linode:nodebalancer":          "linode_nodebalancer",
	"linode:nodebalancer_config":   "linode_nodebalancer_config",
	"linode:nodebalancer_node":     "linode_nodebalancer_node",
	"linode:volume":                "linode_volume",
	"linode:stackscript":           "linode_stackscript",
	"linode:lke_cluster":           "linode_lke_cluster",
	"linode:vpc":                   "linode_vpc",
	"linode:vpc_subnet":            "linode_vpc_subnet",
	"linode:image":                 "linode_image",
	"linode:rdns":                  "linode_rdns",
	"linode:sshkey":                "linode_sshkey",
	"linode:object_storage_bucket": "linode_object_storage_bucket",
	"linode:database_mysql":        "linode_database_mysql",
	"linode:database_postgresql":   "linode_database_postgresql",
}

func tfType(native string) string { return tfTypeMap[native] }
