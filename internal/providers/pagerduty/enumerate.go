package pagerduty

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

// enumerate builds the inventory for one PagerDuty account: the on-call config plane
// (services + integrations, escalation policies, schedules, teams + memberships, users +
// contact methods + notification rules, business services, maintenance windows, extensions,
// webhook subscriptions, tags, response plays) and the legacy ruleset plane. One flat
// container = the account. Top-level lists use the keyed offset/`more` pager; five per-parent
// fan-outs (service→integrations, team→members, user→contact_methods/notification_rules,
// ruleset→rules) reach the sub-resources. Best-effort per list: 401 → fatal; 403/404 →
// Verbose skip (scope/feature absent); other → Warn + count. The token never appears in
// errors/logs.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	acct := run.Scope.ID
	run.Log.Info("Enumerate", "PagerDuty API: account=%s", acct)

	inv := &model.Inventory{
		Cloud:       "pagerduty",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{acct: {ID: acct, Name: acct, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error
	var fromEmail string // one account-user email, for the From-header endpoints

	// Services (+ integrations fan-out via ?include[]=integrations — the DOT composite).
	list(run, &hardFails, &fatal, "services", func() error {
		svcs, err := pdListPaged[pdService](ctx, "/services?include[]=integrations", "services", "")
		for _, s := range svcs {
			if s.ID == "" {
				continue
			}
			addBare(inv, "service/"+s.ID, label(s.ID, s.Name, ""), "pagerduty:service", acct, s.ID)
			for _, ig := range s.Integrations {
				if ig.ID == "" {
					continue
				}
				addPair(inv, "service_integration/"+s.ID+"/"+ig.ID, label(ig.ID, ig.Name, ig.Summary),
					"pagerduty:service_integration", acct, s.ID, ig.ID)
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "escalation policies", func() error {
		eps, err := pdListPaged[pdIDName](ctx, "/escalation_policies", "escalation_policies", "")
		for _, e := range eps {
			addBare(inv, "escalation_policy/"+e.ID, label(e.ID, e.Name, e.Summary), "pagerduty:escalation_policy", acct, e.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "schedules", func() error {
		ss, err := pdListPaged[pdIDName](ctx, "/schedules", "schedules", "")
		for _, s := range ss {
			addBare(inv, "schedule/"+s.ID, label(s.ID, s.Name, s.Summary), "pagerduty:schedule", acct, s.ID)
		}
		return err
	})

	// Teams (+ per-team members fan-out → team_membership, COLON, user-first).
	var teams []pdIDName
	list(run, &hardFails, &fatal, "teams", func() error {
		ts, err := pdListPaged[pdIDName](ctx, "/teams", "teams", "")
		teams = ts
		for _, t := range ts {
			addBare(inv, "team/"+t.ID, label(t.ID, t.Name, t.Summary), "pagerduty:team", acct, t.ID)
		}
		return err
	})
	for _, t := range teams {
		if t.ID == "" || fatal != nil {
			continue
		}
		subList(run, &fatal, "team members", t.ID, func() error {
			ms, err := pdListPaged[pdMember](ctx, "/teams/"+neturl.PathEscape(t.ID)+"/members", "members", "")
			for _, m := range ms {
				if m.User.ID == "" {
					continue
				}
				addPair(inv, "team_membership/"+m.User.ID+"/"+t.ID, m.User.ID+" in "+t.ID,
					"pagerduty:team_membership", acct, m.User.ID, t.ID) // <user_id>:<team_id>
			}
			return err
		})
	}

	// Users (+ per-user contact_methods / notification_rules fan-outs, both COLON user-first).
	var users []pdUser
	list(run, &hardFails, &fatal, "users", func() error {
		us, err := pdListPaged[pdUser](ctx, "/users", "users", "")
		users = us
		for _, u := range us {
			if u.ID == "" {
				continue
			}
			if fromEmail == "" && u.Email != "" {
				fromEmail = u.Email
			}
			// Name fallback is the opaque P-id (NOT the email) — keep user PII out of the
			// generated Terraform resource address.
			addBare(inv, "user/"+u.ID, label(u.ID, u.Name, ""), "pagerduty:user", acct, u.ID)
		}
		return err
	})
	for _, u := range users {
		if u.ID == "" || fatal != nil {
			continue
		}
		uid := u.ID
		uidPath := neturl.PathEscape(uid)
		subList(run, &fatal, "contact methods", uid, func() error {
			cms, err := pdListPaged[pdIDName](ctx, "/users/"+uidPath+"/contact_methods", "contact_methods", "")
			for _, c := range cms {
				addPair(inv, "user_contact_method/"+uid+"/"+c.ID, label(c.ID, c.Name, c.Summary),
					"pagerduty:user_contact_method", acct, uid, c.ID)
			}
			return err
		})
		subList(run, &fatal, "notification rules", uid, func() error {
			nrs, err := pdListPaged[pdIDName](ctx, "/users/"+uidPath+"/notification_rules", "notification_rules", "")
			for _, n := range nrs {
				addPair(inv, "user_notification_rule/"+uid+"/"+n.ID, label(n.ID, n.Name, n.Summary),
					"pagerduty:user_notification_rule", acct, uid, n.ID)
			}
			return err
		})
	}

	list(run, &hardFails, &fatal, "business services", func() error {
		bs, err := pdListPaged[pdIDName](ctx, "/business_services", "business_services", "")
		for _, b := range bs {
			addBare(inv, "business_service/"+b.ID, label(b.ID, b.Name, b.Summary), "pagerduty:business_service", acct, b.ID)
		}
		return err
	})

	// Maintenance windows — the two non-past states only (past windows are dead data). The
	// PagerDuty filter enum is single-valued (past/future/ongoing), so fetch future + ongoing
	// and merge; the inventory map dedupes by id.
	list(run, &hardFails, &fatal, "maintenance windows", func() error {
		for _, f := range []string{"future", "ongoing"} {
			mws, err := pdListPaged[pdIDName](ctx, "/maintenance_windows?filter="+f, "maintenance_windows", "")
			if err != nil {
				return err
			}
			for _, m := range mws {
				addBare(inv, "maintenance_window/"+m.ID, label(m.ID, m.Name, m.Summary), "pagerduty:maintenance_window", acct, m.ID)
			}
		}
		return nil
	})

	// Extensions — discriminate generic vs ServiceNow on the extension schema.
	list(run, &hardFails, &fatal, "extensions", func() error {
		exts, err := pdListPaged[pdExtension](ctx, "/extensions", "extensions", "")
		for _, e := range exts {
			if e.ID == "" {
				continue
			}
			addBare(inv, "extension/"+e.ID, label(e.ID, e.Name, ""), extensionNative(e), acct, e.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "webhook subscriptions", func() error {
		ws, err := pdListPaged[pdIDName](ctx, "/webhook_subscriptions", "webhook_subscriptions", "")
		for _, w := range ws {
			addBare(inv, "webhook_subscription/"+w.ID, label(w.ID, w.Name, w.Summary), "pagerduty:webhook_subscription", acct, w.ID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "tags", func() error {
		tags, err := pdListPaged[pdTag](ctx, "/tags", "tags", "")
		for _, t := range tags {
			addBare(inv, "tag/"+t.ID, orName(t.Label, t.ID), "pagerduty:tag", acct, t.ID)
		}
		return err
	})

	// Response plays — require the From: <user email> header (400 without it).
	list(run, &hardFails, &fatal, "response plays", func() error {
		if fromEmail == "" {
			run.Log.Verbose("Enumerate", "list response plays skipped: no account-user email resolved for the required From header")
			return nil
		}
		rps, err := pdListPaged[pdIDName](ctx, "/response_plays", "response_plays", fromEmail)
		for _, r := range rps {
			addBare(inv, "response_play/"+r.ID, label(r.ID, r.Name, r.Summary), "pagerduty:response_play", acct, r.ID)
		}
		return err
	})

	// Rulesets (legacy Event Rules Engine) + per-ruleset rules fan-out (DOT composite).
	var rulesets []pdIDName
	list(run, &hardFails, &fatal, "rulesets", func() error {
		rs, err := pdListPaged[pdIDName](ctx, "/rulesets", "rulesets", "")
		rulesets = rs
		for _, r := range rs {
			addBare(inv, "ruleset/"+r.ID, label(r.ID, r.Name, r.Summary), "pagerduty:ruleset", acct, r.ID)
		}
		return err
	})
	for _, rs := range rulesets {
		if rs.ID == "" || fatal != nil {
			continue
		}
		rsid := rs.ID
		subList(run, &fatal, "ruleset rules", rsid, func() error {
			rules, err := pdListPaged[pdIDName](ctx, "/rulesets/"+neturl.PathEscape(rsid)+"/rules", "rules", "")
			for _, rule := range rules {
				addPair(inv, "ruleset_rule/"+rsid+"/"+rule.ID, label(rule.ID, rule.Name, rule.Summary),
					"pagerduty:ruleset_rule", acct, rsid, rule.ID) // <ruleset_id>.<rule_id>
			}
			return err
		})
	}

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("pagerduty enumeration failed on %d resource type(s) and found nothing — check PAGERDUTY_TOKEN and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// extensionNative discriminates the generic extension from the ServiceNow extension (both
// arrive on GET /extensions) via the extension schema summary.
func extensionNative(e pdExtension) string {
	if strings.Contains(strings.ToLower(e.ExtensionSchema.Summary), "servicenow") {
		return "pagerduty:extension_servicenow"
	}
	return "pagerduty:extension"
}

func label(id, name, summary string) string {
	if name != "" {
		return name
	}
	if summary != "" {
		return summary
	}
	return id
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// addBare adds a resource whose import id is a bare P-prefixed token.
func addBare(inv *model.Inventory, id, name, native, acct, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: acct, Source: "pagerduty-api", Properties: map[string]any{"token": token},
	}
}

// addPair adds a composite-import resource; left/right are stored in import order (the
// separator — dot vs colon — is chosen per TF type in importid.go).
func addPair(inv *model.Inventory, id, name, native, acct, left, right string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: acct, Source: "pagerduty-api", Properties: map[string]any{"left": left, "right": right},
	}
}

// list runs a best-effort top-level enumeration closure and classifies any error: 401 → the
// token was revoked/expired, every remaining list will fail too, record it fatal; 403/404 →
// the scope/feature is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return // a prior 401 already doomed the run — don't fire more requests or log noise
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *pagerdutyAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (scope/feature absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("pagerduty authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-parent fan-out variant: 403/404 → Verbose skip; 401 → still fatal; other
// → Warn. It does NOT increment hardFails (sub-lists multiply by parent count; the top-level
// lists own the systemic-failure signal).
func subList(run *core.Run, fatal *error, what, parent string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *pagerdutyAPIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == 403 || apiErr.Status == 404 {
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		}
		if apiErr.Status == 401 {
			if *fatal == nil {
				*fatal = fmt.Errorf("pagerduty authentication failed during enumeration (token revoked/expired): %w", err)
			}
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes ---------------------------------------------------

type pdIDName struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type pdService struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Integrations []pdIDName `json:"integrations"`
}

type pdUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type pdMember struct {
	User pdIDName `json:"user"`
	Role string   `json:"role"`
}

type pdExtension struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	ExtensionSchema pdIDName `json:"extension_schema"`
}

type pdTag struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
