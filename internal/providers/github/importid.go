package github

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID for a GitHub
// resource. Escaping is essential: import IDs embed free text (label names, branch
// patterns) that may contain ${ } / %{ }, and hcl.ImportBlock renders the id with
// %q, which does NOT neutralize template sequences — an unescaped `${file(...)}` in
// a label would be evaluated by `terraform plan -generate-config-out`.
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

// rawImportID derives the un-escaped import ID per the integrations/github
// provider's per-resource "Import" docs; the owner is taken from the provider
// config, so most ids are the bare resource name.
func rawImportID(r *model.Resource) string {
	switch r.TFType {
	case "github_repository":
		return r.Name // imported by the repo name alone
	case "github_repository_webhook":
		// Imported by "repository/hook_id" (the owner comes from the provider config).
		repo, _ := r.Properties["repo"].(string)
		id, _ := r.Properties["hook_id"].(string)
		return repo + "/" + id
	case "github_branch_protection":
		// Imported by "repository:pattern".
		repo, _ := r.Properties["repo"].(string)
		pattern, _ := r.Properties["pattern"].(string)
		return repo + ":" + pattern
	case "github_membership":
		// Imported by "org:username".
		org, _ := r.Properties["org"].(string)
		user, _ := r.Properties["username"].(string)
		return org + ":" + user
	case "github_team":
		// Imported by the numeric team id.
		id, _ := r.Properties["team_id"].(string)
		return id
	case "github_team_membership":
		// Imported by "team_id:username".
		id, _ := r.Properties["team_id"].(string)
		user, _ := r.Properties["username"].(string)
		return id + ":" + user
	case "github_organization_webhook":
		// Imported by the numeric hook id.
		id, _ := r.Properties["hook_id"].(string)
		return id
	case "github_issue_label":
		// Imported by "repository:name".
		repo, _ := r.Properties["repo"].(string)
		name, _ := r.Properties["label"].(string)
		return repo + ":" + name
	case "github_actions_secret":
		// Imported by "repository/secret_name".
		repo, _ := r.Properties["repo"].(string)
		name, _ := r.Properties["secret_name"].(string)
		return repo + "/" + name
	default:
		return r.Name
	}
}
