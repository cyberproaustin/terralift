package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for a Cloudflare account: the zones (the spine),
// their per-zone sub-resources, and account-level resources. One flat container =
// the account. Every sub-resource list is best-effort — a plan without a feature
// (e.g. no Load Balancing subscription) 403/404s its endpoint, which is logged at
// Verbose and skipped, not fatal.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	acct := run.Scope.ID
	run.Log.Info("Enumerate", "Cloudflare API: account=%s", acct)

	inv := &model.Inventory{
		Cloud:       "cloudflare",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{acct: {ID: acct, Name: acct, Type: model.ScopeTenant}},
	}

	zones, err := cfList[cfZone](ctx, "/zones?account.id="+acct)
	if err != nil {
		return nil, err
	}
	run.Log.Info("Enumerate", "zones: %d", len(zones))
	for i := range zones {
		enumZone(ctx, run, inv, acct, zones[i])
	}
	enumAccount(ctx, run, inv, acct)

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// add records a resource, resolving its Terraform type. Keyed by the raw id (kind
// namespaces it, so ids are unique and case is preserved).
func add(inv *model.Inventory, r *model.Resource) {
	r.TFType = tfType(r.NativeType)
	inv.Resources[r.ID] = r
}

// --- zone-scoped -----------------------------------------------------------

