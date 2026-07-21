package launchdarkly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one LaunchDarkly account: the feature-flag config plane
// (projects, environments, flags + per-env targeting, segments, destinations, metrics) and the
// account-wide plane (webhooks, teams, custom roles). One flat container = the account. The
// spine is a PROJECT fan-out (GET /api/v2/projects → per-project environments/flags/metrics),
// with a second-level per-(project, environment) fan-out for segments/destinations and a flag×
// env derivation for feature_flag_environment. Best-effort per list: 401 → fatal; 403/404 →
// Verbose skip (role/feature/plan absent); other → Warn + count. The token never appears in
// errors/logs; every _links.next follow is host-validated.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	if ldBase() == "" {
		return nil, fmt.Errorf("launchdarkly: LAUNCHDARKLY_API_HOST is malformed (must be a bare hostname)")
	}
	acct := run.Scope.ID
	run.Log.Info("Enumerate", "LaunchDarkly API: account=%s", acct)

	inv := &model.Inventory{
		Cloud:       "launchdarkly",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{acct: {ID: acct, Name: acct, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Parent: projects (each is a launchdarkly_project + the fan-out key).
	var projects []ldKeyName
	list(run, &hardFails, &fatal, "projects", func() error {
		ps, err := ldList[ldKeyName](ctx, "/api/v2/projects")
		projects = ps
		for _, p := range ps {
			if p.Key == "" {
				continue
			}
			addBare(inv, "project/"+p.Key, orName(p.Name, p.Key), "launchdarkly:project", acct, p.Key)
		}
		return err
	})

	for _, p := range projects {
		if p.Key == "" || fatal != nil {
			continue
		}
		pk := p.Key
		enumProject(ctx, run, inv, acct, pk, &fatal)
	}

	// Account-wide flat lists (no fan-out).
	list(run, &hardFails, &fatal, "webhooks", func() error {
		ws, err := ldList[ldWebhook](ctx, "/api/v2/webhooks")
		for _, w := range ws {
			if w.ID == "" {
				continue
			}
			addBare(inv, "webhook/"+w.ID, orName(w.Name, w.ID), "launchdarkly:webhook", acct, w.ID)
		}
		return err
	})
	list(run, &hardFails, &fatal, "teams", func() error {
		ts, err := ldList[ldKeyName](ctx, "/api/v2/teams")
		for _, t := range ts {
			if t.Key == "" {
				continue
			}
			addBare(inv, "team/"+t.Key, orName(t.Name, t.Key), "launchdarkly:team", acct, t.Key)
		}
		return err
	})
	list(run, &hardFails, &fatal, "custom roles", func() error {
		rs, err := ldList[ldKeyName](ctx, "/api/v2/roles")
		for _, r := range rs {
			if r.Key == "" {
				continue
			}
			addBare(inv, "custom_role/"+r.Key, orName(r.Name, r.Key), "launchdarkly:custom_role", acct, r.Key)
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("launchdarkly enumeration failed on %d resource type(s) and found nothing — check LAUNCHDARKLY_ACCESS_TOKEN and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// enumProject fans out one project: environments (capturing env keys for the two-level fan-out),
// flags (+ the flag×env feature_flag_environment derivation), metrics, then per (project, env)
// the segments and destinations.
func enumProject(ctx context.Context, run *core.Run, inv *model.Inventory, acct, pk string, fatal *error) {
	pe := neturl.PathEscape(pk)
	var envKeys []string

	subList(run, fatal, "environments", pk, func() error {
		es, err := ldList[ldEnv](ctx, "/api/v2/projects/"+pe+"/environments")
		for _, e := range es {
			if e.Key == "" {
				continue
			}
			envKeys = append(envKeys, e.Key)
			addPair(inv, "environment/"+pk+"/"+e.Key, orName(e.Name, e.Key), "launchdarkly:environment", acct, pk, e.Key)
		}
		return err
	})

	// Flags (path is /api/v2/flags/<proj>, NOT /projects/<proj>/flags) + the per-env targeting
	// derived from each flag's embedded `environments` map (the flag×env volume plane).
	subList(run, fatal, "flags", pk, func() error {
		fs, err := ldList[ldFlag](ctx, "/api/v2/flags/"+pe)
		for _, f := range fs {
			if f.Key == "" {
				continue
			}
			addPair(inv, "flag/"+pk+"/"+f.Key, orName(f.Name, f.Key), "launchdarkly:feature_flag", acct, pk, f.Key)
			for envKey := range f.Environments {
				if envKey == "" {
					continue
				}
				// import id is <project>/<env>/<flag> — env in the MIDDLE (NOT flag_id + env).
				addTriple(inv, "flag_env/"+pk+"/"+envKey+"/"+f.Key, f.Key+" @ "+envKey,
					"launchdarkly:feature_flag_environment", acct, pk, envKey, f.Key)
			}
		}
		return err
	})

	subList(run, fatal, "metrics", pk, func() error {
		ms, err := ldList[ldKeyName](ctx, "/api/v2/metrics/"+pe)
		for _, m := range ms {
			if m.Key == "" {
				continue
			}
			addPair(inv, "metric/"+pk+"/"+m.Key, orName(m.Name, m.Key), "launchdarkly:metric", acct, pk, m.Key)
		}
		return err
	})

	// Two-level per (project, env) fan-out.
	for _, ek := range envKeys {
		if ek == "" || *fatal != nil {
			continue
		}
		ekPath := neturl.PathEscape(ek)
		subList(run, fatal, "segments", pk+"/"+ek, func() error {
			ss, err := ldList[ldKeyName](ctx, "/api/v2/segments/"+pe+"/"+ekPath)
			for _, s := range ss {
				if s.Key == "" {
					continue
				}
				addTriple(inv, "segment/"+pk+"/"+ek+"/"+s.Key, orName(s.Name, s.Key), "launchdarkly:segment", acct, pk, ek, s.Key)
			}
			return err
		})
		subList(run, fatal, "destinations", pk+"/"+ek, func() error {
			ds, err := ldList[ldDest](ctx, "/api/v2/destinations/"+pe+"/"+ekPath)
			for _, d := range ds {
				if d.ID == "" {
					continue
				}
				// destination leaf is the server _id, not a key.
				addTriple(inv, "destination/"+pk+"/"+ek+"/"+d.ID, orName(d.Name, d.ID), "launchdarkly:destination", acct, pk, ek, d.ID)
			}
			return err
		})
	}
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// addBare adds an account-wide resource whose import id is a bare key/_id.
func addBare(inv *model.Inventory, id, name, native, acct, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: acct, Source: "launchdarkly-api", Properties: map[string]any{"token": token},
	}
}

// addPair adds a project-scoped 2-part composite (<project_key>/<key>).
func addPair(inv *model.Inventory, id, name, native, acct, left, right string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: acct, Source: "launchdarkly-api", Properties: map[string]any{"left": left, "right": right},
	}
}

// addTriple adds an env-scoped 3-part composite (<project_key>/<env_key>/<leaf>, env in the
// middle).
func addTriple(inv *model.Inventory, id, name, native, acct, a, b, c string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: acct, Source: "launchdarkly-api", Properties: map[string]any{"a": a, "b": b, "c": c},
	}
}

// list runs a best-effort top-level enumeration closure and classifies any error: 401 → the
// token was revoked/expired, every remaining list will fail too, record it fatal; 403/404 →
// the role/feature/plan is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *launchdarklyAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (role/feature/plan absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("launchdarkly authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-parent fan-out variant: 403/404 → Verbose skip; 401 → still fatal; other
// → Warn. It does NOT increment hardFails (sub-lists multiply by project/env count).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *launchdarklyAPIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == 403 || apiErr.Status == 404 {
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		}
		if apiErr.Status == 401 {
			if *fatal == nil {
				*fatal = fmt.Errorf("launchdarkly authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes (secret fields are deliberately NOT decoded) -------

type ldKeyName struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ldEnv decodes only key/name — the apiKey/mobileKey/clientSideId SDK-key secrets are never
// pulled into the inventory.
type ldEnv struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ldFlag carries the embedded per-environment map (keys only are used, for the flag×env
// derivation); the map values are left as raw and never decoded.
type ldFlag struct {
	Key          string                     `json:"key"`
	Name         string                     `json:"name"`
	Environments map[string]json.RawMessage `json:"environments"`
}

// ldWebhook decodes only _id/name — the `secret` (HMAC signing secret) is never pulled.
type ldWebhook struct {
	ID   string `json:"_id"`
	Name string `json:"name"`
}

// ldDest decodes only _id/name — the `config` sink credentials are never pulled.
type ldDest struct {
	ID   string `json:"_id"`
	Name string `json:"name"`
}
