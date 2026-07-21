package azuredevops

// tfTypeMap maps a native Azure DevOps resource key ("azuredevops:<kind>") to its Terraform type.
// The provider is microsoft/azuredevops. Phase A adopts CONFIGURATION under an org→project fan-out
// (plus two org-level roots: agent pools and graph groups). The secret-bearing service-endpoint
// family and the variable-group SECRET values are never decoded/adopted (see curate.go); the policy
// plane and service hooks are deferred.
var tfTypeMap = map[string]string{
	"azuredevops:project":          "azuredevops_project",
	"azuredevops:git_repository":   "azuredevops_git_repository",
	"azuredevops:build_definition": "azuredevops_build_definition",
	"azuredevops:variable_group":   "azuredevops_variable_group",
	"azuredevops:agent_queue":      "azuredevops_agent_queue",
	"azuredevops:team":             "azuredevops_team",
	"azuredevops:environment":      "azuredevops_environment",
	"azuredevops:agent_pool":       "azuredevops_agent_pool",
	"azuredevops:group":            "azuredevops_group",
}

func tfType(native string) string { return tfTypeMap[native] }
