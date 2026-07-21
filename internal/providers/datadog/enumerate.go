package datadog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for a Datadog org: the observability config plane
// (monitors, dashboards, SLOs, synthetics, logs config, notebooks, security rules,
// downtimes) plus the IAM-ish breadth (roles, users). One flat container = the org.
// Every list is best-effort (403/404 → Verbose skip; 401 → fatal; other errors → Warn +
// count, so a systemic failure is told apart from an empty org). Each list is tagged with
// its API version + response shape + pager per docs/v2-specs/datadog.md.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	org := run.Scope.ID
	run.Log.Info("Enumerate", "Datadog API: org=%s", org)

	inv := &model.Inventory{
		Cloud:       "datadog",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{org: {ID: org, Name: org, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// --- v1 bare array -----------------------------------------------------
	// Monitors: 0-based page/page_size. Skip "synthetics alert" monitors — those are
	// owned by the synthetics test, not standalone (per Terraformer).
	list(run, &hardFails, &fatal, "monitors", func() error {
		ms, err := datadogListArrayPaged[ddMonitor](ctx, "/api/v1/monitor")
		for _, m := range ms {
			if m.Type == "synthetics alert" {
				continue
			}
			add(inv, "monitor/"+itoa(m.ID), orName(m.Name, itoa(m.ID)), "datadog:monitor", org, map[string]any{"id": itoa(m.ID)})
		}
		return err
	})
	// Logs custom pipelines: bare array, unpaged. Skip is_read_only integration pipelines.
	list(run, &hardFails, &fatal, "logs pipelines", func() error {
		ps, err := datadogGetArray[ddLogsPipeline](ctx, "/api/v1/logs/config/pipelines")
		for _, p := range ps {
			if p.IsReadOnly {
				continue
			}
			add(inv, "logs_pipeline/"+p.ID, orName(p.Name, p.ID), "datadog:logs_custom_pipeline", org, map[string]any{"id": p.ID})
		}
		return err
	})

	// --- v1 keyed object (unpaged) -----------------------------------------
	list(run, &hardFails, &fatal, "dashboards", func() error {
		ds, err := datadogGetKeyed[ddDashboard](ctx, "/api/v1/dashboard", "dashboards")
		for _, d := range ds {
			add(inv, "dashboard/"+d.ID, orName(d.Title, d.ID), "datadog:dashboard", org, map[string]any{"id": d.ID})
		}
		return err
	})
	list(run, &hardFails, &fatal, "dashboard lists", func() error {
		ls, err := datadogGetKeyed[ddDashboardList](ctx, "/api/v1/dashboard/lists/manual", "dashboard_lists")
		for _, l := range ls {
			add(inv, "dashboard_list/"+itoa(l.ID), orName(l.Name, itoa(l.ID)), "datadog:dashboard_list", org, map[string]any{"id": itoa(l.ID)})
		}
		return err
	})
	list(run, &hardFails, &fatal, "synthetics tests", func() error {
		ts, err := datadogGetKeyed[ddSynthetics](ctx, "/api/v1/synthetics/tests", "tests")
		for _, t := range ts {
			add(inv, "synthetics_test/"+t.PublicID, orName(t.Name, t.PublicID), "datadog:synthetics_test", org, map[string]any{"public_id": t.PublicID})
		}
		return err
	})
	// Logs indexes: the index NAME is the id (no separate numeric id).
	list(run, &hardFails, &fatal, "logs indexes", func() error {
		ix, err := datadogGetKeyed[ddLogsIndex](ctx, "/api/v1/logs/config/indexes", "indexes")
		for _, i := range ix {
			add(inv, "logs_index/"+i.Name, i.Name, "datadog:logs_index", org, map[string]any{"name": i.Name})
		}
		return err
	})

	// --- v1 keyed "data" (flat), offset paged ------------------------------
	list(run, &hardFails, &fatal, "slos", func() error {
		ss, err := datadogListKeyedOffset[ddSLO](ctx, "/api/v1/slo", "data")
		for _, s := range ss {
			add(inv, "slo/"+s.ID, orName(s.Name, s.ID), "datadog:service_level_objective", org, map[string]any{"id": s.ID})
		}
		return err
	})

	// --- v1 JSON:API, start/count paged ------------------------------------
	list(run, &hardFails, &fatal, "notebooks", func() error {
		items, err := datadogListJSONAPIStartCount(ctx, "/api/v1/notebooks")
		for _, it := range items {
			add(inv, "notebook/"+it.id(), orName(it.attr("name"), it.id()), "datadog:notebook", org, map[string]any{"id": it.id()})
		}
		return err
	})

	// --- v2 JSON:API, page[number] paged -----------------------------------
	// Logs metrics: JSON:API, unpaged (id = the metric name).
	list(run, &hardFails, &fatal, "logs metrics", func() error {
		items, err := datadogListJSONAPI(ctx, "/api/v2/logs/config/metrics")
		for _, it := range items {
			add(inv, "logs_metric/"+it.id(), it.id(), "datadog:logs_metric", org, map[string]any{"id": it.id()})
		}
		return err
	})
	// Security rules: skip attributes.isDefault (managed by Datadog).
	list(run, &hardFails, &fatal, "security rules", func() error {
		items, err := datadogListJSONAPI(ctx, "/api/v2/security_monitoring/rules")
		for _, it := range items {
			if it.attrBool("isDefault") {
				continue
			}
			add(inv, "security_rule/"+it.id(), orName(it.attr("name"), it.id()), "datadog:security_monitoring_rule", org, map[string]any{"id": it.id()})
		}
		return err
	})
	list(run, &hardFails, &fatal, "roles", func() error {
		items, err := datadogListJSONAPI(ctx, "/api/v2/roles")
		for _, it := range items {
			add(inv, "role/"+it.id(), orName(it.attr("name"), it.id()), "datadog:role", org, map[string]any{"id": it.id()})
		}
		return err
	})
	list(run, &hardFails, &fatal, "users", func() error {
		items, err := datadogListJSONAPI(ctx, "/api/v2/users")
		for _, it := range items {
			name := it.attr("email")
			if name == "" {
				name = it.attr("handle")
			}
			add(inv, "user/"+it.id(), orName(name, it.id()), "datadog:user", org, map[string]any{"id": it.id()})
		}
		return err
	})

	// --- v2 JSON:API, page[offset] paged (the quirk) -----------------------
	list(run, &hardFails, &fatal, "downtimes", func() error {
		items, err := datadogListJSONAPIOffset(ctx, "/api/v2/downtimes")
		for _, it := range items {
			add(inv, "downtime/"+it.id(), "downtime-"+it.id(), "datadog:downtime_schedule", org, map[string]any{"id": it.id()})
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("datadog enumeration failed on %d resource type(s) and found nothing — check DD_API_KEY/DD_APP_KEY and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

func add(inv *model.Inventory, id, name, native, container string, props map[string]any) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: container, Source: "datadog-api", Properties: props,
	}
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// list runs a best-effort enumeration closure and classifies any error: 403/404 → the
// feature/permission is absent, skip quietly; 401 → the key pair was revoked/expired,
// every remaining list will fail too, so record it fatal rather than ship a partial
// inventory; anything else → Warn and count so a systemic failure is told from an empty
// org. Keys never appear in errors/logs.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *datadogAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (feature/permission absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("datadog authentication failed during enumeration (key revoked/expired): %w", err)
			}
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// --- API response shapes (typed v1 decodes) --------------------------------

type ddMonitor struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type ddDashboard struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type ddDashboardList struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ddSLO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ddSynthetics struct {
	PublicID string `json:"public_id"`
	Name     string `json:"name"`
}

type ddLogsIndex struct {
	Name string `json:"name"`
}

type ddLogsPipeline struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsReadOnly bool   `json:"is_read_only"`
}
