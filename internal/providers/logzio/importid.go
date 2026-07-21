package logzio

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Every Logz.io resource
// imports by a single BARE token — a numeric id (stringified), a string id (drop_filter), or a
// singleton sentinel (authentication_groups). There are NO composites (unlike Fastly's slashes
// or the identity providers' colon composites). Escaping keeps parity with the other providers
// (an alert title-derived name or id is unlikely to carry ${…}, but the guard is cheap).
func deriveImportID(r *model.Resource) string {
	s, _ := r.Properties["token"].(string)
	return util.EscapeHCLTemplate(s)
}