func enumZone(ctx context.Context, run *core.Run, inv *model.Inventory, acct string, z cfZone) {
	zid, zname := z.ID, z.Name

	add(inv, &model.Resource{
		ID: "zone/" + zid, Name: zname, NativeType: "cloudflare:zone", Container: acct, Source: "cf-api",
		Properties: map[string]any{"zone_id": zid, "name": zname, "account_id": acct},
	})
	// zone_settings_override is a singleton whose Terraform id IS the zone id.
	add(inv, &model.Resource{
		ID: "zone_settings/" + zid, Name: zname + "-settings", NativeType: "cloudflare:zone_settings", Container: acct, Source: "cf-api",
		Properties: map[string]any{"zone_id": zid},
	})

	list(run, "records", zname, func() error {
		recs, err := cfList[cfRecord](ctx, fmt.Sprintf("/zones/%s/dns_records", zid))
		for _, r := range recs {
			add(inv, &model.Resource{
				ID: "record/" + r.ID, Name: r.Name + "-" + r.Type, NativeType: "cloudflare:record", Container: acct, Source: "cf-api",
				Properties: map[string]any{"zone_id": zid, "record_id": r.ID, "type": r.Type, "name": r.Name},
			})
		}
		return err
	})

	list(run, "page rules", zname, func() error {
		prs, err := cfList[cfNamed](ctx, fmt.Sprintf("/zones/%s/pagerules", zid))
		for _, pr := range prs {
			add(inv, &model.Resource{
				ID: "page_rule/" + pr.ID, Name: zname + "-pagerule", NativeType: "cloudflare:page_rule", Container: acct, Source: "cf-api",
				Properties: map[string]any{"zone_id": zid, "page_rule_id": pr.ID},
			})
		}
		return err
	})

	list(run, "rulesets", zname, func() error {
		rs, err := cfList[cfRuleset](ctx, fmt.Sprintf("/zones/%s/rulesets", zid))
		for _, r := range rs {
			if r.Kind == "managed" {
				continue // Cloudflare-owned, read-only; not adoptable
			}
			add(inv, &model.Resource{
				ID: "ruleset/" + r.ID, Name: zname + "-ruleset-" + r.Phase, NativeType: "cloudflare:ruleset", Container: acct, Source: "cf-api",
				Properties: map[string]any{"scope": "zone", "parent_id": zid, "ruleset_id": r.ID, "kind": r.Kind, "phase": r.Phase},
			})
		}
		return err
	})

	list(run, "filters", zname, func() error {
		fs, err := cfList[cfNamed](ctx, fmt.Sprintf("/zones/%s/filters", zid))
		for _, f := range fs {
			add(inv, &model.Resource{
				ID: "filter/" + f.ID, Name: zname + "-filter", NativeType: "cloudflare:filter", Container: acct, Source: "cf-api",
				Properties: map[string]any{"zone_id": zid, "filter_id": f.ID},
			})
		}
		return err
	})

	list(run, "firewall rules", zname, func() error {
		frs, err := cfList[cfNamed](ctx, fmt.Sprintf("/zones/%s/firewall/rules", zid))
		for _, f := range frs {
			add(inv, &model.Resource{
				ID: "firewall_rule/" + f.ID, Name: zname + "-firewall-rule", NativeType: "cloudflare:firewall_rule", Container: acct, Source: "cf-api",
				Properties: map[string]any{"zone_id": zid, "firewall_rule_id": f.ID},
			})
		}
		return err
	})

	list(run, "lockdowns", zname, func() error {
		lds, err := cfList[cfNamed](ctx, fmt.Sprintf("/zones/%s/firewall/lockdowns", zid))
		for _, l := range lds {
			add(inv, &model.Resource{
				ID: "zone_lockdown/" + l.ID, Name: zname + "-lockdown", NativeType: "cloudflare:zone_lockdown", Container: acct, Source: "cf-api",
				Properties: map[string]any{"zone_id": zid, "lockdown_id": l.ID},
			})
		}
		return err
	})

	list(run, "rate limits", zname, func() error {
		rls, err := cfList[cfNamed](ctx, fmt.Sprintf("/zones/%s/rate_limits", zid))
		for _, r := range rls {
			add(inv, &model.Resource{
				ID: "rate_limit/" + r.ID, Name: zname + "-ratelimit", NativeType: "cloudflare:rate_limit", Container: acct, Source: "cf-api",
				Properties: map[string]any{"zone_id": zid, "rate_limit_id": r.ID},
			})
		}
		return err
	})

	list(run, "access rules", zname, func() error {
		ars, err := cfList[cfAccessRule](ctx, fmt.Sprintf("/zones/%s/firewall/access_rules/rules", zid))
		for _, a := range ars {
			// The zone endpoint returns inherited (account/org/user) rules too; only a
			// ZONE-owned rule imports as zone/<zone_id>/<id>. Adopt those only; account
			// rules come from enumAccount, and inherited ones are managed elsewhere.
			// (Exact scope.type strings to be confirmed at live QA — see spec.)
			if a.Scope.Type != "zone" && a.Scope.Type != "" {
				continue
			}
			add(inv, &model.Resource{
				ID: "access_rule/" + a.ID, Name: zname + "-accessrule", NativeType: "cloudflare:access_rule", Container: acct, Source: "cf-api",
				Properties: map[string]any{"scope": "zone", "parent_id": zid, "rule_id": a.ID},
			})
		}
		return err
	})

	list(run, "load balancers", zname, func() error {
		lbs, err := cfList[cfNamed](ctx, fmt.Sprintf("/zones/%s/load_balancers", zid))
		for _, lb := range lbs {
			add(inv, &model.Resource{
				ID: "load_balancer/" + lb.ID, Name: zname + "-lb", NativeType: "cloudflare:load_balancer", Container: acct, Source: "cf-api",
				Properties: map[string]any{"zone_id": zid, "load_balancer_id": lb.ID},
			})
		}
		return err
	})

	list(run, "custom certs", zname, func() error {
		cs, err := cfList[cfNamed](ctx, fmt.Sprintf("/zones/%s/custom_certificates", zid))
		for _, c := range cs {
			add(inv, &model.Resource{
				ID: "custom_ssl/" + c.ID, Name: zname + "-customssl", NativeType: "cloudflare:custom_ssl", Container: acct, Source: "cf-api",
				Properties: map[string]any{"zone_id": zid, "certificate_id": c.ID},
			})
		}
		return err
	})
}

// --- account-scoped --------------------------------------------------------

