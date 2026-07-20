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
	default:
		return r.Name
	}
}
