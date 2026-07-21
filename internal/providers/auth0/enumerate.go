package auth0

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Auth0 tenant: the application/API/identity/RBAC config
// core (clients, resource servers, connections, roles, actions, organizations, client grants,
// log streams, email templates) plus the six tenant-wide settings singletons. One flat
// container = the tenant. Keyed+total lists are paged; log streams are a bare array; email
// templates are a fixed-name fan-out; the singletons are single-object GETs. Best-effort per
// list: 401 → fatal; 403/404 → Verbose skip (scope/feature absent); other → Warn + count. The
// client_secret/access_token never appear in errors/logs.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	tenant := run.Scope.ID
	run.Log.Info("Enumerate", "Auth0 Management API: tenant=%s", tenant)

	inv := &model.Inventory{
		Cloud:       "auth0",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{tenant: {ID: tenant, Name: tenant, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Clients — skip the global "all-applications" internal client.
	list(run, &hardFails, &fatal, "clients", func() error {
		cs, err := auth0List[auth0Client](ctx, "/api/v2/clients", "clients")
		for _, c := range cs {
			if c.Global || c.ClientID == "" {
				continue
			}
			addBare(inv, "client/"+c.ClientID, orName(c.Name, c.ClientID), "auth0:client", tenant, c.ClientID)
		}
		return err
	})

	// Resource servers — skip the read-only system "Auth0 Management API".
	list(run, &hardFails, &fatal, "resource servers", func() error {
		rss, err := auth0List[auth0ResourceServer](ctx, "/api/v2/resource-servers", "resource_servers")
		for _, rs := range rss {
			if rs.IsSystem || rs.ID == "" {
				continue
			}
			addBare(inv, "resource_server/"+rs.ID, orName(rs.Name, rs.ID), "auth0:resource_server", tenant, rs.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "connections", func() error {
		cs, err := auth0List[auth0IDName](ctx, "/api/v2/connections", "connections")
		for _, c := range cs {
			addBare(inv, "connection/"+c.ID, orName(c.Name, c.ID), "auth0:connection", tenant, c.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "roles", func() error {
		rs, err := auth0List[auth0IDName](ctx, "/api/v2/roles", "roles")
		for _, r := range rs {
			addBare(inv, "role/"+r.ID, orName(r.Name, r.ID), "auth0:role", tenant, r.ID)
		}
		return err
	})

	// Actions live at the doubled path /api/v2/actions/actions (the outer "actions" is the group).
	list(run, &hardFails, &fatal, "actions", func() error {
		as, err := auth0List[auth0IDName](ctx, "/api/v2/actions/actions", "actions")
		for _, a := range as {
			addBare(inv, "action/"+a.ID, orName(a.Name, a.ID), "auth0:action", tenant, a.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "organizations", func() error {
		orgs, err := auth0List[auth0Org](ctx, "/api/v2/organizations", "organizations")
		for _, o := range orgs {
			addBare(inv, "organization/"+o.ID, orName(orName(o.DisplayName, o.Name), o.ID), "auth0:organization", tenant, o.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "client grants", func() error {
		gs, err := auth0List[auth0ClientGrant](ctx, "/api/v2/client-grants", "client_grants")
		for _, g := range gs {
			addBare(inv, "client_grant/"+g.ID, g.ID, "auth0:client_grant", tenant, g.ID)
		}
		return err
	})

	// Log streams — the lone bare-array endpoint (no envelope, no pager).
	list(run, &hardFails, &fatal, "log streams", func() error {
		ls, err := auth0GetArray[auth0LogStream](ctx, "/api/v2/log-streams")
		for _, l := range ls {
			addBare(inv, "log_stream/"+l.ID, orName(l.Name, l.ID), "auth0:log_stream", tenant, l.ID)
		}
		return err
	})

	// Email templates — a fixed-name fan-out (no list endpoint); adopt those that exist.
	list(run, &hardFails, &fatal, "email templates", func() error {
		var otherErr error
		for _, name := range emailTemplateNames {
			_, _, err := auth0Do(ctx, http.MethodGet, "/api/v2/email-templates/"+name)
			if err == nil {
				addBare(inv, "email_template/"+name, name, "auth0:email_template", tenant, name)
				continue
			}
			var apiErr *auth0APIError
			if errors.As(err, &apiErr) {
				switch apiErr.Status {
				case 400, 404:
					continue // template not configured for this tenant
				case 401, 403:
					return err // whole-endpoint auth/scope issue — let list() classify (fatal/skip)
				}
			}
			otherErr = err
		}
		return otherErr
	})

	// Settings singletons — one object each, imported by a stable sentinel placeholder.
	for _, s := range singletons {
		s := s
		list(run, &hardFails, &fatal, s.what, func() error {
			if _, _, err := auth0Do(ctx, http.MethodGet, s.path); err != nil {
				return err
			}
			addBare(inv, s.token, s.token, s.native, tenant, s.token)
			return nil
		})
	}

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("auth0 enumeration failed on %d resource type(s) and found nothing — check the Auth0 credentials and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// emailTemplateNames are the fixed Auth0 email-template ids (there is no list endpoint).
var emailTemplateNames = []string{
	"verify_email", "verify_email_by_code", "reset_email", "reset_email_by_code",
	"welcome_email", "blocked_account", "stolen_credentials", "enrollment_email",
	"mfa_oob_code", "user_invitation", "change_password", "password_reset",
}

// singletons are the one-per-tenant settings resources. Each imports by a STABLE sentinel (the
// provider discards the supplied id and always reads the tenant-wide object) — a stable token
// keeps re-runs idempotent. attack_protection is probed via one of its three sub-objects.
var singletons = []struct {
	what, path, native, token string
}{
	{"tenant settings", "/api/v2/tenants/settings", "auth0:tenant", "tenant"},
	{"branding", "/api/v2/branding", "auth0:branding", "branding"},
	{"attack protection", "/api/v2/attack-protection/brute-force-protection", "auth0:attack_protection", "attack_protection"},
	{"prompts", "/api/v2/prompts", "auth0:prompt", "prompt"},
	{"guardian factors", "/api/v2/guardian/factors", "auth0:guardian", "guardian"},
	{"email provider", "/api/v2/emails/provider", "auth0:email_provider", "email_provider"},
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// addBare adds a resource whose import id is a bare token — an opaque id, a template name, or a
// singleton sentinel. Phase A has NO :: composites, so every resource routes through this.
func addBare(inv *model.Inventory, id, name, native, tenant, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: tenant, Source: "auth0-api", Properties: map[string]any{"token": token},
	}
}

// list runs a best-effort enumeration closure and classifies any error: 401 → the token was
// revoked/expired (or a mis-scoped audience), every remaining list will fail too, record it
// fatal; 403/404 → the read scope/feature is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *auth0APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (scope/feature absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("auth0 authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// --- API response shapes ---------------------------------------------------

type auth0Client struct {
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
	Global   bool   `json:"global"`
}

type auth0ResourceServer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IsSystem bool   `json:"is_system"`
}

type auth0IDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type auth0Org struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type auth0ClientGrant struct {
	ID string `json:"id"`
}

type auth0LogStream struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
