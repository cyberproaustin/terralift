package azuread

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Entra ID tenant over Microsoft Graph: groups, application
// registrations, service principals, named locations, conditional-access policies, administrative
// units, and directory-role assignments (top-level lists), plus two relationship fan-outs
// (per-group members, per-SP app-role assignments). One flat container = the tenant. Best-effort per
// list: 401 (after adDo's refresh) → fatal; 403/404 → Verbose skip (Graph is permission-scoped, so
// a missing app-role → 403 is expected); other → Warn + count. The bearer/secret never appear in
// errors/logs. Application/SP secret credentials are never decoded; users are deferred (PII/scale).
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	tenant := run.Scope.ID
	run.Log.Info("Enumerate", "Microsoft Graph: tenant=%s", tenant)

	inv := &model.Inventory{
		Cloud:       "azuread",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{tenant: {ID: tenant, Name: tenant, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Groups — capture ids for the per-group member fan-out. Import /groups/<id>.
	var groups []adObj
	list(run, &hardFails, &fatal, "groups", func() error {
		gs, err := gGraphList[adObj](ctx, "/groups")
		groups = gs
		for _, g := range gs {
			if g.ID == "" {
				continue
			}
			addRes(inv, "group/"+g.ID, g.label(), "azuread:group", tenant, "/groups/"+g.ID)
		}
		return err
	})

	// Application registrations — import /applications/<id> (the object id, NOT appId).
	list(run, &hardFails, &fatal, "applications", func() error {
		as, err := gGraphList[adObj](ctx, "/applications")
		for _, a := range as {
			if a.ID == "" {
				continue
			}
			addRes(inv, "application/"+a.ID, a.label(), "azuread:application", tenant, "/applications/"+a.ID)
		}
		return err
	})

	// Service principals — capture ids for the per-SP app-role-assignment fan-out. Import
	// /servicePrincipals/<id>.
	var sps []adObj
	list(run, &hardFails, &fatal, "service principals", func() error {
		ss, err := gGraphList[adObj](ctx, "/servicePrincipals")
		sps = ss
		for _, s := range ss {
			if s.ID == "" {
				continue
			}
			addRes(inv, "service_principal/"+s.ID, s.label(), "azuread:service_principal", tenant, "/servicePrincipals/"+s.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "named locations", func() error {
		ns, err := gGraphList[adObj](ctx, "/identity/conditionalAccess/namedLocations")
		for _, n := range ns {
			if n.ID == "" {
				continue
			}
			addRes(inv, "named_location/"+n.ID, n.label(), "azuread:named_location", tenant, "/identity/conditionalAccess/namedLocations/"+n.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "conditional access policies", func() error {
		ps, err := gGraphList[adObj](ctx, "/identity/conditionalAccess/policies")
		for _, p := range ps {
			if p.ID == "" {
				continue
			}
			addRes(inv, "conditional_access_policy/"+p.ID, p.label(), "azuread:conditional_access_policy", tenant, "/identity/conditionalAccess/policies/"+p.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "administrative units", func() error {
		us, err := gGraphList[adObj](ctx, "/directory/administrativeUnits")
		for _, u := range us {
			if u.ID == "" {
				continue
			}
			addRes(inv, "administrative_unit/"+u.ID, u.label(), "azuread:administrative_unit", tenant, "/directory/administrativeUnits/"+u.ID)
		}
		return err
	})

	// Directory-role assignments — import by a BARE opaque id (no prefix). (The directory ROLE
	// itself is not importable, so we adopt the assignments.)
	list(run, &hardFails, &fatal, "directory role assignments", func() error {
		ras, err := gGraphList[adObj](ctx, "/roleManagement/directory/roleAssignments")
		for _, ra := range ras {
			if ra.ID == "" {
				continue
			}
			addRes(inv, "directory_role_assignment/"+ra.ID, ra.ID, "azuread:directory_role_assignment", tenant, ra.ID)
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}

	// Fan-out: group members — import <group_id>/member/<member_id> (NO leading slash).
	for _, g := range groups {
		if g.ID == "" || fatal != nil {
			continue
		}
		gid := g.ID
		subList(run, &fatal, "group members", gid, func() error {
			ms, err := gGraphList[adObj](ctx, "/groups/"+neturl.PathEscape(gid)+"/members")
			for _, m := range ms {
				if m.ID == "" {
					continue
				}
				addRes(inv, "group_member/"+gid+"/"+m.ID, m.label(), "azuread:group_member", tenant, gid+"/member/"+m.ID)
			}
			return err
		})
	}

	// Fan-out: app-role assignments — import /servicePrincipals/<sp_id>/appRoleAssignedTo/<id>.
	for _, s := range sps {
		if s.ID == "" || fatal != nil {
			continue
		}
		spid := s.ID
		subList(run, &fatal, "app role assignments", spid, func() error {
			as, err := gGraphList[adObj](ctx, "/servicePrincipals/"+neturl.PathEscape(spid)+"/appRoleAssignedTo")
			for _, a := range as {
				if a.ID == "" {
					continue
				}
				addRes(inv, "app_role_assignment/"+spid+"/"+a.ID, a.label(), "azuread:app_role_assignment", tenant, "/servicePrincipals/"+spid+"/appRoleAssignedTo/"+a.ID)
			}
			return err
		})
	}

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("azuread enumeration failed on %d resource type(s) and found nothing — check the ARM_* credentials and the app's Graph API permissions (Directory.Read.All etc.)", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// addRes adds a resource whose import id is the precomputed Graph-path/composite string. The
// property is named "importID" (NOT "token"): the auth credential is an OAuth secret, so an id/path
// field must not borrow that name — it never carries a credential.
func addRes(inv *model.Inventory, id, name, native, tenant, importID string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: tenant, Source: "azuread-graph", Properties: map[string]any{"importID": importID},
	}
}

// list runs a best-effort top-level enumeration closure and classifies any error: 401 → the token
// was revoked (adDo already tried a refresh) so it is fatal; 403/404 → the app lacks the permission
// or the feature is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *azureadAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403:
			// Permission denied on a top-level root — stay a quiet skip, but COUNT it so that an
			// app with no Graph permissions (every root 403s) trips the systemic guard with an
			// actionable "check permissions" error instead of returning a silent empty inventory.
			run.Log.Verbose("Enumerate", "list %s skipped (permission absent): %v", what, err)
			*fails++
			return
		case 404:
			// Feature genuinely absent (e.g. a plane not enabled) — quiet skip, do NOT count.
			run.Log.Verbose("Enumerate", "list %s skipped (feature absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("azuread authentication failed during enumeration (token could not be refreshed): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-parent fan-out variant: 401 → still fatal; 403/404 → Verbose skip; other →
// Warn. It does NOT increment hardFails (sub-lists multiply by group/SP count).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *azureadAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("azuread authentication failed during enumeration (token could not be refreshed): %w", err)
			}
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// adObj is a flexible Graph object: the id (used for the import path) + a display label. Secret
// fields (passwordCredentials/keyCredentials) are deliberately NOT decoded.
type adObj struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"displayName"`
	PrincipalDisplayName string `json:"principalDisplayName"`
}

func (o adObj) label() string {
	for _, v := range []string{o.DisplayName, o.PrincipalDisplayName} {
		if v != "" {
			return v
		}
	}
	return o.ID
}
