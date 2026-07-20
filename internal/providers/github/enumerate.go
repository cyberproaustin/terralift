package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for a GitHub org/user scope via the gh API. There
// is one flat container (the scope login); resources carry a native "github:<kind>"
// type resolved to a Terraform type at export.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	scope := run.Scope
	owner := scope.ID
	run.Log.Info("Enumerate", "GitHub API: %s/%s", scope.Type, owner)

	inv := &model.Inventory{
		Cloud:       "github",
		Scope:       scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{owner: {ID: owner, Name: owner, Type: scope.Type}},
	}

	repos, err := listRepos(ctx, scope)
	if err != nil {
		return nil, err
	}
	active := repos[:0]
	for _, r := range repos {
		if r.Archived {
			continue // archived repos are read-only; not meaningful to manage as IaC
		}
		active = append(active, r)
		add(inv, &model.Resource{
			ID:         r.FullName, // owner/name
			Name:       r.Name,
			NativeType: "github:repository",
			Container:  owner,
			Source:     "gh-api",
			Properties: map[string]any{
				"default_branch": r.DefaultBranch,
				"private":        r.Private,
				"has_downloads":  r.HasDownloads,
			},
		})
	}
	run.Log.Info("Enumerate", "floor: %d repositories", len(active))

	// Per-repo sub-resources: webhooks.
	hooks := 0
	for _, r := range active {
		whs, err := listWebhooks(ctx, owner, r.Name)
		if err != nil {
			run.Log.Verbose("Enumerate", "list webhooks for %s skipped: %v", r.FullName, err)
			continue
		}
		for _, h := range whs {
			hookID := strconv.FormatInt(h.ID, 10)
			add(inv, &model.Resource{
				ID:         fmt.Sprintf("%s/hooks/%s", r.FullName, hookID),
				Name:       r.Name + "-hook-" + hookID,
				NativeType: "github:repository_webhook",
				Container:  owner,
				Source:     "gh-api",
				Properties: map[string]any{"repo": r.Name, "hook_id": hookID, "url": h.Config.URL},
			})
			hooks++
		}
	}
	if hooks > 0 {
		run.Log.Info("Enumerate", "webhooks: %d", hooks)
	}

	// Per-repo sub-resources: branch protection rules (one per protected branch).
	protections := 0
	for _, r := range active {
		pbs, err := listProtectedBranches(ctx, owner, r.Name)
		if err != nil {
			run.Log.Verbose("Enumerate", "list protected branches for %s skipped: %v", r.FullName, err)
			continue
		}
		for _, pb := range pbs {
			add(inv, &model.Resource{
				ID:         fmt.Sprintf("%s/protection/%s", r.FullName, pb.Name),
				Name:       r.Name + "-protect-" + pb.Name,
				NativeType: "github:branch_protection",
				Container:  owner,
				Source:     "gh-api",
				Properties: map[string]any{"repo": r.Name, "pattern": pb.Name},
			})
			protections++
		}
	}
	if protections > 0 {
		run.Log.Info("Enumerate", "branch protections: %d", protections)
	}

	// Org-level resources (only when the scope is an organization).
	if scope.Type == model.ScopeOrganization {
		enumOrg(ctx, run, inv, owner)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	return inv, nil
}

type webhook struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

// listWebhooks returns a repository's configured webhooks.
func listWebhooks(ctx context.Context, owner, repoName string) ([]webhook, error) {
	return ghAPIList[webhook](ctx, fmt.Sprintf("repos/%s/%s/hooks?per_page=100", owner, repoName))
}

// enumOrg injects organization-level resources (members; teams and org webhooks
// are added as their scopes become available). owner is the org login.
func enumOrg(ctx context.Context, run *core.Run, inv *model.Inventory, owner string) {
	members, err := ghAPIList[orgMember](ctx, fmt.Sprintf("orgs/%s/members?per_page=100", owner))
	if err != nil {
		run.Log.Verbose("Enumerate", "list org members skipped: %v", err)
		return
	}
	for _, m := range members {
		add(inv, &model.Resource{
			ID:         fmt.Sprintf("%s/members/%s", owner, m.Login),
			Name:       "member-" + m.Login,
			NativeType: "github:membership",
			Container:  owner,
			Source:     "gh-api",
			Properties: map[string]any{"org": owner, "username": m.Login},
		})
	}
	if len(members) > 0 {
		run.Log.Info("Enumerate", "org members: %d", len(members))
	}
}

type orgMember struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

type protectedBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

// listProtectedBranches returns a repository's protected branches; each maps to a
// github_branch_protection whose pattern is the branch name (exact-branch rules;
// wildcard patterns would need the GraphQL branchProtectionRules API).
func listProtectedBranches(ctx context.Context, owner, repoName string) ([]protectedBranch, error) {
	return ghAPIList[protectedBranch](ctx, fmt.Sprintf("repos/%s/%s/branches?protected=true&per_page=100", owner, repoName))
}

// add records a resource, resolving its Terraform type from the native key.
func add(inv *model.Inventory, r *model.Resource) {
	r.TFType = tfType(r.NativeType)
	inv.Resources[strings.ToLower(r.ID)] = r
}

type repo struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	HasDownloads  bool   `json:"has_downloads"`
	DefaultBranch string `json:"default_branch"`
}

// repoHasDownloads maps each enumerated repository's name to its live has_downloads
// value, for authoring the attribute generate-config-out omits.
func repoHasDownloads(inv *model.Inventory) map[string]bool {
	out := map[string]bool{}
	for _, r := range inv.Resources {
		if r.NativeType == "github:repository" {
			hd, _ := r.Properties["has_downloads"].(bool)
			out[r.Name] = hd
		}
	}
	return out
}

// listRepos returns the repositories in scope: an org's repos, or (for a user
// scope, which is the authenticated account) the repos the user owns.
func listRepos(ctx context.Context, scope model.Scope) ([]repo, error) {
	path := "user/repos?per_page=100&affiliation=owner"
	if scope.Type == model.ScopeOrganization {
		path = "orgs/" + scope.ID + "/repos?per_page=100"
	}
	return ghAPIList[repo](ctx, path)
}
