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

	// Per-repo sub-resources: custom issue labels + actions secrets.
	labels, secrets := 0, 0
	for _, r := range active {
		lbls, err := listLabels(ctx, owner, r.Name)
		if err != nil {
			run.Log.Verbose("Enumerate", "list labels for %s skipped: %v", r.FullName, err)
		} else {
			for _, l := range lbls {
				if l.Default {
					continue // GitHub auto-creates 9 default labels; onboarding them is noise
				}
				add(inv, &model.Resource{
					ID:         fmt.Sprintf("%s/labels/%s", r.FullName, l.Name),
					Name:       r.Name + "-label-" + l.Name,
					NativeType: "github:issue_label",
					Container:  owner,
					Source:     "gh-api",
					Properties: map[string]any{"repo": r.Name, "label": l.Name},
				})
				labels++
			}
		}
		secs, err := listSecrets(ctx, owner, r.Name)
		if err != nil {
			run.Log.Verbose("Enumerate", "list actions secrets for %s skipped: %v", r.FullName, err)
			continue
		}
		for _, s := range secs {
			add(inv, &model.Resource{
				ID:         fmt.Sprintf("%s/secrets/%s", r.FullName, s.Name),
				Name:       r.Name + "-secret-" + s.Name,
				NativeType: "github:actions_secret",
				Container:  owner,
				Source:     "gh-api",
				Properties: map[string]any{"repo": r.Name, "secret_name": s.Name},
			})
			secrets++
		}
	}
	if labels > 0 {
		run.Log.Info("Enumerate", "custom labels: %d", labels)
	}
	if secrets > 0 {
		run.Log.Info("Enumerate", "actions secrets: %d (values are write-only; excluded from adoption)", secrets)
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

// enumOrg injects organization-level resources: members, teams (+ their
// memberships), and org webhooks. owner is the org login. Each list call is
// best-effort — a missing scope leaves a Verbose trace rather than aborting.
func enumOrg(ctx context.Context, run *core.Run, inv *model.Inventory, owner string) {
	if members, err := ghAPIList[orgMember](ctx, fmt.Sprintf("orgs/%s/members?per_page=100", owner)); err != nil {
		run.Log.Verbose("Enumerate", "list org members skipped: %v", err)
	} else {
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

	teams, err := ghAPIList[team](ctx, fmt.Sprintf("orgs/%s/teams?per_page=100", owner))
	if err != nil {
		run.Log.Verbose("Enumerate", "list teams skipped: %v", err)
	} else {
		tms := 0
		for _, tm := range teams {
			teamID := strconv.FormatInt(tm.ID, 10)
			add(inv, &model.Resource{
				ID:         fmt.Sprintf("%s/teams/%s", owner, tm.Slug),
				Name:       "team-" + tm.Slug,
				NativeType: "github:team",
				Container:  owner,
				Source:     "gh-api",
				Properties: map[string]any{"team_id": teamID},
			})
			mems, merr := ghAPIList[orgMember](ctx, fmt.Sprintf("orgs/%s/teams/%s/members?per_page=100", owner, tm.Slug))
			if merr != nil {
				run.Log.Verbose("Enumerate", "list members of team %s skipped: %v", tm.Slug, merr)
				continue
			}
			for _, mm := range mems {
				add(inv, &model.Resource{
					ID:         fmt.Sprintf("%s/teams/%s/members/%s", owner, tm.Slug, mm.Login),
					Name:       "teammember-" + tm.Slug + "-" + mm.Login,
					NativeType: "github:team_membership",
					Container:  owner,
					Source:     "gh-api",
					Properties: map[string]any{"team_id": teamID, "username": mm.Login},
				})
				tms++
			}
		}
		if len(teams) > 0 {
			run.Log.Info("Enumerate", "teams: %d (%d membership(s))", len(teams), tms)
		}
	}

	if hooks, err := ghAPIList[webhook](ctx, fmt.Sprintf("orgs/%s/hooks?per_page=100", owner)); err != nil {
		run.Log.Verbose("Enumerate", "list org webhooks skipped: %v", err)
	} else {
		for _, h := range hooks {
			hookID := strconv.FormatInt(h.ID, 10)
			add(inv, &model.Resource{
				ID:         fmt.Sprintf("%s/hooks/%s", owner, hookID),
				Name:       "orghook-" + hookID,
				NativeType: "github:organization_webhook",
				Container:  owner,
				Source:     "gh-api",
				Properties: map[string]any{"hook_id": hookID, "url": h.Config.URL},
			})
		}
		if len(hooks) > 0 {
			run.Log.Info("Enumerate", "org webhooks: %d", len(hooks))
		}
	}
}

type orgMember struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

type team struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
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

type label struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// listLabels returns a repository's issue labels (default GitHub labels included;
// the caller filters them out).
func listLabels(ctx context.Context, owner, repoName string) ([]label, error) {
	return ghAPIList[label](ctx, fmt.Sprintf("repos/%s/%s/labels?per_page=100", owner, repoName))
}

type secretName struct {
	Name string `json:"name"`
}

// listSecrets returns a repository's Actions secret NAMES (values are never
// returned by the API — they are write-only). The endpoint nests the array under
// `.secrets`, so this decodes the object rather than paging a bare array.
func listSecrets(ctx context.Context, owner, repoName string) ([]secretName, error) {
	var resp struct {
		Secrets []secretName `json:"secrets"`
	}
	if err := ghAPI(ctx, &resp, fmt.Sprintf("repos/%s/%s/actions/secrets?per_page=100", owner, repoName)); err != nil {
		return nil, err
	}
	return resp.Secrets, nil
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
