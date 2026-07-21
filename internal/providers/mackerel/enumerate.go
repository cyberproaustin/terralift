package mackerel

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Mackerel organization: services (+ the per-service roles
// fan-out), monitors, channels, notification groups, dashboards, AWS integrations, downtimes, and
// alert-group settings. One flat container = the org. The spine is a SERVICE fan-out for roles
// (GET /api/v0/services/<svc>/roles); everything else is a flat org-level list. Best-effort per
// list: 401 → fatal; 403/404 → Verbose skip (feature/permission absent); other → Warn + count. The
// API key never appears in errors/logs. Hosts, users, *_metadata, and the runtime alert plane are
// deferred.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	org := run.Scope.ID
	run.Log.Info("Enumerate", "Mackerel API: org=%s", org)

	inv := &model.Inventory{
		Cloud:       "mackerel",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{org: {ID: org, Name: org, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Parent: services. Each service is imported by its bare name; capture the names for the
	// per-service role fan-out.
	var services []mkObj
	list(run, &hardFails, &fatal, "services", func() error {
		ss, err := mkList[mkObj](ctx, "/api/v0/services", "services")
		services = ss
		for _, s := range ss {
			if s.Name == "" {
				continue
			}
			addBare(inv, "service/"+s.Name, s.Name, "mackerel:service", org, s.Name)
		}
		return err
	})

	// Roles — a per-service fan-out; import is the colon composite <service>:<role>.
	for _, s := range services {
		if s.Name == "" || fatal != nil {
			continue
		}
		svc := s.Name
		subList(run, &fatal, "roles", svc, func() error {
			rs, err := mkList[mkObj](ctx, "/api/v0/services/"+neturl.PathEscape(svc)+"/roles", "roles")
			for _, role := range rs {
				if role.Name == "" {
					continue
				}
				addRole(inv, "role/"+svc+"/"+role.Name, role.Name, "mackerel:role", org, svc, role.Name)
			}
			return err
		})
	}

	// Flat org-level lists — each imports by its opaque string id.
	list(run, &hardFails, &fatal, "monitors", func() error {
		ms, err := mkList[mkObj](ctx, "/api/v0/monitors", "monitors")
		for _, m := range ms {
			if m.ID != "" {
				addBare(inv, "monitor/"+m.ID, m.label(), "mackerel:monitor", org, m.ID)
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "channels", func() error {
		cs, err := mkList[mkObj](ctx, "/api/v0/channels", "channels")
		for _, c := range cs {
			if c.ID != "" {
				addBare(inv, "channel/"+c.ID, c.label(), "mackerel:channel", org, c.ID)
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "notification groups", func() error {
		gs, err := mkList[mkObj](ctx, "/api/v0/notification-groups", "notificationGroups")
		for _, g := range gs {
			if g.ID != "" {
				addBare(inv, "notification_group/"+g.ID, g.label(), "mackerel:notification_group", org, g.ID)
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "dashboards", func() error {
		ds, err := mkList[mkObj](ctx, "/api/v0/dashboards", "dashboards")
		for _, d := range ds {
			if d.ID != "" {
				addBare(inv, "dashboard/"+d.ID, d.label(), "mackerel:dashboard", org, d.ID)
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "aws integrations", func() error {
		as, err := mkList[mkObj](ctx, "/api/v0/aws-integrations", "aws_integrations")
		for _, a := range as {
			if a.ID != "" {
				addBare(inv, "aws_integration/"+a.ID, a.label(), "mackerel:aws_integration", org, a.ID)
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "downtimes", func() error {
		ds, err := mkList[mkObj](ctx, "/api/v0/downtimes", "downtimes")
		for _, d := range ds {
			if d.ID != "" {
				addBare(inv, "downtime/"+d.ID, d.label(), "mackerel:downtime", org, d.ID)
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "alert group settings", func() error {
		as, err := mkList[mkObj](ctx, "/api/v0/alert-group-settings", "alertGroupSettings")
		for _, a := range as {
			if a.ID != "" {
				addBare(inv, "alert_group_setting/"+a.ID, a.label(), "mackerel:alert_group_setting", org, a.ID)
			}
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("mackerel enumeration failed on %d resource type(s) and found nothing — check MACKEREL_APIKEY and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// addBare adds a resource whose import id is a bare token (a service name or an opaque string id).
func addBare(inv *model.Inventory, id, name, native, org, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: org, Source: "mackerel-api", Properties: map[string]any{"token": token},
	}
}

// addRole adds a role, whose import id is the colon composite <service>:<role> (built in
// deriveImportID from the separately-stored parts).
func addRole(inv *model.Inventory, id, name, native, org, service, role string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: org, Source: "mackerel-api", Properties: map[string]any{"service": service, "role": role},
	}
}

// list runs a best-effort top-level enumeration closure and classifies any error: 401 → the key
// was revoked, every remaining list will fail too, record it fatal; 403/404 → the feature/
// permission is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *mackerelAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (feature/permission absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("mackerel authentication failed during enumeration (API key revoked): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-service fan-out variant: 403/404 → Verbose skip; 401 → still fatal; other →
// Warn. It does NOT increment hardFails (sub-lists multiply by service count).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *mackerelAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("mackerel authentication failed during enumeration (API key revoked): %w", err)
			}
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}
