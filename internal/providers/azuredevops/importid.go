package azuredevops

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. Azure DevOps has four import
// shapes, all precomputed per resource in enumerate:
//   - bare GUID: azuredevops_project;
//   - bare int: azuredevops_agent_pool (org-level);
//   - bare descriptor (vssgp./aadgp./ungrp.): azuredevops_group (org-level graph);
//   - 2-part <projectGUID>/<child> for the project children — the leaf is a UUID
//     (git_repository, team) or an int (build_definition, variable_group, agent_queue, environment).
//
// The value is stored under "importID" (deliberately NOT "token": the auth credential is a PAT, so
// an id/path field must not borrow that name). Here we only escape it: neutralize any ${…}/%{…}
// before hcl.ImportBlock's %q (which does not).
func deriveImportID(r *model.Resource) string {
	id, _ := r.Properties["importID"].(string)
	return util.EscapeHCLTemplate(id)
}
