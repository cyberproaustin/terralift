package gitlab

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one GitLab instance under a two-ROOT fan-out: the groups and
// projects the token can manage (membership + min_access_level=40), then their durable config
// children — CI/CD variables (shells; the value is a secret, never decoded), labels, webhooks,
// deploy keys, protected branches/tags, memberships, milestones, group LDAP links, and project
// share-group links (derived from the project object, no list endpoint). One flat container = the
// instance. Best-effort per list: 401 → fatal; 403/404 → Verbose skip; other → Warn + count. The
// token never appears in errors/logs. Access-token resources and secret DATA are never enumerated.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	if glBase() == "" {
		return nil, fmt.Errorf("gitlab: GITLAB_BASE_URL is malformed (must be an http/https URL)")
	}
	instance := run.Scope.ID
	run.Log.Info("Enumerate", "GitLab API: instance=%s", instance)

	inv := &model.Inventory{
		Cloud:       "gitlab",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{instance: {ID: instance, Name: instance, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Root: groups the token can manage (subgroups come back FLAT — no recursion needed).
	var groups []glGroup
	list(run, &hardFails, &fatal, "groups", func() error {
		gs, err := glList[glGroup](ctx, "/groups?membership=true&min_access_level=40")
		groups = gs
		for _, g := range gs {
			if g.ID == 0 {
				continue
			}
			addRes(inv, "group/"+itoa(g.ID), orName(g.FullPath, g.Name), "gitlab:group", instance, itoa(g.ID))
		}
		return err
	})

	// Root: projects the token can manage. The share-group links ride on the project object.
	var projects []glProject
	list(run, &hardFails, &fatal, "projects", func() error {
		ps, err := glList[glProject](ctx, "/projects?membership=true&min_access_level=40")
		projects = ps
		for _, p := range ps {
			if p.ID == 0 {
				continue
			}
			pid := itoa(p.ID)
			addRes(inv, "project/"+pid, orName(p.PathWithNamespace, p.Name), "gitlab:project", instance, pid)
			for _, sg := range p.SharedWithGroups {
				if sg.GroupID == 0 {
					continue
				}
				addRes(inv, "project_share_group/"+pid+"/"+itoa(sg.GroupID), p.Name+"/"+sg.GroupName,
					"gitlab:project_share_group", instance, pid+":"+itoa(sg.GroupID))
			}
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}

	for _, g := range groups {
		if g.ID == 0 || fatal != nil {
			continue
		}
		enumGroup(ctx, run, inv, instance, g.ID, &fatal)
	}
	for _, p := range projects {
		if p.ID == 0 || fatal != nil {
			continue
		}
		enumProject(ctx, run, inv, instance, p.ID, &fatal)
	}

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("gitlab enumeration failed on %d resource type(s) and found nothing — check GITLAB_TOKEN/GITLAB_BASE_URL and the token's scope", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// enumGroup fans out one group: variables (3-part env-scope import), labels, hooks, memberships,
// and LDAP links (Premium/self-managed — expect 403/404 → skip).
func enumGroup(ctx context.Context, run *core.Run, inv *model.Inventory, instance string, id int, fatal *error) {
	gid := itoa(id)

	subList(run, fatal, "group variables", gid, func() error {
		vs, err := glList[glVariable](ctx, "/groups/"+gid+"/variables")
		for _, v := range vs {
			if v.Key == "" {
				continue
			}
			scope := envScope(v.EnvironmentScope)
			addRes(inv, "group_variable/"+gid+"/"+v.Key+"/"+scope, v.Key, "gitlab:group_variable", instance, gid+":"+v.Key+":"+scope)
		}
		return err
	})

	subList(run, fatal, "group labels", gid, func() error {
		ls, err := glList[glLabel](ctx, "/groups/"+gid+"/labels")
		for _, l := range ls {
			if l.ID == 0 {
				continue
			}
			// VERIFY (Phase B): gitlab_group_label's canonical import example is <group>:<name>,
			// while gitlab_project_label uses <project>:<id>. We emit the numeric id for both (the
			// labels API accepts id-or-title, so it likely round-trips); if the group-label import
			// rejects the id at live round-trip, fall back to l.Name here.
			addRes(inv, "group_label/"+gid+"/"+itoa(l.ID), l.Name, "gitlab:group_label", instance, gid+":"+itoa(l.ID))
		}
		return err
	})

	subList(run, fatal, "group hooks", gid, func() error {
		hs, err := glList[glHook](ctx, "/groups/"+gid+"/hooks")
		for _, h := range hs {
			if h.ID == 0 {
				continue
			}
			addRes(inv, "group_hook/"+gid+"/"+itoa(h.ID), hookName(h), "gitlab:group_hook", instance, gid+":"+itoa(h.ID))
		}
		return err
	})

	subList(run, fatal, "group members", gid, func() error {
		ms, err := glList[glMember](ctx, "/groups/"+gid+"/members")
		for _, m := range ms {
			if m.ID == 0 {
				continue
			}
			addRes(inv, "group_membership/"+gid+"/"+itoa(m.ID), m.Username, "gitlab:group_membership", instance, gid+":"+itoa(m.ID))
		}
		return err
	})

	// LDAP group links (Premium + self-managed only — 403/404 on CE/SaaS is a quiet skip).
	subList(run, fatal, "group ldap links", gid, func() error {
		lls, err := glList[glLdapLink](ctx, "/groups/"+gid+"/ldap_group_links")
		for _, ll := range lls {
			if ll.Provider == "" {
				continue
			}
			// 4-part <group>:<provider>:<cn>:<filter> — cn XOR filter, one segment is empty. The
			// inventory key keeps a '/' between cn and filter so "ab"+"" can't collide with "a"+"b".
			addRes(inv, "group_ldap_link/"+gid+"/"+ll.Provider+"/"+ll.CN+"/"+ll.Filter, ll.Provider,
				"gitlab:group_ldap_link", instance, gid+":"+ll.Provider+":"+ll.CN+":"+ll.Filter)
		}
		return err
	})
}

// enumProject fans out one project: variables (3-part env-scope import), labels, hooks, deploy keys,
// protected branches/tags (leaf is a NAME, not an id), memberships, and milestones.
func enumProject(ctx context.Context, run *core.Run, inv *model.Inventory, instance string, id int, fatal *error) {
	pid := itoa(id)

	subList(run, fatal, "project variables", pid, func() error {
		vs, err := glList[glVariable](ctx, "/projects/"+pid+"/variables")
		for _, v := range vs {
			if v.Key == "" {
				continue
			}
			scope := envScope(v.EnvironmentScope)
			addRes(inv, "project_variable/"+pid+"/"+v.Key+"/"+scope, v.Key, "gitlab:project_variable", instance, pid+":"+v.Key+":"+scope)
		}
		return err
	})

	subList(run, fatal, "project labels", pid, func() error {
		ls, err := glList[glLabel](ctx, "/projects/"+pid+"/labels")
		for _, l := range ls {
			if l.ID == 0 {
				continue
			}
			addRes(inv, "project_label/"+pid+"/"+itoa(l.ID), l.Name, "gitlab:project_label", instance, pid+":"+itoa(l.ID))
		}
		return err
	})

	subList(run, fatal, "project hooks", pid, func() error {
		hs, err := glList[glHook](ctx, "/projects/"+pid+"/hooks")
		for _, h := range hs {
			if h.ID == 0 {
				continue
			}
			addRes(inv, "project_hook/"+pid+"/"+itoa(h.ID), hookName(h), "gitlab:project_hook", instance, pid+":"+itoa(h.ID))
		}
		return err
	})

	subList(run, fatal, "deploy keys", pid, func() error {
		ks, err := glList[glDeployKey](ctx, "/projects/"+pid+"/deploy_keys")
		for _, k := range ks {
			if k.ID == 0 {
				continue
			}
			addRes(inv, "deploy_key/"+pid+"/"+itoa(k.ID), k.Title, "gitlab:deploy_key", instance, pid+":"+itoa(k.ID))
		}
		return err
	})

	subList(run, fatal, "protected branches", pid, func() error {
		bs, err := glList[glProtected](ctx, "/projects/"+pid+"/protected_branches")
		for _, b := range bs {
			if b.Name == "" {
				continue
			}
			addRes(inv, "branch_protection/"+pid+"/"+b.Name, b.Name, "gitlab:branch_protection", instance, pid+":"+b.Name)
		}
		return err
	})

	subList(run, fatal, "protected tags", pid, func() error {
		ts, err := glList[glProtected](ctx, "/projects/"+pid+"/protected_tags")
		for _, tg := range ts {
			if tg.Name == "" {
				continue
			}
			addRes(inv, "tag_protection/"+pid+"/"+tg.Name, tg.Name, "gitlab:tag_protection", instance, pid+":"+tg.Name)
		}
		return err
	})

	subList(run, fatal, "project members", pid, func() error {
		ms, err := glList[glMember](ctx, "/projects/"+pid+"/members")
		for _, m := range ms {
			if m.ID == 0 {
				continue
			}
			addRes(inv, "project_membership/"+pid+"/"+itoa(m.ID), m.Username, "gitlab:project_membership", instance, pid+":"+itoa(m.ID))
		}
		return err
	})

	subList(run, fatal, "project milestones", pid, func() error {
		mss, err := glList[glMilestone](ctx, "/projects/"+pid+"/milestones")
		for _, ms := range mss {
			if ms.ID == 0 {
				continue
			}
			addRes(inv, "project_milestone/"+pid+"/"+itoa(ms.ID), ms.Title, "gitlab:project_milestone", instance, pid+":"+itoa(ms.ID))
		}
		return err
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

// envScope defaults an empty CI/CD variable environment scope to GitLab's wildcard "*".
func envScope(s string) string {
	if s == "" {
		return "*"
	}
	return s
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// hookName labels a webhook by its URL host (the token/secret fields are never decoded).
func hookName(h glHook) string {
	if h.URL != "" {
		return h.URL
	}
	return "hook-" + itoa(h.ID)
}

// addRes adds a resource whose import id is the precomputed composite. The property is named
// "importID" (NOT "token") deliberately: GitLab is full of real tokens (PATs, hook tokens), so a
// path/id field must not borrow that name — it never carries a credential.
func addRes(inv *model.Inventory, id, name, native, instance, importID string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: instance, Source: "gitlab-api", Properties: map[string]any{"importID": importID},
	}
}

// list runs a best-effort ROOT enumeration closure and classifies any error: 401 → the token was
// revoked/expired, record it fatal; 403/404 → skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *gitlabAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (forbidden/absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("gitlab authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-parent fan-out variant: 401 → still fatal; 403/404 → Verbose skip (the token
// lacks access or the feature is absent); other → Warn. It does NOT increment hardFails (sub-lists
// multiply by group/project count).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *gitlabAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("gitlab authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes (secret fields are deliberately NOT decoded) -------

type glGroup struct {
	ID       int    `json:"id"`
	FullPath string `json:"full_path"`
	Name     string `json:"name"`
}

type glProject struct {
	ID                int             `json:"id"`
	PathWithNamespace string          `json:"path_with_namespace"`
	Name              string          `json:"name"`
	SharedWithGroups  []glSharedGroup `json:"shared_with_groups"`
}

type glSharedGroup struct {
	GroupID   int    `json:"group_id"`
	GroupName string `json:"group_name"`
}

// glVariable decodes ONLY the key + environment scope — the `value` (the secret) is never pulled.
type glVariable struct {
	Key              string `json:"key"`
	EnvironmentScope string `json:"environment_scope"`
}

type glLabel struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// glHook decodes only id + url — the `token`, `custom_headers`, and `url_variables` are never pulled.
type glHook struct {
	ID  int    `json:"id"`
	URL string `json:"url"`
}

type glMember struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

// glDeployKey decodes id + title — the `key` (a public key) is not needed for the import id.
type glDeployKey struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type glProtected struct {
	Name string `json:"name"`
}

type glMilestone struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type glLdapLink struct {
	Provider string `json:"provider"`
	CN       string `json:"cn"`
	Filter   string `json:"filter"`
}
