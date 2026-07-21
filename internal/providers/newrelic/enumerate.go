package newrelic

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one New Relic account via NerdGraph: the observability
// config plane (dashboards, alert policies + NRQL conditions + muting rules, the
// notification stack, synthetics, workloads, key transactions, obfuscation). One flat
// container = the account. Every query is best-effort: a NerdGraph UNAUTHORIZED/FORBIDDEN
// errorClass means the product/permission is absent → Verbose skip; an HTTP 401/403 means
// the key was revoked mid-run → fatal; anything else → Warn + count so a systemic failure
// is told apart from an empty account. The API key never appears in errors/logs.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	acct, err := nrAccountID()
	if err != nil {
		return nil, err
	}
	acctStr := strconv.Itoa(acct)
	org := run.Scope.ID
	run.Log.Info("Enumerate", "NerdGraph: account=%s", acctStr)

	inv := &model.Inventory{
		Cloud:       "newrelic",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{org: {ID: org, Name: run.Scope.ID, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// --- entitySearch: dashboards (keep only parents) ----------------------
	list(run, &hardFails, &fatal, "dashboards", func() error {
		ents, err := nrPaged(ctx, qEntitySearch, map[string]any{"query": entityFilter("type = 'DASHBOARD'", acct)}, extractEntities)
		for _, e := range ents {
			if e.DashboardParentGUID != "" {
				continue // a dashboard PAGE child — owned by the parent as a nested block
			}
			add(inv, "dashboard/"+e.GUID, orName(e.Name, e.GUID), "newrelic:dashboard", org, map[string]any{"guid": e.GUID})
		}
		return err
	})

	// --- entitySearch: synthetics monitors (ONE search → SIX resources) ----
	list(run, &hardFails, &fatal, "synthetics monitors", func() error {
		ents, err := nrPaged(ctx, qEntitySearch, map[string]any{"query": entityFilter("domain = 'SYNTH' AND type = 'MONITOR'", acct)}, extractEntities)
		for _, e := range ents {
			native := syntheticsNative(e.MonitorType)
			if native == "" {
				run.Log.Verbose("Enumerate", "synthetics monitor %s has unknown monitorType %q — skipped", e.GUID, e.MonitorType)
				continue
			}
			add(inv, "synthetics/"+e.GUID, orName(e.Name, e.GUID), native, org, map[string]any{"guid": e.GUID})
		}
		return err
	})

	// --- entitySearch: key transactions ------------------------------------
	list(run, &hardFails, &fatal, "key transactions", func() error {
		ents, err := nrPaged(ctx, qEntitySearch, map[string]any{"query": entityFilter("type = 'KEY_TRANSACTION'", acct)}, extractEntities)
		for _, e := range ents {
			add(inv, "key_transaction/"+e.GUID, orName(e.Name, e.GUID), "newrelic:key_transaction", org, map[string]any{"guid": e.GUID})
		}
		return err
	})

	// --- entitySearch: workloads (+ per-entity workloadId follow-up) -------
	list(run, &hardFails, &fatal, "workloads", func() error {
		ents, err := nrPaged(ctx, qEntitySearch, map[string]any{"query": entityFilter("type = 'WORKLOAD'", acct)}, extractEntities)
		for _, e := range ents {
			wid, werr := resolveWorkloadID(ctx, e.GUID)
			if werr != nil {
				logSub(run, "workloadId", e.GUID, werr)
				continue
			}
			if wid == "" {
				run.Log.Verbose("Enumerate", "workload %s has no workloadId — skipped (cannot build import id)", e.GUID)
				continue
			}
			add(inv, "workload/"+e.GUID, orName(e.Name, e.GUID), "newrelic:workload", org,
				map[string]any{"account_id": acctStr, "workload_id": wid, "guid": e.GUID})
		}
		return err
	})

	// --- dedicated: alert policies -----------------------------------------
	list(run, &hardFails, &fatal, "alert policies", func() error {
		ps, err := nrPaged(ctx, qAlertPolicies, map[string]any{"acct": acct}, extractPolicies)
		for _, p := range ps {
			add(inv, "alert_policy/"+p.ID.String(), orName(p.Name, p.ID.String()), "newrelic:alert_policy", org,
				map[string]any{"policy_id": p.ID.String(), "account_id": acctStr})
		}
		return err
	})

	// --- dedicated: NRQL alert conditions (type is part of the import id) ---
	list(run, &hardFails, &fatal, "nrql alert conditions", func() error {
		cs, err := nrPaged(ctx, qNRQLConditions, map[string]any{"acct": acct}, extractNRQLConditions)
		for _, c := range cs {
			add(inv, "nrql_condition/"+c.ID.String(), orName(c.Name, c.ID.String()), "newrelic:nrql_alert_condition", org,
				map[string]any{"policy_id": c.PolicyID.String(), "condition_id": c.ID.String(), "condition_type": c.Type})
		}
		return err
	})

	// --- dedicated: alert muting rules (no cursor) -------------------------
	list(run, &hardFails, &fatal, "alert muting rules", func() error {
		data, err := nrOnce(ctx, qMutingRules, map[string]any{"acct": acct})
		if err != nil {
			return err
		}
		rs, err := decodeMutingRules(data)
		for _, m := range rs {
			add(inv, "muting_rule/"+m.ID.String(), orName(m.Name, m.ID.String()), "newrelic:alert_muting_rule", org,
				map[string]any{"account_id": acctStr, "id": m.ID.String()})
		}
		return err
	})

	// --- dedicated: notification destinations ------------------------------
	list(run, &hardFails, &fatal, "notification destinations", func() error {
		ds, err := nrPaged(ctx, qDestinations, map[string]any{"acct": acct}, extractDestinations)
		for _, d := range ds {
			add(inv, "notif_destination/"+d.ID.String(), orName(d.Name, d.ID.String()), "newrelic:notification_destination", org,
				map[string]any{"id": d.ID.String()})
		}
		return err
	})

	// --- dedicated: notification channels (import by CHANNEL id) -----------
	list(run, &hardFails, &fatal, "notification channels", func() error {
		cs, err := nrPaged(ctx, qChannels, map[string]any{"acct": acct}, extractChannels)
		for _, c := range cs {
			add(inv, "notif_channel/"+c.ID.String(), orName(c.Name, c.ID.String()), "newrelic:notification_channel", org,
				map[string]any{"id": c.ID.String()})
		}
		return err
	})

	// --- dedicated: workflows ----------------------------------------------
	list(run, &hardFails, &fatal, "workflows", func() error {
		ws, err := nrPaged(ctx, qWorkflows, map[string]any{"acct": acct}, extractWorkflows)
		for _, w := range ws {
			add(inv, "workflow/"+w.ID.String(), orName(w.Name, w.ID.String()), "newrelic:workflow", org,
				map[string]any{"id": w.ID.String()})
		}
		return err
	})

	// --- dedicated: obfuscation rules + expressions (one query) ------------
	list(run, &hardFails, &fatal, "obfuscation", func() error {
		data, err := nrOnce(ctx, qObfuscation, map[string]any{"acct": acct})
		if err != nil {
			return err
		}
		rules, exprs, err := decodeObfuscation(data)
		for _, r := range rules {
			add(inv, "obfuscation_rule/"+r.ID.String(), orName(r.Name, r.ID.String()), "newrelic:obfuscation_rule", org,
				map[string]any{"id": r.ID.String()})
		}
		for _, e := range exprs {
			add(inv, "obfuscation_expression/"+e.ID.String(), orName(e.Name, e.ID.String()), "newrelic:obfuscation_expression", org,
				map[string]any{"id": e.ID.String()})
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("newrelic enumeration failed on %d query type(s) and found nothing — check NEW_RELIC_API_KEY/NEW_RELIC_ACCOUNT_ID/NEW_RELIC_REGION and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// resolveWorkloadID fetches a WorkloadEntity's numeric workloadId (needed for the 3-part
// import composite; entitySearch does not return it).
func resolveWorkloadID(ctx context.Context, guid string) (string, error) {
	data, err := nrOnce(ctx, qWorkloadID, map[string]any{"guid": guid})
	if err != nil {
		return "", err
	}
	return decodeWorkloadID(data)
}

// syntheticsNative maps a SyntheticMonitorEntityOutline.monitorType to the correct TF
// resource. ONE entitySearch returns every monitor; monitorType is the ONLY discriminator.
func syntheticsNative(monitorType string) string {
	switch monitorType {
	case "SIMPLE", "BROWSER":
		return "newrelic:synthetics_monitor"
	case "SCRIPT_API", "SCRIPT_BROWSER":
		return "newrelic:synthetics_script_monitor"
	case "CERT_CHECK":
		return "newrelic:synthetics_cert_check_monitor"
	case "BROKEN_LINKS":
		return "newrelic:synthetics_broken_links_monitor"
	case "STEP_MONITOR":
		return "newrelic:synthetics_step_monitor"
	default:
		return ""
	}
}

func add(inv *model.Inventory, id, name, native, container string, props map[string]any) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: container, Source: "nerdgraph", Properties: props,
	}
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// list runs a best-effort enumeration closure and classifies any error. A GraphQL
// UNAUTHORIZED/FORBIDDEN errorClass → the product/permission is absent, skip quietly. An
// HTTP 401/403 → the key was revoked/expired mid-run, every remaining query will fail too,
// so record it fatal rather than ship a partial inventory. Anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var ngErr *nerdgraphError
	if errors.As(err, &ngErr) {
		switch ngErr.ErrorClass {
		case "UNAUTHORIZED", "FORBIDDEN":
			run.Log.Verbose("Enumerate", "query %s skipped (product/permission absent): %v", what, err)
			return
		}
		if ngErr.Status == 401 || ngErr.Status == 403 {
			if *fatal == nil {
				*fatal = fmt.Errorf("newrelic authentication failed during enumeration (key revoked/expired): %w", err)
			}
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "query %s failed — enumeration may be incomplete: %v", what, err)
}

func logSub(run *core.Run, what, parent string, err error) {
	var ngErr *nerdgraphError
	if errors.As(err, &ngErr) && (ngErr.ErrorClass == "UNAUTHORIZED" || ngErr.ErrorClass == "FORBIDDEN") {
		run.Log.Verbose("Enumerate", "%s for %s skipped: %v", what, parent, err)
		return
	}
	run.Log.Warn("Enumerate", "%s for %s failed — may be incomplete: %v", what, parent, err)
}