func enumAccount(ctx context.Context, run *core.Run, inv *model.Inventory, acct string) {
	list(run, "account rulesets", acct, func() error {
		rs, err := cfList[cfRuleset](ctx, fmt.Sprintf("/accounts/%s/rulesets", acct))
		for _, r := range rs {
			if r.Kind == "managed" {
				continue
			}
			add(inv, &model.Resource{
				ID: "ruleset/" + r.ID, Name: "account-ruleset-" + r.Phase, NativeType: "cloudflare:ruleset", Container: acct, Source: "cf-api",
				Properties: map[string]any{"scope": "account", "parent_id": acct, "ruleset_id": r.ID, "kind": r.Kind, "phase": r.Phase},
			})
		}
		return err
	})

	list(run, "account access rules", acct, func() error {
		ars, err := cfList[cfAccessRule](ctx, fmt.Sprintf("/accounts/%s/firewall/access_rules/rules", acct))
		for _, a := range ars {
			// Only an ACCOUNT-owned rule imports as account/<account_id>/<id>; zone/org/
			// user rules that surface here are managed at their own scope.
			if a.Scope.Type != "account" {
				continue
			}
			add(inv, &model.Resource{
				ID: "access_rule/" + a.ID, Name: "account-accessrule", NativeType: "cloudflare:access_rule", Container: acct, Source: "cf-api",
				Properties: map[string]any{"scope": "account", "parent_id": acct, "rule_id": a.ID},
			})
		}
		return err
	})

	list(run, "lb pools", acct, func() error {
		ps, err := cfList[cfNamed](ctx, fmt.Sprintf("/accounts/%s/load_balancers/pools", acct))
		for _, p := range ps {
			add(inv, &model.Resource{
				ID: "lb_pool/" + p.ID, Name: "lb-pool", NativeType: "cloudflare:load_balancer_pool", Container: acct, Source: "cf-api",
				Properties: map[string]any{"account_id": acct, "pool_id": p.ID},
			})
		}
		return err
	})

	list(run, "lb monitors", acct, func() error {
		ms, err := cfList[cfNamed](ctx, fmt.Sprintf("/accounts/%s/load_balancers/monitors", acct))
		for _, m := range ms {
			add(inv, &model.Resource{
				ID: "lb_monitor/" + m.ID, Name: "lb-monitor", NativeType: "cloudflare:load_balancer_monitor", Container: acct, Source: "cf-api",
				Properties: map[string]any{"account_id": acct, "monitor_id": m.ID},
			})
		}
		return err
	})

	list(run, "access apps", acct, func() error {
		apps, err := cfList[cfAccessApp](ctx, fmt.Sprintf("/accounts/%s/access/apps", acct))
		for _, app := range apps {
			add(inv, &model.Resource{
				ID: "access_application/" + app.ID, Name: app.Name, NativeType: "cloudflare:access_application", Container: acct, Source: "cf-api",
				Properties: map[string]any{"account_id": acct, "app_id": app.ID, "name": app.Name},
			})
			// Per-app policies.
			pols, perr := cfList[cfNamed](ctx, fmt.Sprintf("/accounts/%s/access/apps/%s/policies", acct, app.ID))
			if perr != nil {
				run.Log.Verbose("Enumerate", "list access policies for %s skipped: %v", app.Name, perr)
				continue
			}
			for _, pol := range pols {
				add(inv, &model.Resource{
					ID: "access_policy/" + pol.ID, Name: app.Name + "-policy", NativeType: "cloudflare:access_policy", Container: acct, Source: "cf-api",
					Properties: map[string]any{"account_id": acct, "app_id": app.ID, "policy_id": pol.ID},
				})
			}
		}
		return err
	})
}

// list runs a best-effort enumeration closure and continues on failure. A 403/404
// means the feature/permission is absent (expected) and is logged at Verbose; any
// other error (429, 5xx, network, API-level failure) means enumeration may be
// SILENTLY INCOMPLETE, which is dangerous for an adoption tool — surface it at Warn.
func list(run *core.Run, what, scope string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *cfAPIError
	if errors.As(err, &apiErr) && (apiErr.Status == 403 || apiErr.Status == 404) {
		run.Log.Verbose("Enumerate", "list %s for %s skipped (feature/permission absent): %v", what, scope, err)
		return
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — enumeration may be incomplete: %v", what, scope, err)
}

// --- API response shapes ---------------------------------------------------

type cfZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cfNamed struct {
	ID string `json:"id"`
}

type cfRecord struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

type cfRuleset struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Phase string `json:"phase"`
	Name  string `json:"name"`
}

type cfAccessRule struct {
	ID    string `json:"id"`
	Scope struct {
		Type string `json:"type"`
	} `json:"scope"`
}

type cfAccessApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
