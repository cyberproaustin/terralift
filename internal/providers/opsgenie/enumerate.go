package opsgenie

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for one Opsgenie account: the on-call config plane (teams +
// routing rules, users + contacts + notification rules, schedules + rotations, escalations,
// services + incident rules, API/Email integrations, alert/notification policies, maintenance,
// heartbeats). One flat container = the account. Top-level lists use the data/paging.next
// pager; per-parent fan-outs reach the sub-resources. Best-effort per list: 401/403 → fatal;
// 404 → Verbose skip (feature absent); other → Warn + count. The GenieKey never appears in
// errors/logs; every paging.next follow is host-validated inside ogList.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	acct := run.Scope.ID
	run.Log.Info("Enumerate", "Opsgenie API: account=%s", acct)

	inv := &model.Inventory{
		Cloud:       "opsgenie",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{acct: {ID: acct, Name: acct, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// Teams (+ inline member roster handled at Phase-B curation; routing-rules + per-team
	// policies fan out below).
	var teams []ogIDName
	list(run, &hardFails, &fatal, "teams", func() error {
		ts, err := ogList[ogIDName](ctx, "/v2/teams")
		teams = ts
		for _, t := range ts {
			addBare(inv, "team/"+t.ID, label(t.ID, t.Name), "opsgenie:team", acct, t.ID)
		}
		return err
	})

	// Users (capture id AND username — the two per-user fan-outs use different parents).
	var users []ogUser
	list(run, &hardFails, &fatal, "users", func() error {
		us, err := ogList[ogUser](ctx, "/v2/users")
		users = us
		for _, u := range us {
			addBare(inv, "user/"+u.ID, label(u.ID, u.FullName), "opsgenie:user", acct, u.ID)
		}
		return err
	})

	// Schedules (+ embedded rotations via ?expand=rotation → schedule_rotation composite).
	list(run, &hardFails, &fatal, "schedules", func() error {
		ss, err := ogList[ogSchedule](ctx, "/v2/schedules?expand=rotation")
		for _, s := range ss {
			addBare(inv, "schedule/"+s.ID, label(s.ID, s.Name), "opsgenie:schedule", acct, s.ID)
			for _, rot := range s.Rotations {
				if rot.ID == "" {
					continue
				}
				addPair(inv, "schedule_rotation/"+s.ID+"/"+rot.ID, label(rot.ID, rot.Name),
					"opsgenie:schedule_rotation", acct, s.ID, rot.ID) // <schedule_id>/<rotation_id>
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "escalations", func() error {
		es, err := ogList[ogIDName](ctx, "/v2/escalations")
		for _, e := range es {
			addBare(inv, "escalation/"+e.ID, label(e.ID, e.Name), "opsgenie:escalation", acct, e.ID)
		}
		return err
	})

	// Services (capture — incident-rules fan out below on the v1 path).
	var services []ogIDName
	list(run, &hardFails, &fatal, "services", func() error {
		svcs, err := ogList[ogIDName](ctx, "/v2/services")
		services = svcs
		for _, s := range svcs {
			addBare(inv, "service/"+s.ID, label(s.ID, s.Name), "opsgenie:service", acct, s.ID)
		}
		return err
	})

	// Integrations — adopt only API + Email; every other type carries vendor credentials and
	// is out of scope (skip at Verbose).
	list(run, &hardFails, &fatal, "integrations", func() error {
		igs, err := ogList[ogIntegration](ctx, "/v2/integrations")
		for _, ig := range igs {
			if ig.ID == "" {
				continue
			}
			switch ig.Type {
			case "API":
				addBare(inv, "integration/"+ig.ID, label(ig.ID, ig.Name), "opsgenie:api_integration", acct, ig.ID)
			case "Email":
				addBare(inv, "integration/"+ig.ID, label(ig.ID, ig.Name), "opsgenie:email_integration", acct, ig.ID)
			default:
				run.Log.Verbose("Enumerate", "integration %s type %q skipped (vendor-credential plane, out of scope)", ig.ID, ig.Type)
			}
		}
		return err
	})

	// Global alert policies (team-scope derived from the object's teamId → bare vs team/policy).
	list(run, &hardFails, &fatal, "alert policies (global)", func() error {
		ps, err := ogList[ogPolicy](ctx, "/v2/policies/alert")
		for _, p := range ps {
			addAlertPolicy(inv, p.ID, label(p.ID, p.Name), acct, p.ID, p.TeamID)
		}
		return err
	})

	list(run, &hardFails, &fatal, "maintenance", func() error {
		ms, err := ogList[ogIDName](ctx, "/v2/maintenance?type=non-expired")
		for _, m := range ms {
			addBare(inv, "maintenance/"+m.ID, label(m.ID, m.Name), "opsgenie:maintenance", acct, m.ID)
		}
		return err
	})

	// Heartbeats — the odd nested envelope; import by name.
	list(run, &hardFails, &fatal, "heartbeats", func() error {
		hs, err := ogListHeartbeats(ctx)
		for _, h := range hs {
			if h.Name == "" {
				continue
			}
			addBare(inv, "heartbeat/"+h.Name, h.Name, "opsgenie:heartbeat", acct, h.Name)
		}
		return err
	})

	// --- per-parent fan-outs ------------------------------------------------
	for _, t := range teams {
		if t.ID == "" || fatal != nil {
			continue
		}
		tid := t.ID
		subList(run, &fatal, "team routing rules", tid, func() error {
			rs, err := ogList[ogIDName](ctx, "/v2/teams/"+neturl.PathEscape(tid)+"/routing-rules")
			for _, r := range rs {
				addPair(inv, "team_routing_rule/"+tid+"/"+r.ID, label(r.ID, r.Name),
					"opsgenie:team_routing_rule", acct, tid, r.ID) // <team_id>/<routing_rule_id>
			}
			return err
		})
		subList(run, &fatal, "team alert policies", tid, func() error {
			ps, err := ogList[ogPolicy](ctx, "/v2/policies/alert?teamId="+neturl.QueryEscape(tid))
			for _, p := range ps {
				addAlertPolicy(inv, p.ID, label(p.ID, p.Name), acct, p.ID, tid)
			}
			return err
		})
		subList(run, &fatal, "team notification policies", tid, func() error {
			ps, err := ogList[ogPolicy](ctx, "/v2/policies/notification?teamId="+neturl.QueryEscape(tid))
			for _, p := range ps {
				addPair(inv, "notification_policy/"+tid+"/"+p.ID, label(p.ID, p.Name),
					"opsgenie:notification_policy", acct, tid, p.ID) // <team_id>/<policy_id>, team required
			}
			return err
		})
	}

	for _, u := range users {
		if u.ID == "" || fatal != nil {
			continue
		}
		uid, uname := u.ID, u.Username
		subList(run, &fatal, "user contacts", uid, func() error {
			cs, err := ogList[ogIDName](ctx, "/v2/users/"+neturl.PathEscape(uid)+"/contacts")
			for _, c := range cs {
				// import parent is the USERNAME (not the id) — the sharpest trap.
				addPair(inv, "user_contact/"+uid+"/"+c.ID, "contact-"+c.ID,
					"opsgenie:user_contact", acct, uname, c.ID) // <username>/<contact_id>
			}
			return err
		})
		subList(run, &fatal, "user notification rules", uid, func() error {
			rs, err := ogList[ogIDName](ctx, "/v2/users/"+neturl.PathEscape(uid)+"/notification-rules")
			for _, r := range rs {
				// import parent is the USER ID (not the username).
				addPair(inv, "notification_rule/"+uid+"/"+r.ID, label(r.ID, r.Name),
					"opsgenie:notification_rule", acct, uid, r.ID) // <user_id>/<rule_id>
			}
			return err
		})
	}

	for _, s := range services {
		if s.ID == "" || fatal != nil {
			continue
		}
		sid := s.ID
		subList(run, &fatal, "service incident rules", sid, func() error {
			rs, err := ogList[ogIDName](ctx, "/v1/services/"+neturl.PathEscape(sid)+"/incident-rules")
			for _, r := range rs {
				addPair(inv, "service_incident_rule/"+sid+"/"+r.ID, label(r.ID, r.Name),
					"opsgenie:service_incident_rule", acct, sid, r.ID) // <service_id>/<rule_id>, v1 path
			}
			return err
		})
	}

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("opsgenie enumeration failed on %d resource type(s) and found nothing — check OPSGENIE_API_KEY and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

func label(id, name string) string {
	if name != "" {
		return name
	}
	return id
}

// addBare adds a resource whose import id is a bare uuid or name.
func addBare(inv *model.Inventory, id, name, native, acct, token string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: acct, Source: "opsgenie-api", Properties: map[string]any{"token": token},
	}
}

// addPair adds a SLASH-composite resource; left/right are stored in import order (all
// Opsgenie composites use `/`).
func addPair(inv *model.Inventory, id, name, native, acct, left, right string) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: acct, Source: "opsgenie-api", Properties: map[string]any{"left": left, "right": right},
	}
}

// addAlertPolicy adds an alert policy, which imports BARE when global and <team_id>/<policy_id>
// when team-attached — team is "" for global. Defensive: once a policy is recorded as global
// (the authoritative global list), a later per-team pass must not flip it to team-scoped (a
// genuinely global policy must never inherit a ?teamId= from a stray fan-out result).
func addAlertPolicy(inv *model.Inventory, policyID, name, acct, token, team string) {
	key := "alert_policy/" + policyID
	if existing, ok := inv.Resources[key]; ok {
		if t, _ := existing.Properties["team"].(string); t == "" {
			return
		}
	}
	inv.Resources[key] = &model.Resource{
		ID: "alert_policy/" + policyID, Name: name, NativeType: "opsgenie:alert_policy",
		TFType: tfType("opsgenie:alert_policy"), Container: acct, Source: "opsgenie-api",
		Properties: map[string]any{"token": token, "team": team},
	}
}

// list runs a best-effort top-level enumeration closure and classifies any error: 401/403 →
// the key was revoked/insufficient, every remaining list will fail too, record it fatal;
// 404 → the feature is absent, skip quietly; anything else → Warn + count.
func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	if *fatal != nil {
		return
	}
	err := fn()
	if err == nil {
		return
	}
	var apiErr *opsgenieAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 404:
			run.Log.Verbose("Enumerate", "list %s skipped (feature absent): %v", what, err)
			return
		case 401, 403:
			if *fatal == nil {
				*fatal = fmt.Errorf("opsgenie authentication failed during enumeration (key revoked/insufficient): %w", err)
			}
			return
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// subList is the per-parent fan-out variant: 404 → Verbose skip; 401/403 → still fatal; other
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
	var apiErr *opsgenieAPIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == 404 {
			run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
			return
		}
		if apiErr.Status == 401 || apiErr.Status == 403 {
			if *fatal == nil {
				*fatal = fmt.Errorf("opsgenie authentication failed during enumeration (key revoked/insufficient): %w", err)
			}
			return
		}
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes ---------------------------------------------------

type ogIDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ogUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	FullName string `json:"fullName"`
}

type ogSchedule struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Rotations []ogIDName `json:"rotations"`
}

type ogIntegration struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type ogPolicy struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	TeamID string `json:"teamId"`
}

type ogHeartbeat struct {
	Name string `json:"name"`
}
