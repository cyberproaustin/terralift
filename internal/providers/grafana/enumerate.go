package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Grafana org: dashboards, folders, data sources, the
// unified-alerting provisioning plane (contact points, notification policy, message
// templates, mute timings, rule groups), teams, service accounts, playlists, library panels,
// and (best-effort, Enterprise) custom roles and reports. One flat container = the org, whose
// numeric id (resolved in Connect) is stamped as every resource's Container and reused as the
// orgID prefix in every composite import ID. Best-effort per list: 401 → fatal; 403/404 →
// Verbose skip (permission/feature/OSS-vs-Enterprise absent); other → Warn + count. The auth
// material never appears in errors/logs.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	org := run.Scope.ID
	run.Log.Info("Enumerate", "Grafana API: org=%s", org)

	inv := &model.Inventory{
		Cloud:       "grafana",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{org: {ID: org, Name: org, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// --- dashboards: search array (paged); the uid is all Phase A needs (the model is
	// fetched by generate-config-out, not us) ------------------------------------------
	list(run, &hardFails, &fatal, "dashboards", func() error {
		hits, err := grafanaListArrayPaged[grafanaSearchHit](ctx, "/api/search?type=dash-db", grafanaDashPage)
		for _, h := range hits {
			if h.UID == "" {
				continue
			}
			addTok(inv, "dashboard/"+h.UID, orName(h.Title, h.UID), "grafana:dashboard", org, h.UID)
		}
		return err
	})

	// --- folders: bare array (paged); skip the "General" folder (empty uid) ------------
	list(run, &hardFails, &fatal, "folders", func() error {
		fs, err := grafanaListArrayPaged[grafanaFolder](ctx, "/api/folders", grafanaPerPage)
		for _, f := range fs {
			if f.UID == "" {
				continue // the built-in "General" folder — not a real, adoptable folder
			}
			addTok(inv, "folder/"+f.UID, orName(f.Title, f.UID), "grafana:folder", org, f.UID)
		}
		return err
	})

	// --- data sources: bare array; import by uid ---------------------------------------
	list(run, &hardFails, &fatal, "data sources", func() error {
		ds, err := grafanaGetArray[grafanaDataSource](ctx, "/api/datasources")
		for _, d := range ds {
			if d.UID == "" {
				continue
			}
			addTok(inv, "data_source/"+d.UID, orName(d.Name, d.UID), "grafana:data_source", org, d.UID)
		}
		return err
	})

	// --- unified alerting (provisioning API) -------------------------------------------
	// Contact points: several integrations share one name; a contact_point resource is per
	// NAME, so dedupe by name.
	list(run, &hardFails, &fatal, "contact points", func() error {
		cps, err := grafanaGetArray[grafanaNamed](ctx, "/api/v1/provisioning/contact-points")
		seen := map[string]bool{}
		for _, c := range cps {
			if c.Name == "" || seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			addName(inv, "contact_point/"+c.Name, c.Name, "grafana:contact_point", org, c.Name)
		}
		return err
	})
	// Notification policy: a SINGLE tree per org (not a list). Emit one resource unless the
	// tree is truly empty (no receiver and no routes). NB: a real instance's DEFAULT policy
	// always carries a receiver (e.g. grafana-default-email), so the default tree IS adopted —
	// which is intended (its routing is org config); we only skip a genuinely empty tree.
	list(run, &hardFails, &fatal, "notification policy", func() error {
		p, err := grafanaGetOne[grafanaPolicy](ctx, "/api/v1/provisioning/policies")
		if err != nil {
			return err
		}
		if p.Receiver != "" || len(p.Routes) > 0 {
			inv.Resources["notification_policy"] = &model.Resource{
				ID: "notification_policy", Name: "notification-policy", NativeType: "grafana:notification_policy",
				TFType: tfType("grafana:notification_policy"), Container: org, Source: "grafana-api", Properties: map[string]any{},
			}
		}
		return nil
	})
	list(run, &hardFails, &fatal, "message templates", func() error {
		ts, err := grafanaGetArray[grafanaNamed](ctx, "/api/v1/provisioning/templates")
		for _, t := range ts {
			if t.Name == "" {
				continue
			}
			addName(inv, "message_template/"+t.Name, t.Name, "grafana:message_template", org, t.Name)
		}
		return err
	})
	list(run, &hardFails, &fatal, "mute timings", func() error {
		ms, err := grafanaGetArray[grafanaNamed](ctx, "/api/v1/provisioning/mute-timings")
		for _, m := range ms {
			if m.Name == "" {
				continue
			}
			addName(inv, "mute_timing/"+m.Name, m.Name, "grafana:mute_timing", org, m.Name)
		}
		return err
	})
	// Rule groups: synthesised by grouping the flat alert-rule list on (folderUID, ruleGroup).
	list(run, &hardFails, &fatal, "alert rule groups", func() error {
		rules, err := grafanaGetArray[grafanaAlertRule](ctx, "/api/v1/provisioning/alert-rules")
		seen := map[string]bool{}
		for _, r := range rules {
			if r.FolderUID == "" || r.RuleGroup == "" {
				continue
			}
			key := r.FolderUID + "\x00" + r.RuleGroup
			if seen[key] {
				continue
			}
			seen[key] = true
			inv.Resources["rule_group/"+key] = &model.Resource{
				ID: "rule_group/" + key, Name: r.RuleGroup, NativeType: "grafana:rule_group",
				TFType: tfType("grafana:rule_group"), Container: org, Source: "grafana-api",
				Properties: map[string]any{"folder_uid": r.FolderUID, "title": r.RuleGroup},
			}
		}
		return err
	})

	// --- teams / service accounts: keyed + paged; import by numeric id -----------------
	list(run, &hardFails, &fatal, "teams", func() error {
		ts, err := grafanaListKeyedPaged[grafanaIDName](ctx, "/api/teams/search", "teams", "perpage", grafanaPerPage)
		for _, t := range ts {
			addTok(inv, "team/"+itoa(t.ID), orName(t.Name, itoa(t.ID)), "grafana:team", org, itoa(t.ID))
		}
		return err
	})
	list(run, &hardFails, &fatal, "service accounts", func() error {
		sas, err := grafanaListKeyedPaged[grafanaIDName](ctx, "/api/serviceaccounts/search", "serviceAccounts", "perpage", grafanaPerPage)
		for _, s := range sas {
			addTok(inv, "service_account/"+itoa(s.ID), orName(s.Name, itoa(s.ID)), "grafana:service_account", org, itoa(s.ID))
		}
		return err
	})

	// --- playlists / library panels ----------------------------------------------------
	list(run, &hardFails, &fatal, "playlists", func() error {
		ps, err := grafanaGetArray[grafanaUIDName](ctx, "/api/playlists")
		for _, p := range ps {
			if p.UID == "" {
				continue
			}
			addTok(inv, "playlist/"+p.UID, orName(p.Name, p.UID), "grafana:playlist", org, p.UID)
		}
		return err
	})
	list(run, &hardFails, &fatal, "library panels", func() error {
		lps, err := listLibraryPanels(ctx)
		for _, l := range lps {
			if l.UID == "" {
				continue
			}
			addTok(inv, "library_panel/"+l.UID, orName(l.Name, l.UID), "grafana:library_panel", org, l.UID)
		}
		return err
	})

	// --- Enterprise (best-effort; 403/404 on OSS → skip) -------------------------------
	// Custom RBAC roles: skip fixed/global built-ins.
	list(run, &hardFails, &fatal, "roles", func() error {
		rs, err := grafanaGetArray[grafanaRole](ctx, "/api/access-control/roles")
		for _, r := range rs {
			if r.UID == "" || r.Global || strings.HasPrefix(r.UID, "fixed:") {
				continue
			}
			addTok(inv, "role/"+r.UID, orName(r.Name, r.UID), "grafana:role", org, r.UID)
		}
		return err
	})
	list(run, &hardFails, &fatal, "reports", func() error {
		rs, err := grafanaGetArray[grafanaIDName](ctx, "/api/reports")
		for _, r := range rs {
			addTok(inv, "report/"+itoa(r.ID), orName(r.Name, itoa(r.ID)), "grafana:report", org, itoa(r.ID))
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("grafana enumeration failed on %d resource type(s) and found nothing — check GRAFANA_URL/GRAFANA_AUTH and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// listLibraryPanels paginates the double-nested library-elements endpoint
// ({"result":{"elements":[...],"totalCount":n}}), kind=1 (panels).
func listLibraryPanels(ctx context.Context) ([]grafanaUIDName, error) {
	var all []grafanaUIDName
	for page := 1; ; page++ {
		if page > grafanaMaxPages {
			return nil, &grafanaAPIError{msg: "grafana /api/library-elements: pagination exceeded max pages"}
		}
		url := fmt.Sprintf("/api/library-elements?kind=1&perPage=%d&page=%d", grafanaPerPage, page)
		body, _, err := grafanaDo(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		var env struct {
			Result struct {
				Elements   []grafanaUIDName `json:"elements"`
				TotalCount int              `json:"totalCount"`
			} `json:"result"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &env); err != nil {
				return nil, &grafanaAPIError{msg: "grafana /api/library-elements: decode: " + err.Error()}
			}
		}
		all = append(all, env.Result.Elements...)
		if len(env.Result.Elements) < grafanaPerPage || (env.Result.TotalCount > 0 && len(all) >= env.Result.TotalCount) {
			return all, nil
		}
	}
}

// addTok adds a resource whose import token is a uid or a stringified numeric id (the
// orgID prefix is applied at export time from Container).
func addTok(inv *model.Inventory, id, name, native, org, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: org, Source: "grafana-api", Properties: map[string]any{"token": token},
	}
}

// addName adds a resource whose import token is a free-text name (contact_point,
// message_template, mute_timing).
func addName(inv *model.Inventory, id, name, native, org, nm string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: org, Source: "grafana-api", Properties: map[string]any{"name": nm},
	}
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// list runs a best-effort enumeration closure and classifies any error: 401 → the auth was
// revoked/expired, every remaining list will fail too, so record it fatal; 403/404 → the
// feature/permission is absent (OSS-vs-Enterprise, or the org role lacks the scope), skip
// quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *grafanaAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (feature/permission absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("grafana authentication failed during enumeration (token/basic revoked/expired): %w", err)
			}
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// --- API response shapes ---------------------------------------------------

type grafanaSearchHit struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

type grafanaFolder struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
}

type grafanaDataSource struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type grafanaNamed struct {
	Name string `json:"name"`
}

type grafanaUIDName struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

type grafanaIDName struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type grafanaPolicy struct {
	Receiver string            `json:"receiver"`
	Routes   []json.RawMessage `json:"routes"`
}

type grafanaAlertRule struct {
	UID       string `json:"uid"`
	Title     string `json:"title"`
	FolderUID string `json:"folderUID"`
	RuleGroup string `json:"ruleGroup"`
}

type grafanaRole struct {
	UID    string `json:"uid"`
	Name   string `json:"name"`
	Global bool   `json:"global"`
}
