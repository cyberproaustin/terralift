package okta

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Okta org: the identity/access config core (users,
// groups + rules, user types, the signOnMode-discriminated app family, trusted origins,
// network zones, auth servers + the deepest fan-out, the ?type=-discriminated policies +
// rules, inline/event hooks, and the type-discriminated IdPs). One flat container = the org.
// Top-level lists use bare-array + Link-header pagination; per-parent fan-outs (auth_server →
// scopes/claims/policies → rules; policy → rules) reach the sub-resources. Best-effort per
// list: 401 → fatal; 403/404 → Verbose skip (role/feature absent); other → Warn + count. The
// SSWS token never appears in errors/logs; every Link rel="next" follow is host-validated.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	org := run.Scope.ID
	run.Log.Info("Enumerate", "Okta API: org=%s", org)

	inv := &model.Inventory{
		Cloud:       "okta",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{org: {ID: org, Name: org, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Users — the largest list; the resource NAME is the opaque id (not the login/email) to
	// keep user PII out of the generated resource address.
	list(run, &hardFails, &fatal, "users", func() error {
		us, err := oktaList[oktaUser](ctx, "/api/v1/users")
		for _, u := range us {
			addBare(inv, "user/"+u.ID, u.ID, "okta:user", org, u.ID)
		}
		return err
	})

	// Groups — filter to OKTA_GROUP server-side; guard BUILT_IN/APP_GROUP defensively.
	list(run, &hardFails, &fatal, "groups", func() error {
		gs, err := oktaList[oktaGroup](ctx, "/api/v1/groups?filter="+neturl.QueryEscape(`type eq "OKTA_GROUP"`))
		for _, g := range gs {
			if g.Type == "APP_GROUP" || g.Type == "BUILT_IN" {
				continue
			}
			addBare(inv, "group/"+g.ID, orName(g.Profile.Name, g.ID), "okta:group", org, g.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "group rules", func() error {
		rs, err := oktaList[oktaIDName](ctx, "/api/v1/groups/rules")
		for _, r := range rs {
			addBare(inv, "group_rule/"+r.ID, orName(r.Name, r.ID), "okta:group_rule", org, r.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "user types", func() error {
		ts, err := oktaList[oktaIDName](ctx, "/api/v1/meta/types/user")
		for _, t := range ts {
			addBare(inv, "user_type/"+t.ID, orName(t.Name, t.ID), "okta:user_type", org, t.ID)
		}
		return err
	})

	// Apps — ONE list, discriminated by signOnMode into seven TF types; skip Okta's own apps.
	list(run, &hardFails, &fatal, "apps", func() error {
		as, err := oktaList[oktaApp](ctx, "/api/v1/apps")
		for _, a := range as {
			if a.ID == "" {
				continue
			}
			if oktaOwnApp(a.Name) {
				run.Log.Verbose("Enumerate", "app %s (%s) skipped (Okta-managed app)", a.ID, a.Name)
				continue
			}
			native := appNative(a.SignOnMode, a.Name)
			if native == "" {
				run.Log.Verbose("Enumerate", "app %s has unmapped signOnMode %q — skipped", a.ID, a.SignOnMode)
				continue
			}
			addBare(inv, "app/"+a.ID, orName(a.Label, a.ID), native, org, a.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "trusted origins", func() error {
		ts, err := oktaList[oktaIDName](ctx, "/api/v1/trustedOrigins")
		for _, t := range ts {
			addBare(inv, "trusted_origin/"+t.ID, orName(t.Name, t.ID), "okta:trusted_origin", org, t.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "network zones", func() error {
		zs, err := oktaList[oktaIDName](ctx, "/api/v1/zones")
		for _, z := range zs {
			addBare(inv, "network_zone/"+z.ID, orName(z.Name, z.ID), "okta:network_zone", org, z.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "inline hooks", func() error {
		hs, err := oktaList[oktaIDName](ctx, "/api/v1/inlineHooks")
		for _, h := range hs {
			addBare(inv, "inline_hook/"+h.ID, orName(h.Name, h.ID), "okta:inline_hook", org, h.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "event hooks", func() error {
		hs, err := oktaList[oktaIDName](ctx, "/api/v1/eventHooks")
		for _, h := range hs {
			addBare(inv, "event_hook/"+h.ID, orName(h.Name, h.ID), "okta:event_hook", org, h.ID)
		}
		return err
	})

	// IdPs — discriminate on type (OIDC/SAML2); social/other deferred.
	list(run, &hardFails, &fatal, "idps", func() error {
		is, err := oktaList[oktaIdP](ctx, "/api/v1/idps")
		for _, i := range is {
			native := idpNative(i.Type)
			if native == "" {
				run.Log.Verbose("Enumerate", "idp %s type %q skipped (deferred/social)", i.ID, i.Type)
				continue
			}
			addBare(inv, "idp/"+i.ID, orName(i.Name, i.ID), native, org, i.ID)
		}
		return err
	})

	// Policies — ?type= is REQUIRED; loop the Phase-A types, each fanning out to its rules.
	for _, pt := range policyTypes {
		pt := pt
		list(run, &hardFails, &fatal, "policies "+pt.apiType, func() error {
			ps, err := oktaList[oktaIDName](ctx, "/api/v1/policies?type="+pt.apiType)
			for _, p := range ps {
				addBare(inv, "policy/"+p.ID, orName(p.Name, p.ID), pt.policyNative, org, p.ID)
				pid := p.ID
				subList(run, &fatal, "policy rules", pid, func() error {
					rs, rerr := oktaList[oktaIDName](ctx, "/api/v1/policies/"+neturl.PathEscape(pid)+"/rules")
					for _, r := range rs {
						addPair(inv, "policy_rule/"+pid+"/"+r.ID, orName(r.Name, r.ID), pt.ruleNative, org, pid, r.ID)
					}
					return rerr
				})
			}
			return err
		})
	}

	// Auth servers — the deepest fan-out: server → scopes/claims/policies, policy → rules.
	var authServers []oktaIDName
	list(run, &hardFails, &fatal, "auth servers", func() error {
		ss, err := oktaList[oktaIDName](ctx, "/api/v1/authorizationServers")
		authServers = ss
		for _, s := range ss {
			addBare(inv, "auth_server/"+s.ID, orName(s.Name, s.ID), "okta:auth_server", org, s.ID)
		}
		return err
	})
	for _, as := range authServers {
		if as.ID == "" || fatal != nil {
			continue
		}
		asid := as.ID
		enumAuthServer(ctx, run, inv, org, asid, &fatal)
	}

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("okta enumeration failed on %d resource type(s) and found nothing — check OKTA_ORG_NAME/OKTA_BASE_URL/OKTA_API_TOKEN and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// enumAuthServer fans out one authorization server's scopes/claims/policies, then each policy's
// rules — the 3-part <auth_server_id>/<policy_id>/<rule_id> composite.
func enumAuthServer(ctx context.Context, run *core.Run, inv *model.Inventory, org, asid string, fatal *error) {
	base := "/api/v1/authorizationServers/" + neturl.PathEscape(asid)

	subList(run, fatal, "auth server scopes", asid, func() error {
		scs, err := oktaList[oktaIDName](ctx, base+"/scopes")
		for _, sc := range scs {
			addPair(inv, "auth_server_scope/"+asid+"/"+sc.ID, orName(sc.Name, sc.ID), "okta:auth_server_scope", org, asid, sc.ID)
		}
		return err
	})
	subList(run, fatal, "auth server claims", asid, func() error {
		cls, err := oktaList[oktaIDName](ctx, base+"/claims")
		for _, cl := range cls {
			addPair(inv, "auth_server_claim/"+asid+"/"+cl.ID, orName(cl.Name, cl.ID), "okta:auth_server_claim", org, asid, cl.ID)
		}
		return err
	})

	var policies []oktaIDName
	subList(run, fatal, "auth server policies", asid, func() error {
		ps, err := oktaList[oktaIDName](ctx, base+"/policies")
		policies = ps
		for _, p := range ps {
			addPair(inv, "auth_server_policy/"+asid+"/"+p.ID, orName(p.Name, p.ID), "okta:auth_server_policy", org, asid, p.ID)
		}
		return err
	})
	for _, p := range policies {
		if p.ID == "" || *fatal != nil {
			continue
		}
		pid := p.ID
		subList(run, fatal, "auth server policy rules", asid+"/"+pid, func() error {
			rs, err := oktaList[oktaIDName](ctx, base+"/policies/"+neturl.PathEscape(pid)+"/rules")
			for _, r := range rs {
				addTriple(inv, "auth_server_policy_rule/"+asid+"/"+pid+"/"+r.ID, orName(r.Name, r.ID),
					"okta:auth_server_policy_rule", org, asid, pid, r.ID)
			}
			return err
		})
	}
}

var policyTypes = []struct {
	apiType      string
	policyNative string
	ruleNative   string
}{
	{"OKTA_SIGN_ON", "okta:policy_signon", "okta:policy_rule_signon"},
	{"PASSWORD", "okta:policy_password", "okta:policy_rule_password"},
	{"MFA_ENROLL", "okta:policy_mfa", "okta:policy_rule_mfa"},
}

// appNative maps an app's signOnMode (with a name sub-discriminator for BROWSER_PLUGIN) to the
// TF type. "" for an unmapped mode (WS_FEDERATION/etc.) → skipped.
func appNative(signOnMode, name string) string {
	switch signOnMode {
	case "OPENID_CONNECT":
		return "okta:app_oauth"
	case "SAML_2_0", "SAML_1_1":
		return "okta:app_saml"
	case "AUTO_LOGIN":
		return "okta:app_auto_login"
	case "BOOKMARK":
		return "okta:app_bookmark"
	case "BASIC_AUTH":
		return "okta:app_basic_auth"
	case "BROWSER_PLUGIN":
		if name == "template_swa3field" {
			return "okta:app_three_field"
		}
		return "okta:app_swa"
	case "SECURE_PASSWORD_STORE":
		return "okta:app_secure_password_store"
	default:
		return ""
	}
}

// oktaOwnApp reports whether an app is one of Okta's own admin/dashboard/template apps (not
// adoptable), by name — Terraformer's exact skip list.
func oktaOwnApp(name string) bool {
	switch name {
	case "saasure", "okta_enduser", "okta_browser_plugin", "template_wsfed", "template_swa_two_page":
		return true
	}
	return false
}

func idpNative(typ string) string {
	switch typ {
	case "OIDC":
		return "okta:idp_oidc"
	case "SAML2":
		return "okta:idp_saml"
	default:
		return ""
	}
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// addBare adds a resource whose import id is a bare opaque id.
func addBare(inv *model.Inventory, id, name, native, org, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: org, Source: "okta-api", Properties: map[string]any{"token": token},
	}
}

// addPair adds a 2-part slash composite (<parent>/<child>); left/right in import order.
func addPair(inv *model.Inventory, id, name, native, org, left, right string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: org, Source: "okta-api", Properties: map[string]any{"left": left, "right": right},
	}
}

// addTriple adds a 3-part slash composite (<a>/<b>/<c>, outermost-first); the lone
// okta_auth_server_policy_rule.
func addTriple(inv *model.Inventory, id, name, native, org, a, b, c string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: org, Source: "okta-api", Properties: map[string]any{"a": a, "b": b, "c": c},
	}
}

// list runs a best-effort top-level enumeration closure and classifies any error: 401 → the
// token was revoked/expired, every remaining list will fail too, record it fatal; 403/404 →
// the role/feature is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *oktaAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (role/feature absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("okta authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-parent fan-out variant: 403/404 → Verbose skip; 401 → still fatal; other
// → Warn. It does NOT increment hardFails (sub-lists multiply by parent count).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *oktaAPIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == 403 || apiErr.Status == 404 {
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		}
		if apiErr.Status == 401 {
			if *fatal == nil {
				*fatal = fmt.Errorf("okta authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes ---------------------------------------------------

type oktaUser struct {
	ID string `json:"id"`
}

type oktaGroup struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
}

type oktaApp struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	SignOnMode string `json:"signOnMode"`
}

type oktaIdP struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type oktaIDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
