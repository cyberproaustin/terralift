package fastly

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for a Fastly customer: the services (each a single
// service-centric resource carrying its whole nested config tree), their versioned
// content companions (dictionary items / acl entries / dynamic snippet content), the
// account-plane TLS resources (JSON:API), service authorizations, and users. One flat
// container = the customer. Best-effort per list (403/404 → Verbose skip; other errors
// → Warn + count, so a systemic failure is distinguished from an empty account).
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	cust := run.Scope.ID
	run.Log.Info("Enumerate", "Fastly API: customer=%s", cust)

	inv := &model.Inventory{
		Cloud:       "fastly",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{cust: {ID: cust, Name: cust, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	list(run, &hardFails, &fatal, "services", func() error {
		svcs, err := fastlyListPaged[fastlyService](ctx, "/service")
		for _, s := range svcs {
			native := "fastly:service_vcl"
			if s.Type == "wasm" {
				native = "fastly:service_compute"
			}
			add(inv, "service/"+s.ID, s.Name, native, cust, map[string]any{"service_id": s.ID})
			if v := activeVersion(s); v > 0 {
				enumServiceContent(ctx, run, inv, cust, s.ID, s.Name, v)
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "tls subscriptions", func() error {
		items, err := fastlyListJSONAPI[fastlyItem](ctx, "/tls/subscriptions")
		for _, it := range items {
			add(inv, "tls_subscription/"+it.ID, "tls-subscription-"+it.ID, "fastly:tls_subscription", cust, map[string]any{"id": it.ID})
		}
		return err
	})
	list(run, &hardFails, &fatal, "tls activations", func() error {
		items, err := fastlyListJSONAPI[fastlyItem](ctx, "/tls/activations")
		for _, it := range items {
			add(inv, "tls_activation/"+it.ID, "tls-activation-"+it.ID, "fastly:tls_activation", cust, map[string]any{"id": it.ID})
		}
		return err
	})
	list(run, &hardFails, &fatal, "tls certificates", func() error {
		items, err := fastlyListJSONAPI[fastlyItem](ctx, "/tls/certificates")
		for _, it := range items {
			add(inv, "tls_certificate/"+it.ID, "tls-certificate-"+it.ID, "fastly:tls_certificate", cust, map[string]any{"id": it.ID})
		}
		return err
	})
	// Enumerated for visibility but excluded at export (write-only key_pem).
	list(run, &hardFails, &fatal, "tls private keys", func() error {
		items, err := fastlyListJSONAPI[fastlyItem](ctx, "/tls/private_keys")
		for _, it := range items {
			add(inv, "tls_private_key/"+it.ID, "tls-private-key-"+it.ID, "fastly:tls_private_key", cust, map[string]any{"id": it.ID})
		}
		return err
	})

	list(run, &hardFails, &fatal, "service authorizations", func() error {
		items, err := fastlyListJSONAPI[fastlyItem](ctx, "/service-authorizations")
		for _, it := range items {
			add(inv, "service_authorization/"+it.ID, "service-authorization-"+it.ID, "fastly:service_authorization", cust, map[string]any{"id": it.ID})
		}
		return err
	})

	list(run, &hardFails, &fatal, "users", func() error {
		us, err := fastlyGet[fastlyUser](ctx, "/customer/"+cust+"/users")
		for _, u := range us {
			name := u.Login
			if name == "" {
				name = u.ID
			}
			add(inv, "user/"+u.ID, name, "fastly:user", cust, map[string]any{"id": u.ID})
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("fastly enumeration failed on %d resource type(s) and found nothing — check FASTLY_API_KEY and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// enumServiceContent enumerates a service version's manageable dictionaries, ACLs, and
// dynamic snippets (the content companions). Private (write_only) dictionaries and
// static (dynamic==0) snippets are skipped — those live only as blocks on the service.
func enumServiceContent(ctx context.Context, run *core.Run, inv *model.Inventory, cust, sid, sname string, v int) {
	base := fmt.Sprintf("/service/%s/version/%d", sid, v)

	if dicts, err := fastlyGet[fastlyDict](ctx, base+"/dictionary"); err != nil {
		logSub(run, "dictionaries", sname, err)
	} else {
		for _, d := range dicts {
			if d.WriteOnly {
				continue // private dictionary; items cannot be managed by this resource
			}
			add(inv, "dictionary_items/"+sid+"/"+d.ID, sname+"-dict-"+d.Name, "fastly:dictionary_items", cust,
				map[string]any{"service_id": sid, "dictionary_id": d.ID})
		}
	}

	if acls, err := fastlyGet[fastlyACL](ctx, base+"/acl"); err != nil {
		logSub(run, "acls", sname, err)
	} else {
		for _, a := range acls {
			add(inv, "acl_entries/"+sid+"/"+a.ID, sname+"-acl-"+a.Name, "fastly:acl_entries", cust,
				map[string]any{"service_id": sid, "acl_id": a.ID})
		}
	}

	if snips, err := fastlyGet[fastlySnippet](ctx, base+"/snippet"); err != nil {
		logSub(run, "snippets", sname, err)
	} else {
		for _, s := range snips {
			if s.Dynamic != 1 {
				continue // static snippet — it is a block on the service, not a standalone resource
			}
			add(inv, "dynamic_snippet_content/"+sid+"/"+s.ID, sname+"-snippet-"+s.Name, "fastly:dynamic_snippet_content", cust,
				map[string]any{"service_id": sid, "snippet_id": s.ID})
		}
	}
}

// activeVersion returns the version the service resource manages: the active version,
// else the highest-numbered (matching the provider's active-then-latest rule).
func activeVersion(s fastlyService) int {
	max := 0
	for _, v := range s.Versions {
		if v.Active {
			return v.Number
		}
		if v.Number > max {
			max = v.Number
		}
	}
	return max
}

func add(inv *model.Inventory, id, name, native, container string, props map[string]any) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: container, Source: "fastly-api", Properties: props,
	}
}

func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *fastlyAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (feature/permission absent): %v", what, err)
			return
		case 401:
			// The token was revoked/expired mid-run; every remaining list will fail too,
			// so record it as fatal rather than shipping a partial inventory.
			if *fatal == nil {
				*fatal = fmt.Errorf("fastly authentication failed during enumeration (token revoked/expired): %w", err)
			}
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

func logSub(run *core.Run, what, parent string, err error) {
	var apiErr *fastlyAPIError
	if errors.As(err, &apiErr) && (apiErr.Status == 403 || apiErr.Status == 404) {
		run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
		return
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes ---------------------------------------------------

type fastlyService struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // "vcl" | "wasm"
	Versions []struct {
		Number int  `json:"number"`
		Active bool `json:"active"`
	} `json:"versions"`
}

type fastlyDict struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	WriteOnly bool   `json:"write_only"`
}

type fastlyACL struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type fastlySnippet struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Dynamic int    `json:"dynamic"`
}

// fastlyItem is a JSON:API resource object (id at the top level, not under attributes).
type fastlyItem struct {
	ID string `json:"id"`
}

type fastlyUser struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}
