package github

import "github.com/cyberproaustin/terralift/internal/model"

// deriveImportID returns the Terraform import ID for a GitHub resource. Formats are
// per the integrations/github provider's per-resource "Import" docs; the owner is
// taken from the provider config, so most ids are the bare resource name.
func deriveImportID(r *model.Resource) string {
	switch r.TFType {
	case "github_repository":
		return r.Name // imported by the repo name alone
	default:
		return r.Name
	}
}
