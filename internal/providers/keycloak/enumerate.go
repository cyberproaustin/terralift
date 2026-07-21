package keycloak

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Keycloak server: the realm config core (realms,
// clients, roles, groups, client scopes, authentication flows, identity providers, LDAP
// federations, required actions). One flat container = the server. The spine is a REALM fan-out
// (GET /admin/realms → per-realm sub-lists), with a second-level per-(realm, client) fan-out for
// client roles. Best-effort per list: 401 → fatal (after kcDo's refresh-retry); 403/404 →
// Verbose skip (admin-role/feature absent); other → Warn + count. Secrets never appear in
// errors/logs.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	if kcBase() == "" {
		return nil, fmt.Errorf("keycloak: KEYCLOAK_URL is malformed (must be an http/https URL)")
	}
	server := run.Scope.ID
	run.Log.Info("Enumerate", "Keycloak Admin API: server=%s", server)

	inv := &model.Inventory{
		Cloud:       "keycloak",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{server: {ID: server, Name: server, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Parent: realms (skip the built-in master realm).
	var realms []kcRealm
	list(run, &hardFails, &fatal, "realms", func() error {
		rs, err := kcGet[kcRealm](ctx, "/admin/realms")
		realms = rs
		for _, r := range rs {
			if r.Realm == "" || r.Realm == "master" {
				continue
			}
			addBare(inv, "realm/"+r.Realm, r.Realm, "keycloak:realm", server, r.Realm)
		}
		return err
	})

	for _, r := range realms {
		if r.Realm == "" || r.Realm == "master" || fatal != nil {
			continue
		}
		enumRealm(ctx, run, inv, server, r.Realm, &fatal)
	}

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("keycloak enumeration failed on %d resource type(s) and found nothing — check the Keycloak credentials and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// enumRealm fans out one realm: clients (+ the two-level per-client roles fan-out), realm roles,
// groups (tree), client scopes, authentication flows, identity providers, LDAP federations, and
// required actions.
func enumRealm(ctx context.Context, run *core.Run, inv *model.Inventory, server, realm string, fatal *error) {
	re := neturl.PathEscape(realm)

	// Clients (paged) — discriminate on protocol; skip Keycloak's built-in clients. Capture the
	// non-built-in client UUIDs for the client-role fan-out.
	var clients []kcClient
	subList(run, fatal, "clients", realm, func() error {
		cs, err := kcList[kcClient](ctx, "/admin/realms/"+re+"/clients")
		clients = cs
		for _, c := range cs {
			if c.ID == "" || builtInClient(c.ClientID) {
				continue
			}
			native := clientNative(c.Protocol)
			if native == "" {
				continue
			}
			addPair(inv, "client/"+realm+"/"+c.ID, orName(c.ClientID, c.ID), native, server, realm, c.ID)
		}
		return err
	})
	// Client roles (two-level fan-out; import stays 2-part <realm>/<role_id>).
	for _, c := range clients {
		if c.ID == "" || builtInClient(c.ClientID) || *fatal != nil {
			continue
		}
		cid := c.ID
		subList(run, fatal, "client roles", realm+"/"+cid, func() error {
			rs, err := kcList[kcIDName](ctx, "/admin/realms/"+re+"/clients/"+neturl.PathEscape(cid)+"/roles")
			for _, role := range rs {
				if role.ID == "" {
					continue
				}
				addPair(inv, "role/"+realm+"/"+role.ID, orName(role.Name, role.ID), "keycloak:role", server, realm, role.ID)
			}
			return err
		})
	}

	// Realm roles (paged).
	subList(run, fatal, "realm roles", realm, func() error {
		rs, err := kcList[kcIDName](ctx, "/admin/realms/"+re+"/roles")
		for _, role := range rs {
			if role.ID == "" {
				continue
			}
			addPair(inv, "role/"+realm+"/"+role.ID, orName(role.Name, role.ID), "keycloak:role", server, realm, role.ID)
		}
		return err
	})

	// Groups (paged, tree — recursively flatten subGroups).
	subList(run, fatal, "groups", realm, func() error {
		gs, err := kcList[kcGroup](ctx, "/admin/realms/"+re+"/groups")
		var flat []kcGroup
		flattenGroups(gs, &flat)
		for _, g := range flat {
			if g.ID == "" {
				continue
			}
			addPair(inv, "group/"+realm+"/"+g.ID, orName(g.Name, g.ID), "keycloak:group", server, realm, g.ID)
		}
		return err
	})

	// Client scopes (unpaged) — openid-connect only (saml scope deferred).
	subList(run, fatal, "client scopes", realm, func() error {
		scs, err := kcGet[kcClientScope](ctx, "/admin/realms/"+re+"/client-scopes")
		for _, sc := range scs {
			if sc.ID == "" || clientScopeNative(sc.Protocol) == "" {
				continue
			}
			addPair(inv, "client_scope/"+realm+"/"+sc.ID, orName(sc.Name, sc.ID), "keycloak:openid_client_scope", server, realm, sc.ID)
		}
		return err
	})

	// Authentication flows (unpaged) — skip built-in.
	subList(run, fatal, "authentication flows", realm, func() error {
		fs, err := kcGet[kcFlow](ctx, "/admin/realms/"+re+"/authentication/flows")
		for _, f := range fs {
			if f.ID == "" || f.BuiltIn {
				continue
			}
			addPair(inv, "flow/"+realm+"/"+f.ID, orName(f.Alias, f.ID), "keycloak:authentication_flow", server, realm, f.ID)
		}
		return err
	})

	// Identity providers (unpaged) — discriminate on providerId (oidc/saml; social deferred).
	subList(run, fatal, "identity providers", realm, func() error {
		idps, err := kcGet[kcIdP](ctx, "/admin/realms/"+re+"/identity-provider/instances")
		for _, i := range idps {
			if i.Alias == "" {
				continue
			}
			native := idpNative(i.ProviderID)
			if native == "" {
				continue
			}
			addPair(inv, "idp/"+realm+"/"+i.Alias, i.Alias, native, server, realm, i.Alias)
		}
		return err
	})

	// LDAP user federations (unpaged) — filter providerId==ldap.
	subList(run, fatal, "user federations", realm, func() error {
		comps, err := kcGet[kcComponent](ctx, "/admin/realms/"+re+"/components?type=org.keycloak.storage.UserStorageProvider")
		for _, comp := range comps {
			if comp.ID == "" || comp.ProviderID != "ldap" {
				continue
			}
			addPair(inv, "federation/"+realm+"/"+comp.ID, orName(comp.Name, comp.ID), "keycloak:ldap_user_federation", server, realm, comp.ID)
		}
		return err
	})

	// Required actions (unpaged; alias-leaf).
	subList(run, fatal, "required actions", realm, func() error {
		ras, err := kcGet[kcRequiredAction](ctx, "/admin/realms/"+re+"/authentication/required-actions")
		for _, ra := range ras {
			if ra.Alias == "" {
				continue
			}
			addPair(inv, "required_action/"+realm+"/"+ra.Alias, orName(ra.Name, ra.Alias), "keycloak:required_action", server, realm, ra.Alias)
		}
		return err
	})
}

// flattenGroups recursively flattens the Keycloak group tree (subGroups) into a flat slice; each
// nested group is still a flat 2-part <realm>/<group_id> import.
func flattenGroups(gs []kcGroup, out *[]kcGroup) {
	for _, g := range gs {
		*out = append(*out, g)
		if len(g.SubGroups) > 0 {
			flattenGroups(g.SubGroups, out)
		}
	}
}

// clientNative maps a client's protocol to its TF type (empty protocol defaults to OIDC).
func clientNative(protocol string) string {
	switch protocol {
	case "", "openid-connect":
		return "keycloak:openid_client"
	case "saml":
		return "keycloak:saml_client"
	default:
		return ""
	}
}

// clientScopeNative maps a client scope's protocol — only OIDC scopes are Phase A (SAML deferred).
func clientScopeNative(protocol string) string {
	switch protocol {
	case "", "openid-connect":
		return "keycloak:openid_client_scope"
	default:
		return ""
	}
}

// idpNative maps an identity provider's providerId to its TF type (social IdPs deferred).
func idpNative(providerID string) string {
	switch providerID {
	case "oidc", "keycloak-oidc":
		return "keycloak:oidc_identity_provider"
	case "saml":
		return "keycloak:saml_identity_provider"
	default:
		return ""
	}
}

// builtInClient reports whether a clientId is one of Keycloak's built-in clients (present in
// every realm, not usefully adoptable) — the Okta saasure-skip analogue.
func builtInClient(clientID string) bool {
	switch clientID {
	case "account", "account-console", "admin-cli", "broker", "realm-management", "security-admin-console":
		return true
	}
	return false
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// addBare adds the realm resource (bare import id = the realm name).
func addBare(inv *model.Inventory, id, name, native, server, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: server, Source: "keycloak-api", Properties: map[string]any{"token": token},
	}
}

// addPair adds a realm-prefixed 2-part composite (<realm>/<leaf>).
func addPair(inv *model.Inventory, id, name, native, server, left, right string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: server, Source: "keycloak-api", Properties: map[string]any{"left": left, "right": right},
	}
}

// list runs a best-effort top-level enumeration closure and classifies any error: 401 → the
// token was revoked (kcDo already tried a refresh) so it is fatal; 403/404 → the admin role/
// feature is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *keycloakAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (admin-role/feature absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("keycloak authentication failed during enumeration (token could not be refreshed): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-parent fan-out variant: 403/404 → Verbose skip; 401 → still fatal; other →
// Warn. It does NOT increment hardFails (sub-lists multiply by realm/client count).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *keycloakAPIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == 403 || apiErr.Status == 404 {
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		}
		if apiErr.Status == 401 {
			if *fatal == nil {
				*fatal = fmt.Errorf("keycloak authentication failed during enumeration (token could not be refreshed): %w", err)
			}
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes (secret fields are deliberately NOT decoded) -------

type kcRealm struct {
	ID    string `json:"id"`
	Realm string `json:"realm"`
}

// kcClient decodes only id/clientId/protocol — the client_secret is never pulled.
type kcClient struct {
	ID       string `json:"id"`
	ClientID string `json:"clientId"`
	Protocol string `json:"protocol"`
}

type kcIDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type kcGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SubGroups []kcGroup `json:"subGroups"`
}

type kcClientScope struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
}

type kcFlow struct {
	ID      string `json:"id"`
	Alias   string `json:"alias"`
	BuiltIn bool   `json:"builtIn"`
}

// kcIdP decodes only alias/providerId — the config.clientSecret is never pulled.
type kcIdP struct {
	Alias      string `json:"alias"`
	ProviderID string `json:"providerId"`
}

// kcComponent decodes only id/name/providerId — the config.bindCredential is never pulled.
type kcComponent struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ProviderID string `json:"providerId"`
}

type kcRequiredAction struct {
	Alias string `json:"alias"`
	Name  string `json:"name"`
}
