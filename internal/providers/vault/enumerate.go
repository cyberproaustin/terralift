package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Vault server: the CONFIG plane only — secret-engine mounts,
// auth-method mounts, ACL policies, audit devices, namespaces (Enterprise), and the safe backend
// ROLES (pki/database/aws secret roles; jwt/approle/token auth roles). It NEVER reads secret DATA
// (KV contents, dynamic credentials, root/unseal keys) — reading a secret value would leak it into
// config/state. The spine is the sys/* backbone (mounts + auth), then a per-mount role fan-out keyed
// on the mount's type. Best-effort per list: 401 → fatal; 404 → skip (an empty LIST directory 404s);
// 403 → skip on a leaf, but counted on the sys/* backbone so a fully-blind token surfaces as
// systemic rather than silently empty; other → Warn + count. The token never appears in errors/logs.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	if vAddr() == "" {
		return nil, fmt.Errorf("vault: VAULT_ADDR is malformed (must be an http/https URL)")
	}
	server := run.Scope.ID
	run.Log.Info("Enumerate", "Vault API: server=%s", server)

	inv := &model.Inventory{
		Cloud:       "vault",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{server: {ID: server, Name: server, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Secret-engine mounts (map-keyed sys/mounts). Skip the built-in system mounts by type; capture
	// the user mounts + their types for the role fan-out.
	secretMounts := map[string]string{} // path (no trailing slash) -> type
	list(run, &hardFails, &fatal, "secret engine mounts", true, func() error {
		mounts, err := vGetMounts(ctx, "/v1/sys/mounts")
		for path, m := range mounts {
			if isSystemMount(m.Type) {
				continue
			}
			p := strings.TrimSuffix(path, "/")
			addBare(inv, "mount/"+p, p, "vault:mount", server, p)
			secretMounts[p] = m.Type
		}
		return err
	})

	// Auth-method mounts (map-keyed sys/auth). The built-in token/ auth mount is not manageable via
	// vault_auth_backend (skip its emission) but its roles ARE adoptable — capture it too.
	authMounts := map[string]string{} // path (no trailing slash) -> type
	list(run, &hardFails, &fatal, "auth method mounts", true, func() error {
		auths, err := vGetMounts(ctx, "/v1/sys/auth")
		for path, m := range auths {
			p := strings.TrimSuffix(path, "/")
			authMounts[p] = m.Type
			if m.Type == "token" {
				continue // built-in, always enabled — not a vault_auth_backend resource
			}
			addBare(inv, "auth/"+p, p, "vault:auth_backend", server, p)
		}
		return err
	})

	// ACL policies (LIST keys). Skip the built-in root/default.
	list(run, &hardFails, &fatal, "acl policies", false, func() error {
		names, err := vList(ctx, "/v1/sys/policies/acl")
		for _, name := range names {
			n := strings.TrimSuffix(name, "/")
			if n == "root" || n == "default" || n == "" {
				continue
			}
			addBare(inv, "policy/"+n, n, "vault:policy", server, n)
		}
		return err
	})

	// Audit devices (map-keyed sys/audit; requires a sudo-capable token — 403 → quiet skip).
	list(run, &hardFails, &fatal, "audit devices", false, func() error {
		auds, err := vGetMounts(ctx, "/v1/sys/audit")
		for path := range auds {
			p := strings.TrimSuffix(path, "/")
			addBare(inv, "audit/"+p, p, "vault:audit", server, p)
		}
		return err
	})

	// Namespaces (Enterprise only — OSS returns 404 → quiet skip).
	list(run, &hardFails, &fatal, "namespaces", false, func() error {
		names, err := vList(ctx, "/v1/sys/namespaces")
		for _, name := range names {
			n := strings.TrimSuffix(name, "/")
			if n == "" {
				continue
			}
			addBare(inv, "namespace/"+n, n, "vault:namespace", server, n)
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}

	// Secret-engine role fan-out — one LIST per pki/database/aws mount. Import id = <backend>/roles/<name>.
	for path, typ := range secretMounts {
		if fatal != nil {
			break
		}
		native, ok := secretRoleNative(typ)
		if !ok {
			continue
		}
		p := path
		subList(run, &fatal, "roles", p, func() error {
			names, err := vList(ctx, "/v1/"+p+"/roles")
			for _, name := range names {
				n := strings.TrimSuffix(name, "/")
				if n == "" {
					continue
				}
				importID := p + "/roles/" + n
				addBare(inv, native+"/"+importID, n, native, server, importID)
			}
			return err
		})
	}

	// Auth-method role fan-out — jwt/oidc + approle use auth/<backend>/role/<name>; token uses the
	// odd auth/token/roles/<name>. Import id matches the LIST path shape.
	for path, typ := range authMounts {
		if fatal != nil {
			break
		}
		p := path
		switch {
		case typ == "jwt" || typ == "oidc":
			subList(run, &fatal, "jwt roles", p, func() error {
				return listAuthRoles(ctx, inv, server, "/v1/auth/"+p+"/role", "auth/"+p+"/role/", "vault:jwt_auth_backend_role")
			})
		case typ == "approle":
			subList(run, &fatal, "approle roles", p, func() error {
				return listAuthRoles(ctx, inv, server, "/v1/auth/"+p+"/role", "auth/"+p+"/role/", "vault:approle_auth_backend_role")
			})
		case typ == "token":
			subList(run, &fatal, "token roles", p, func() error {
				return listAuthRoles(ctx, inv, server, "/v1/auth/token/roles", "auth/token/roles/", "vault:token_auth_backend_role")
			})
		}
	}

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("vault enumeration failed on %d resource type(s) and found nothing — check VAULT_ADDR/VAULT_TOKEN and the token's policy (sys/mounts+sys/auth read)", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// listAuthRoles LISTs an auth-backend role path and adds each role with the given native type and
// import-id prefix (prefix already ends in '/'; the role name is appended).
func listAuthRoles(ctx context.Context, inv *model.Inventory, server, listPath, importPrefix, native string) error {
	names, err := vList(ctx, listPath)
	for _, name := range names {
		n := strings.TrimSuffix(name, "/")
		if n == "" {
			continue
		}
		importID := importPrefix + n
		addBare(inv, native+"/"+importID, n, native, server, importID)
	}
	return err
}

// secretRoleNative maps a secret-engine mount type to the native role key we fan out for it (empty/
// false for engines whose roles are deferred or which have none).
func secretRoleNative(mountType string) (string, bool) {
	switch mountType {
	case "pki":
		return "vault:pki_secret_backend_role", true
	case "database":
		return "vault:database_secret_backend_role", true
	case "aws":
		return "vault:aws_secret_backend_role", true
	}
	return "", false
}

// isSystemMount reports whether a secret mount is one of Vault's built-in system mounts (by type),
// which are not user-managed resources — sys/, identity/, cubbyhole/ and their namespace variants.
func isSystemMount(mountType string) bool {
	switch mountType {
	case "system", "identity", "cubbyhole", "ns_system", "ns_identity", "ns_cubbyhole":
		return true
	}
	return false
}

// addBare adds a resource whose import id is the precomputed path. The property is named
// "importID" (NOT "token") deliberately: in a secrets provider "token" means VAULT_TOKEN, so a
// path-holding field must not borrow that name — it never carries a credential.
func addBare(inv *model.Inventory, id, name, native, server, importID string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: server, Source: "vault-api", Properties: map[string]any{"importID": importID},
	}
}

// list runs a best-effort top-level enumeration closure and classifies any error. 401 → the token
// was revoked/expired, record it fatal. 404 → quiet skip (an empty LIST directory or an OSS-absent
// feature 404s). 403 → skip on a soft list, but on the sys/* backbone (core=true: mounts, auth) a
// permission-denied is a real gap counted toward hardFails so a fully-blind token surfaces via the
// systemic guard rather than shipping empty. Anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, core bool, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *vaultAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("vault authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		case 404:
			run.Log.Verbose("Enumerate", "list %s skipped (empty/absent): %v", what, err)
			return
		case 403:
			if !core {
				run.Log.Verbose("Enumerate", "list %s skipped (permission denied): %v", what, err)
				return
			}
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-mount role fan-out variant: 401 → still fatal; 403/404 → Verbose skip (the
// mount has no roles or the token can't read them); other → Warn. It does NOT increment hardFails
// (sub-lists multiply by mount count).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *vaultAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("vault authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}
