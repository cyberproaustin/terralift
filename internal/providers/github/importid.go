package github

import "github.com/cyberproaustin/terralift/internal/model"

// deriveImportID returns the Terraform import ID for a GitHub resource. Formats are
// per the integrations/github provider's per-resource "Import" docs; the owner is
// taken from the provider config, so most ids are the bare resource name.
func deriveImportID(r *model.Resource) string {
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
	default:
		return r.Name
	}
}
