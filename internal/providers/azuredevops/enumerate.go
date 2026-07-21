package azuredevops

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Azure DevOps organization: the projects (root), their
// durable config children (git repositories, build definitions, variable-group shells, agent
// queues, teams, environments), and two org-level roots — agent pools and graph groups (on the
// separate vssps host). One flat container = the org. Best-effort per list: 401 (incl. the 203/HTML
// sign-in gotcha) → fatal; 404 → skip; other → Warn + count. The PAT never appears in errors/logs.
// The secret-bearing service-endpoint family and variable-group SECRET values are never enumerated.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	org := azOrgURL()
	if org == "" {
		return nil, fmt.Errorf("azuredevops: AZDO_ORG_SERVICE_URL is malformed or unset (need e.g. https://dev.azure.com/<org>)")
	}
	orgID := run.Scope.ID
	run.Log.Info("Enumerate", "Azure DevOps API: org=%s", orgID)

	inv := &model.Inventory{
		Cloud:       "azuredevops",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{orgID: {ID: orgID, Name: orgID, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Root: projects. Each is imported by its bare GUID; capture ids for the per-project fan-out.
	var projects []azProject
	list(run, &hardFails, &fatal, "projects", func() error {
		ps, err := azList[azProject](ctx, org, "/_apis/projects", apiV)
		projects = ps
		for _, p := range ps {
			if p.ID == "" {
				continue
			}
			addRes(inv, "project/"+p.ID, orName(p.Name, p.ID), "azuredevops:project", orgID, p.ID)
		}
		return err
	})

	// Org-level: agent pools (skip the Azure-hosted pools — not user-managed). Bare int import.
	list(run, &hardFails, &fatal, "agent pools", func() error {
		pools, err := azList[azPool](ctx, org, "/_apis/distributedtask/pools", apiV)
		for _, pl := range pools {
			if pl.ID == 0 || pl.IsHosted {
				continue
			}
			addRes(inv, "agent_pool/"+itoa(pl.ID), pl.Name, "azuredevops:agent_pool", orgID, itoa(pl.ID))
		}
		return err
	})

	// Org-level: graph groups (on the vssps host — bare descriptor import). Absent host / 403 → skip.
	list(run, &hardFails, &fatal, "groups", func() error {
		graph := azGraphURL()
		if graph == "" {
			return nil // non-standard host — groups skipped (Phase-B)
		}
		groups, err := azList[azGroup](ctx, graph, "/_apis/graph/groups", apiVPreview1)
		for _, g := range groups {
			if g.Descriptor == "" {
				continue
			}
			addRes(inv, "group/"+g.Descriptor, orName(g.PrincipalName, g.DisplayName), "azuredevops:group", orgID, g.Descriptor)
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}

	for _, p := range projects {
		if p.ID == "" || fatal != nil {
			continue
		}
		enumProject(ctx, run, inv, orgID, org, p.ID, &fatal)
	}

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("azuredevops enumeration failed on %d resource type(s) and found nothing — check AZDO_ORG_SERVICE_URL/AZDO_PERSONAL_ACCESS_TOKEN and the PAT's scope", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// enumProject fans out one project: git repositories + teams (UUID leaf), build definitions,
// variable-group shells, agent queues, and environments (int leaf). Import id = <projectGUID>/<leaf>.
func enumProject(ctx context.Context, run *core.Run, inv *model.Inventory, orgID, org, pid string, fatal *error) {
	subList(run, fatal, "git repositories", pid, func() error {
		rs, err := azList[azRepo](ctx, org, "/"+pid+"/_apis/git/repositories", apiV)
		for _, r := range rs {
			if r.ID == "" {
				continue
			}
			addRes(inv, "git_repository/"+pid+"/"+r.ID, r.Name, "azuredevops:git_repository", orgID, pid+"/"+r.ID)
		}
		return err
	})

	subList(run, fatal, "build definitions", pid, func() error {
		ds, err := azList[azDef](ctx, org, "/"+pid+"/_apis/build/definitions", apiV)
		for _, d := range ds {
			if d.ID == 0 {
				continue
			}
			addRes(inv, "build_definition/"+pid+"/"+itoa(d.ID), d.Name, "azuredevops:build_definition", orgID, pid+"/"+itoa(d.ID))
		}
		return err
	})

	// Variable groups — adopt the SHELL only; the secret variable values are never decoded.
	subList(run, fatal, "variable groups", pid, func() error {
		vgs, err := azList[azVarGroup](ctx, org, "/"+pid+"/_apis/distributedtask/variablegroups", apiVPreview2)
		for _, vg := range vgs {
			if vg.ID == 0 {
				continue
			}
			addRes(inv, "variable_group/"+pid+"/"+itoa(vg.ID), vg.Name, "azuredevops:variable_group", orgID, pid+"/"+itoa(vg.ID))
		}
		return err
	})

	subList(run, fatal, "agent queues", pid, func() error {
		// Agent queues have historically stayed on the preview api-version (VERIFY at Phase B).
		qs, err := azList[azQueue](ctx, org, "/"+pid+"/_apis/distributedtask/queues", apiVPreview1)
		for _, q := range qs {
			if q.ID == 0 {
				continue
			}
			addRes(inv, "agent_queue/"+pid+"/"+itoa(q.ID), q.Name, "azuredevops:agent_queue", orgID, pid+"/"+itoa(q.ID))
		}
		return err
	})

	subList(run, fatal, "teams", pid, func() error {
		ts, err := azList[azTeam](ctx, org, "/_apis/projects/"+pid+"/teams", apiV)
		for _, t := range ts {
			if t.ID == "" {
				continue
			}
			addRes(inv, "team/"+pid+"/"+t.ID, t.Name, "azuredevops:team", orgID, pid+"/"+t.ID)
		}
		return err
	})

	subList(run, fatal, "environments", pid, func() error {
		es, err := azList[azEnv](ctx, org, "/"+pid+"/_apis/distributedtask/environments", apiV)
		for _, e := range es {
			if e.ID == 0 {
				continue
			}
			addRes(inv, "environment/"+pid+"/"+itoa(e.ID), e.Name, "azuredevops:environment", orgID, pid+"/"+itoa(e.ID))
		}
		return err
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// addRes adds a resource whose import id is the precomputed bare/composite string. The property is
// named "importID" (NOT "token"): the auth credential is a PAT, so an id/path field must not borrow
// that name — it never carries a credential.
func addRes(inv *model.Inventory, id, name, native, orgID, importID string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: orgID, Source: "azuredevops-api", Properties: map[string]any{"importID": importID},
	}
}

// list runs a best-effort ROOT enumeration closure and classifies any error: 401 (incl. the
// 203/HTML sign-in gotcha normalized to 401) → the PAT was revoked/expired, record it fatal; 404 →
// skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *azdoAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (forbidden/absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("azuredevops authentication failed during enumeration (PAT revoked/expired or under-scoped): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-project fan-out variant: 401 → still fatal; 403/404 → Verbose skip; other →
// Warn. It does NOT increment hardFails (sub-lists multiply by project count).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *azdoAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("azuredevops authentication failed during enumeration (PAT revoked/expired): %w", err)
			}
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes (secret fields are deliberately NOT decoded) -------

type azProject struct {
	ID   string `json:"id"` // GUID
	Name string `json:"name"`
}

type azRepo struct {
	ID   string `json:"id"` // GUID
	Name string `json:"name"`
}

type azDef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// azVarGroup decodes ONLY id + name — the `variables` map (which carries secret values / Key Vault
// refs) is never pulled.
type azVarGroup struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type azQueue struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type azTeam struct {
	ID   string `json:"id"` // GUID
	Name string `json:"name"`
}

type azEnv struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type azPool struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	IsHosted bool   `json:"isHosted"`
}

type azGroup struct {
	Descriptor    string `json:"descriptor"`
	DisplayName   string `json:"displayName"`
	PrincipalName string `json:"principalName"`
}
