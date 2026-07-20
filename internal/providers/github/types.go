package github

// tfTypeMap maps a native GitHub resource key (our own "github:<kind>" scheme) to
// its Terraform type in the integrations/github provider. New resource kinds are
// added here as enumeration is extended.
var tfTypeMap = map[string]string{
	"github:repository":         "github_repository",
	"github:repository_webhook": "github_repository_webhook",
}

// tfType returns the Terraform type for a native key, or "" if unmapped (a gap).
func tfType(native string) string { return tfTypeMap[native] }
