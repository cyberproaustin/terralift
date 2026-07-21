package ns1

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for an NS1 account: zones (with their records, read
// from the per-zone GET), monitoring jobs, data sources (with their feeds), notify
// lists, teams, users, and the account API keys / TSIG keys (surfaced but excluded —
// secret material). One flat synthetic container. Best-effort per list (403/404 →
// Verbose; 401 → fatal; other errors → Warn + count).
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	acct := run.Scope.ID
	run.Log.Info("Enumerate", "NS1 API")

	inv := &model.Inventory{
		Cloud:       "ns1",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{acct: {ID: acct, Name: acct, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	list(run, &hardFails, &fatal, "zones", func() error {
		zones, err := ns1List[ns1ZoneSummary](ctx, "/zones")
		for _, z := range zones {
			add(inv, "zone/"+z.Zone, z.Zone, "ns1:zone", acct, map[string]any{"zone": z.Zone})
			// Records live on the per-zone GET (not a separate list).
			detail, derr := ns1GetOne[ns1ZoneDetail](ctx, "/zones/"+z.Zone)
			if derr != nil {
				logSub(run, "records", z.Zone, derr)
				continue
			}
			if detail.Link != "" || detail.Secondary.Enabled {
				continue // linked/secondary zone — records are managed on the primary, not here
			}
			for _, rec := range detail.Records {
				add(inv, "record/"+z.Zone+"/"+rec.Domain+"/"+rec.Type, rec.Domain+"-"+rec.Type, "ns1:record", acct,
					map[string]any{"zone": z.Zone, "domain": rec.Domain, "type": rec.Type})
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "monitoring jobs", func() error {
		js, err := ns1List[ns1ID](ctx, "/monitoring/jobs")
		for _, j := range js {
			add(inv, "monitoringjob/"+j.ID, "monitoringjob-"+j.ID, "ns1:monitoringjob", acct, map[string]any{"id": j.ID})
		}
		return err
	})

	list(run, &hardFails, &fatal, "data sources", func() error {
		ss, err := ns1List[ns1ID](ctx, "/data/sources")
		for _, s := range ss {
			add(inv, "datasource/"+s.ID, "datasource-"+s.ID, "ns1:datasource", acct, map[string]any{"id": s.ID})
			feeds, ferr := ns1List[ns1ID](ctx, "/data/feeds/"+s.ID)
			if ferr != nil {
				logSub(run, "data feeds", s.ID, ferr)
				continue
			}
			for _, f := range feeds {
				add(inv, "datafeed/"+s.ID+"/"+f.ID, "datafeed-"+f.ID, "ns1:datafeed", acct,
					map[string]any{"datasource_id": s.ID, "datafeed_id": f.ID})
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "notify lists", func() error {
		ls, err := ns1List[ns1ID](ctx, "/lists")
		for _, l := range ls {
			add(inv, "notifylist/"+l.ID, "notifylist-"+l.ID, "ns1:notifylist", acct, map[string]any{"id": l.ID})
		}
		return err
	})

	list(run, &hardFails, &fatal, "teams", func() error {
		ts, err := ns1List[ns1ID](ctx, "/account/teams")
		for _, t := range ts {
			add(inv, "team/"+t.ID, "team-"+t.ID, "ns1:team", acct, map[string]any{"id": t.ID})
		}
		return err
	})

	list(run, &hardFails, &fatal, "users", func() error {
		us, err := ns1List[ns1User](ctx, "/account/users")
		for _, u := range us {
			add(inv, "user/"+u.Username, u.Username, "ns1:user", acct, map[string]any{"username": u.Username})
		}
		return err
	})

	// Surfaced but excluded at export (secret material — key / TSIG secret).
	list(run, &hardFails, &fatal, "api keys", func() error {
		ks, err := ns1List[ns1ID](ctx, "/account/apikeys")
		for _, k := range ks {
			add(inv, "apikey/"+k.ID, "apikey-"+k.ID, "ns1:apikey", acct, map[string]any{"id": k.ID})
		}
		return err
	})
	list(run, &hardFails, &fatal, "tsig keys", func() error {
		ks, err := ns1List[ns1TSIG](ctx, "/tsig")
		for _, k := range ks {
			add(inv, "tsigkey/"+k.Name, k.Name, "ns1:tsigkey", acct, map[string]any{"name": k.Name})
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("ns1 enumeration failed on %d resource type(s) and found nothing — check NS1_APIKEY and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

func add(inv *model.Inventory, id, name, native, container string, props map[string]any) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: container, Source: "ns1-api", Properties: props,
	}
}

func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *ns1APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (feature/permission absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("ns1 authentication failed during enumeration (key revoked/invalid): %w", err)
			}
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

func logSub(run *core.Run, what, parent string, err error) {
	var apiErr *ns1APIError
	if errors.As(err, &apiErr) && (apiErr.Status == 403 || apiErr.Status == 404) {
		run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
		return
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes ---------------------------------------------------

type ns1ZoneSummary struct {
	Zone string `json:"zone"`
}

type ns1ZoneDetail struct {
	Zone      string `json:"zone"`
	Link      string `json:"link"`
	Secondary struct {
		Enabled bool `json:"enabled"`
	} `json:"secondary"`
	Records []struct {
		Domain string `json:"domain"`
		Type   string `json:"type"`
	} `json:"records"`
}

type ns1ID struct {
	ID string `json:"id"`
}

type ns1User struct {
	Username string `json:"username"`
}

type ns1TSIG struct {
	Name string `json:"name"`
}
