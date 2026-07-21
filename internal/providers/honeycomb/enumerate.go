package honeycomb

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Honeycomb environment via the v1 config plane. The
// spine is a dataset FAN-OUT (the Fastly per-service pattern): GET /1/datasets (parent) → per
// dataset the column/derived_column/trigger/slo/query_annotation lists, plus a second-level
// per-SLO burn-alert fan-out; and a synthetic "__all__" pass captures the environment-wide
// (non-Classic) derived columns and multi-dataset triggers/SLOs (404 on Classic → skipped).
// Boards and recipients are flat env-wide lists. Best-effort per list: 401 → fatal; 403/404 →
// Verbose skip (scope/feature absent); other → Warn + count. The key never appears in
// errors/logs.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	env := run.Scope.ID
	run.Log.Info("Enumerate", "Honeycomb API: environment=%s", env)

	inv := &model.Inventory{
		Cloud:       "honeycomb",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{env: {ID: env, Name: env, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Parent: datasets (each is also a honeycombio_dataset, imported by its bare slug).
	var datasets []honeycombDataset
	list(run, &hardFails, &fatal, "datasets", func() error {
		ds, err := honeycombGet[honeycombDataset](ctx, "/1/datasets")
		datasets = ds
		for _, d := range ds {
			if d.Slug == "" {
				continue
			}
			addTeam(inv, "dataset/"+d.Slug, orName(d.Name, d.Slug), "honeycomb:dataset", env, d.Slug)
		}
		return err
	})

	// Per-dataset fan-out, then the env-wide __all__ pass. A 401 mid-fan-out sets fatal;
	// since every remaining list would re-401, stop early rather than emit Warn noise.
	for _, d := range datasets {
		if fatal != nil {
			break
		}
		if d.Slug == "" {
			continue
		}
		enumDataset(ctx, run, inv, env, d.Slug, false, &fatal)
	}
	if fatal == nil {
		enumDataset(ctx, run, inv, env, "__all__", true, &fatal)
	}
	if fatal != nil {
		return nil, fatal
	}

	// Team/environment-wide: boards (flexible) — skip classic boards (deprecated).
	list(run, &hardFails, &fatal, "boards", func() error {
		bs, err := honeycombGet[honeycombBoard](ctx, "/1/boards")
		for _, b := range bs {
			if b.ID == "" {
				continue
			}
			if strings.EqualFold(b.Type, "classic") {
				run.Log.Verbose("Enumerate", "board %s is a classic board — skipped (current provider uses flexible boards)", b.ID)
				continue
			}
			addTeam(inv, "board/"+b.ID, orName(b.Name, b.ID), "honeycomb:board", env, b.ID)
		}
		return err
	})

	// Team/environment-wide: recipients, mapped to the typed resource by `type`.
	list(run, &hardFails, &fatal, "recipients", func() error {
		rs, err := honeycombGet[honeycombRecipient](ctx, "/1/recipients")
		for _, r := range rs {
			if r.ID == "" {
				continue
			}
			native := recipientNative(r.Type)
			if native == "" {
				run.Log.Verbose("Enumerate", "recipient %s has unmapped type %q — skipped", r.ID, r.Type)
				continue
			}
			// Name from type+id only — never the target (a webhook/msteams target is a secret URL).
			addTeam(inv, "recipient/"+r.ID, r.Type+"-"+r.ID, native, env, r.ID)
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("honeycomb enumeration failed on %d resource type(s) and found nothing — check HONEYCOMB_API_KEY and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// enumDataset fans out one dataset's sub-resources. isAll marks the synthetic "__all__"
// environment-wide pass, which only carries derived columns and multi-dataset triggers/SLOs
// (no columns/query-annotations), and whose resources import BARE (the "__all__/" prefix is
// dropped — handled in importid.go via the dataset property).
func enumDataset(ctx context.Context, run *core.Run, inv *model.Inventory, env, ds string, isAll bool, fatal *error) {
	// dsPath is the URL-escaped dataset segment used in every request path (identity for a
	// valid slug / "__all__"; defence in depth against a reserved char). The raw ds is kept
	// for inventory ids and the import composite.
	dsPath := neturl.PathEscape(ds)
	if !isAll {
		subList(run, fatal, "columns", ds, func() error {
			cs, err := honeycombGet[honeycombColumn](ctx, "/1/columns/"+dsPath)
			for _, c := range cs {
				if c.KeyName == "" {
					continue
				}
				addDS(inv, "column/"+ds+"/"+c.KeyName, c.KeyName, "honeycomb:column", env, ds, c.KeyName)
			}
			return err
		})
		subList(run, fatal, "query_annotations", ds, func() error {
			qs, err := honeycombGet[honeycombIDName](ctx, "/1/query_annotations/"+dsPath)
			for _, q := range qs {
				if q.ID == "" {
					continue
				}
				addDS(inv, "query_annotation/"+ds+"/"+q.ID, orName(q.Name, q.ID), "honeycomb:query_annotation", env, ds, q.ID)
			}
			return err
		})
	}
	// Derived columns: dataset-scoped AND env-wide (__all__).
	subList(run, fatal, "derived_columns", ds, func() error {
		dcs, err := honeycombGet[honeycombDerivedColumn](ctx, "/1/derived_columns/"+dsPath)
		for _, dc := range dcs {
			if dc.Alias == "" {
				continue
			}
			addDS(inv, "derived_column/"+ds+"/"+dc.Alias, dc.Alias, "honeycomb:derived_column", env, ds, dc.Alias)
		}
		return err
	})
	// Triggers: dataset-scoped AND multi-dataset (__all__).
	subList(run, fatal, "triggers", ds, func() error {
		ts, err := honeycombGet[honeycombIDName](ctx, "/1/triggers/"+dsPath)
		for _, t := range ts {
			if t.ID == "" {
				continue
			}
			addDS(inv, "trigger/"+ds+"/"+t.ID, orName(t.Name, t.ID), "honeycomb:trigger", env, ds, t.ID)
		}
		return err
	})
	// SLOs: dataset-scoped AND multi-dataset (__all__); each SLO fans out to its burn alerts.
	var slos []honeycombIDName
	subList(run, fatal, "slos", ds, func() error {
		ss, err := honeycombGet[honeycombIDName](ctx, "/1/slos/"+dsPath)
		slos = ss
		for _, s := range ss {
			if s.ID == "" {
				continue
			}
			addDS(inv, "slo/"+ds+"/"+s.ID, orName(s.Name, s.ID), "honeycomb:slo", env, ds, s.ID)
		}
		return err
	})
	for _, s := range slos {
		if s.ID == "" {
			continue
		}
		subList(run, fatal, "burn_alerts", ds+"/"+s.ID, func() error {
			bas, err := honeycombGet[honeycombIDName](ctx, "/1/burn_alerts/"+dsPath+"?slo_id="+neturl.QueryEscape(s.ID))
			for _, ba := range bas {
				if ba.ID == "" {
					continue
				}
				addDS(inv, "burn_alert/"+ds+"/"+ba.ID, orName(ba.Name, ba.ID), "honeycomb:burn_alert", env, ds, ba.ID)
			}
			return err
		})
	}
}

func recipientNative(t string) string {
	switch t {
	case "email":
		return "honeycomb:email_recipient"
	case "pagerduty":
		return "honeycomb:pagerduty_recipient"
	case "slack":
		return "honeycomb:slack_recipient"
	case "webhook":
		return "honeycomb:webhook_recipient"
	case "msteams":
		return "honeycomb:msteams_recipient"
	case "msteams_workflow":
		return "honeycomb:msteams_workflow_recipient"
	default:
		return ""
	}
}

// addTeam adds an environment/team-wide resource whose import id is a BARE token (dataset
// slug, board id, recipient id).
func addTeam(inv *model.Inventory, id, name, native, env, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: env, Source: "honeycomb-api", Properties: map[string]any{"token": token},
	}
}

// addDS adds a dataset-scoped resource. The dataset property drives the conditional import
// composite (`<dataset>/<token>`, or bare `<token>` when dataset == "__all__").
func addDS(inv *model.Inventory, id, name, native, env, dataset, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: env, Source: "honeycomb-api", Properties: map[string]any{"dataset": dataset, "token": token},
	}
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// list runs a best-effort top-level enumeration closure and classifies any error: 401 → the
// key was revoked/expired, every remaining list will fail too, record it fatal; 403/404 → the
// scope/feature is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *honeycombAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (scope/feature absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("honeycomb authentication failed during enumeration (key revoked/expired): %w", err)
			}
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-dataset fan-out variant: 403/404 → Verbose skip (a dataset may lack a
// feature, or __all__ 404s on a Classic environment); 401 → still fatal; other → Warn. It
// does NOT increment hardFails (sub-lists multiply by dataset count; the top-level lists own
// the systemic-failure signal).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *honeycombAPIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == 403 || apiErr.Status == 404 {
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		}
		if apiErr.Status == 401 && *fatal == nil {
			*fatal = fmt.Errorf("honeycomb authentication failed during enumeration (key revoked/expired): %w", err)
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes (bare-array elements) -----------------------------

type honeycombDataset struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type honeycombColumn struct {
	KeyName string `json:"key_name"`
}

type honeycombDerivedColumn struct {
	Alias string `json:"alias"`
}

type honeycombIDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type honeycombBoard struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type honeycombRecipient struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}
