package azuread

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. The azuread v3 provider is
// the defining hazard here: it changed EVERY object import from a bare UUID to a Graph-PATH prefix,
// and each object type has a different prefix — so enumerate PRECOMPUTES the full string per
// resource and stores it under "importID":
//   - /groups/<id>, /applications/<id>, /servicePrincipals/<id>,
//     /identity/conditionalAccess/policies/<id>, /identity/conditionalAccess/namedLocations/<id>,
//     /directory/administrativeUnits/<id>;
//   - <group_id>/member/<member_id> for group_member (NO leading slash — the exception);
//   - /servicePrincipals/<sp_id>/appRoleAssignedTo/<assignment_id> for app_role_assignment;
//   - a BARE opaque id for directory_role_assignment (no prefix).
//
// The property is named "importID" (not "token": the auth credential is an OAuth secret, so an
// id/path field must not borrow that name). Here we only escape: neutralize any ${…}/%{…} before
// hcl.ImportBlock's %q.
func deriveImportID(r *model.Resource) string {
	id, _ := r.Properties["importID"].(string)
	return util.EscapeHCLTemplate(id)
}
