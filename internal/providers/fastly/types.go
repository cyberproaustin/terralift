package fastly

// tfTypeMap maps a native Fastly resource key ("fastly:<kind>") to its Terraform type
// in the fastly/fastly provider. NB: the CURRENT (un-_v1) names are used — Terraformer
// emits the deprecated _v1 aliases.
var tfTypeMap = map[string]string{
	"fastly:service_vcl":             "fastly_service_vcl",
	"fastly:service_compute":         "fastly_service_compute",
	"fastly:dictionary_items":        "fastly_service_dictionary_items",
	"fastly:acl_entries":             "fastly_service_acl_entries",
	"fastly:dynamic_snippet_content": "fastly_service_dynamic_snippet_content",
	"fastly:service_authorization":   "fastly_service_authorization",
	"fastly:tls_subscription":        "fastly_tls_subscription",
	"fastly:tls_activation":          "fastly_tls_activation",
	"fastly:tls_certificate":         "fastly_tls_certificate",
	"fastly:tls_private_key":         "fastly_tls_private_key",
	"fastly:user":                    "fastly_user",
}

func tfType(native string) string { return tfTypeMap[native] }
