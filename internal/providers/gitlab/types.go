package gitlab

// tfTypeMap maps a native GitLab resource key ("gitlab:<kind>") to its Terraform type. The provider
// is gitlabhq/gitlab. Phase A adopts CONFIGURATION under a two-root fan-out (groups + projects) —
// the durable objects, not secret DATA. CI/CD variable VALUES, hook tokens, and access-token
// resources are never decoded/adopted (see curate.go); access tokens are hard-excluded entirely.
var tfTypeMap = map[string]string{
	"gitlab:group":               "gitlab_group",
	"gitlab:project":             "gitlab_project",
	"gitlab:group_variable":      "gitlab_group_variable",
	"gitlab:project_variable":    "gitlab_project_variable",
	"gitlab:group_label":         "gitlab_group_label",
	"gitlab:project_label":       "gitlab_project_label",
	"gitlab:group_hook":          "gitlab_group_hook",
	"gitlab:project_hook":        "gitlab_project_hook",
	"gitlab:deploy_key":          "gitlab_deploy_key",
	"gitlab:branch_protection":   "gitlab_branch_protection",
	"gitlab:tag_protection":      "gitlab_tag_protection",
	"gitlab:group_membership":    "gitlab_group_membership",
	"gitlab:project_membership":  "gitlab_project_membership",
	"gitlab:project_milestone":   "gitlab_project_milestone",
	"gitlab:project_share_group": "gitlab_project_share_group",
	"gitlab:group_ldap_link":     "gitlab_group_ldap_link",
}

func tfType(native string) string { return tfTypeMap[native] }
