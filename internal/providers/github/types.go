package github

// tfTypeMap maps a native GitHub resource key (our own "github:<kind>" scheme) to
// its Terraform type in the integrations/github provider. New resource kinds are
// added here as enumeration is extended.
var tfTypeMap = map[string]string{
	"github:repository":           "github_repository",
	"github:repository_webhook":   "github_repository_webhook",
	"github:branch_protection":    "github_branch_protection",
	"github:membership":           "github_membership",
	"github:team":                 "github_team",
	"github:team_membership":      "github_team_membership",
	"github:organization_webhook": "github_organization_webhook",
}

// tfType returns the Terraform type for a native key, or "" if unmapped (a gap).
func tfType(native string) string { return tfTypeMap[native] }
