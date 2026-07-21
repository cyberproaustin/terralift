package fastly

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

// rawImportID builds the un-escaped import ID per the fastly/fastly docs. Quirks:
// services import by the bare service id (active/latest version — do NOT pin @version);
// the three service-content companions use a SLASH composite <service_id>/<sub_id>;
// TLS/service_authorization/user import by a bare opaque id.
func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "fastly_service_vcl", "fastly_service_compute":
		return p("service_id")
	case "fastly_service_dictionary_items":
		return p("service_id") + "/" + p("dictionary_id")
	case "fastly_service_acl_entries":
		return p("service_id") + "/" + p("acl_id")
	case "fastly_service_dynamic_snippet_content":
		return p("service_id") + "/" + p("snippet_id")
	case "fastly_service_authorization", "fastly_tls_subscription", "fastly_tls_activation",
		"fastly_tls_certificate", "fastly_tls_private_key", "fastly_user":
		return p("id")
	default:
		return r.ID
	}
}
