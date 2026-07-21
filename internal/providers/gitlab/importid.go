package gitlab

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. GitLab's import ids are the
// provider's defining hazard — four distinct composite shapes, all colon-separated, whose exact
// form differs per resource:
//   - bare numeric id: gitlab_group / gitlab_project;
//   - 2-part <parent_id>:<leaf> where leaf is a numeric id (hook/label/deploy_key/membership/
//     milestone/share_group) or a NAME (branch_protection/tag_protection);
//   - 3-part <parent_id>:<key>:<environment_scope> for the CI/CD variables (the env scope, default
//     '*', is part of the identity — the same key in two scopes is two resources);
//   - 4-part <group_id>:<provider>:<cn>:<filter> for gitlab_group_ldap_link (cn XOR filter — one
//     of the last two segments is always empty).
//
// enumerate PRECOMPUTES the correct string per resource and stores it under "importID" (deliberately
// NOT "token": GitLab is full of real tokens — PATs, hook tokens — so a path/id field must not
// borrow that name). Here we only escape it: neutralize any ${…}/%{…} before hcl.ImportBlock's %q.
func deriveImportID(r *model.Resource) string {
	id, _ := r.Properties["importID"].(string)
	return util.EscapeHCLTemplate(id)
}
