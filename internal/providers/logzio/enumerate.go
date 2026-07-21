package logzio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Logz.io account: the native config plane (alerts,
// notification endpoints, drop filters, sub-accounts, users, log-shipping tokens, s3 fetchers,
// archive settings, metrics accounts) plus the auth-groups singleton. One flat container = the
// account (no fan-out). Each resource is a single best-effort account-level list — GET
// bare-list, POST …/search, or a singleton GET. Best-effort per list: 401 → fatal; 403/404 →
// Verbose skip (feature/plan absent); other → Warn + count. The token never appears in
// errors/logs. The grafana_* embedded-Grafana plane and the Kibana data-view are deferred.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	acct := run.Scope.ID
	run.Log.Info("Enumerate", "Logz.io API: account=%s", acct)

	inv := &model.Inventory{
		Cloud:       "logzio",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{acct: {ID: acct, Name: acct, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Alerts (v2). The list-all GET; a POST /v2/alerts/search also exists (VERIFY at Phase B).
	list(run, &hardFails, &fatal, "alerts", func() error {
		as, err := lzGet[lzObj](ctx, "/v2/alerts")
		for _, a := range as {
			if a.id() != "" {
				addBare(inv, "alert/"+a.id(), a.label(), "logzio:alert_v2", acct, a.id())
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "endpoints", func() error {
		es, err := lzGet[lzObj](ctx, "/v1/endpoints")
		for _, e := range es {
			if e.id() != "" {
				addBare(inv, "endpoint/"+e.id(), e.label(), "logzio:endpoint", acct, e.id())
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "drop filters", func() error {
		ds, err := lzGet[lzObj](ctx, "/v1/drop-filters")
		for _, d := range ds {
			if d.id() != "" {
				addBare(inv, "drop_filter/"+d.id(), d.label(), "logzio:drop_filter", acct, d.id())
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "sub accounts", func() error {
		sas, err := lzGet[lzObj](ctx, "/v1/account-management/time-based-accounts")
		for _, sa := range sas {
			if sa.id() != "" {
				addBare(inv, "sub_account/"+sa.id(), sa.label(), "logzio:sub_account", acct, sa.id())
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "users", func() error {
		us, err := lzGet[lzObj](ctx, "/v1/user-management/users")
		for _, u := range us {
			if u.id() != "" {
				addBare(inv, "user/"+u.id(), u.label(), "logzio:user", acct, u.id())
			}
		}
		return err
	})

	// Log-shipping tokens — POST …/retrieve (a search body + pager); the token VALUE is
	// write-only and never decoded (lzObj omits it).
	list(run, &hardFails, &fatal, "log shipping tokens", func() error {
		ts, err := lzSearch[lzObj](ctx, "/v1/log-shipping/tokens/retrieve", "results")
		for _, t := range ts {
			if t.id() != "" {
				addBare(inv, "log_shipping_token/"+t.id(), t.label(), "logzio:log_shipping_token", acct, t.id())
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "s3 fetchers", func() error {
		fs, err := lzGet[lzObj](ctx, "/v1/s3-fetcher")
		for _, f := range fs {
			if f.id() != "" {
				addBare(inv, "s3_fetcher/"+f.id(), f.label(), "logzio:s3_fetcher", acct, f.id())
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "archive settings", func() error {
		as, err := lzGet[lzObj](ctx, "/v1/archive/settings")
		for _, a := range as {
			if a.id() != "" {
				addBare(inv, "archive/"+a.id(), a.label(), "logzio:archive_logs", acct, a.id())
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "metrics accounts", func() error {
		ms, err := lzGet[lzObj](ctx, "/v1/account-management/metrics-accounts")
		for _, m := range ms {
			if m.id() != "" {
				addBare(inv, "metrics_account/"+m.id(), m.label(), "logzio:metrics_account", acct, m.id())
			}
		}
		return err
	})

	// Authentication groups — a SINGLETON (the whole SAML group set is one resource); import by
	// a stable sentinel. Emit one resource if the endpoint is reachable.
	list(run, &hardFails, &fatal, "authentication groups", func() error {
		if _, _, err := lzDo(ctx, http.MethodGet, "/v1/authentication-groups", nil); err != nil {
			return err
		}
		addBare(inv, "authentication_groups", "authentication-groups", "logzio:authentication_groups", acct, "authentication_groups")
		return nil
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("logzio enumeration failed on %d resource type(s) and found nothing — check LOGZIO_API_TOKEN/LOGZIO_REGION and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// addBare adds a resource whose import id is a bare token (numeric/string id or a singleton
// sentinel) — Logz.io has no composite imports.
func addBare(inv *model.Inventory, id, name, native, acct, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: acct, Source: "logzio-api", Properties: map[string]any{"token": token},
	}
}

// list runs a best-effort enumeration closure and classifies any error: 401 → the token was
// revoked/expired, every remaining list will fail too, record it fatal; 403/404 → the feature/
// plan is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *logzioAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (feature/plan absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("logzio authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}
