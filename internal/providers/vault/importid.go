package vault

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Every Vault resource imports
// by its PATH, and the exact path form differs by type — so enumerate PRECOMPUTES the correct
// string per resource (mount path with the trailing '/' stripped; policy/namespace name;
// `<backend>/roles/<name>` for secret-engine roles; `auth/<backend>/role/<name>` for jwt/approle
// roles; `auth/token/roles/<name>` for token roles) and stores it under "importID" (never "token"
// — that name is reserved for the VAULT_TOKEN credential). Here we only escape it: neutralize any
// ${…}/%{…} before hcl.ImportBlock's %q (which does not).
func deriveImportID(r *model.Resource) string {
	id, _ := r.Properties["importID"].(string)
	return util.EscapeHCLTemplate(id)
}
