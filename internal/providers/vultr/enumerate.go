package vultr

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for a Vultr account: instances, bare metal, DNS
// domains + records, firewall groups + rules, block storage, load balancers, VPCs
// (v1 + v2), ssh keys, reserved IPs, startup scripts, VKE clusters, managed databases,
// and object storage. One flat container. Best-effort per list (403/404 → Verbose;
// 401 → fatal; other errors → Warn + count).
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	acct := run.Scope.ID
	run.Log.Info("Enumerate", "Vultr API: account=%s", acct)

	inv := &model.Inventory{
		Cloud:       "vultr",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{acct: {ID: acct, Name: acct, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	// flat enumerates a top-level UUID-keyed collection.
	flat := func(what, path, key, native string) {
		list(run, &hardFails, &fatal, what, func() error {
			xs, err := vultrList[vultrObj](ctx, path, key)
			short := strings.TrimPrefix(native, "vultr:")
			for _, x := range xs {
				add(inv, short+"/"+x.ID, nm(label(x), short, x.ID), native, acct, map[string]any{"id": x.ID})
			}
			return err
		})
	}

	flat("instances", "/instances", "instances", "vultr:instance")
	flat("bare metal servers", "/bare-metals", "bare_metals", "vultr:bare_metal_server")
	flat("block storage", "/blocks", "blocks", "vultr:block_storage")
	flat("load balancers", "/load-balancers", "load_balancers", "vultr:load_balancer")
	flat("vpcs", "/vpcs", "vpcs", "vultr:vpc")
	flat("vpc2", "/vpc2", "vpcs", "vultr:vpc2") // NB: vpc and vpc2 BOTH use the "vpcs" key
	flat("ssh keys", "/ssh-keys", "ssh_keys", "vultr:ssh_key")
	flat("reserved ips", "/reserved-ips", "reserved_ips", "vultr:reserved_ip")
	flat("startup scripts", "/startup-scripts", "startup_scripts", "vultr:startup_script")
	flat("kubernetes clusters", "/kubernetes/clusters", "vke_clusters", "vultr:kubernetes")
	flat("databases", "/databases", "databases", "vultr:database")
	flat("object storage", "/object-storage", "object_storages", "vultr:object_storage")

	// domains → records
	list(run, &hardFails, &fatal, "domains", func() error {
		ds, err := vultrList[vultrDomain](ctx, "/domains", "domains")
		for _, d := range ds {
			add(inv, "dns_domain/"+d.Domain, d.Domain, "vultr:dns_domain", acct, map[string]any{"domain": d.Domain})
			recs, rerr := vultrList[vultrObj](ctx, "/domains/"+d.Domain+"/records", "records")
			if rerr != nil {
				logSub(run, "records", d.Domain, rerr)
				continue
			}
			for _, rec := range recs {
				add(inv, "dns_record/"+d.Domain+"/"+rec.ID, nm(rec.Name, "record", rec.ID), "vultr:dns_record", acct,
					map[string]any{"domain": d.Domain, "record_id": rec.ID})
			}
		}
		return err
	})

	// firewall groups → rules (one call per group returns all v4+v6 rules)
	list(run, &hardFails, &fatal, "firewall groups", func() error {
		gs, err := vultrList[vultrObj](ctx, "/firewalls", "firewall_groups")
		for _, g := range gs {
			add(inv, "firewall_group/"+g.ID, nm(label(g), "firewall", g.ID), "vultr:firewall_group", acct, map[string]any{"id": g.ID})
			rules, rerr := vultrList[vultrRule](ctx, "/firewalls/"+g.ID+"/rules", "firewall_rules")
			if rerr != nil {
				logSub(run, "firewall rules", g.ID, rerr)
				continue
			}
			for _, r := range rules {
				rid := strconv.Itoa(r.ID) // the firewall rule id is an INTEGER
				add(inv, "firewall_rule/"+g.ID+"/"+rid, "fwrule-"+rid, "vultr:firewall_rule", acct,
					map[string]any{"firewall_group_id": g.ID, "rule_id": rid})
			}
		}
		return err
	})

	// NB: vultr_kubernetes_node_pools is deliberately NOT enumerated as a standalone
	// resource here. A cluster's INITIAL pool lives inside vultr_kubernetes.node_pools
	// AND is returned by the node-pools list; adopting it separately would double-manage
	// it, and the initial-vs-additional distinction needs live QA (see spec). The cluster
	// (with its inline pools) is adopted above; the type/import id are wired for Phase B.

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("vultr enumeration failed on %d resource type(s) and found nothing — check VULTR_API_KEY and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

func add(inv *model.Inventory, id, name, native, container string, props map[string]any) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: container, Source: "vultr-api", Properties: props,
	}
}

// label returns the first non-empty human field on an object.
func label(x vultrObj) string {
	for _, s := range []string{x.Label, x.Name, x.Description} {
		if s != "" {
			return s
		}
	}
	return ""
}

func nm(preferred, kind, id string) string {
	if preferred != "" {
		return preferred
	}
	return kind + "-" + id
}

func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *vultrAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (feature/permission absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("vultr authentication failed during enumeration (key revoked/invalid): %w", err)
			}
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

func logSub(run *core.Run, what, parent string, err error) {
	var apiErr *vultrAPIError
	if errors.As(err, &apiErr) && (apiErr.Status == 403 || apiErr.Status == 404) {
		run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
		return
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes ---------------------------------------------------

// vultrObj covers the many resources with a UUID id + an optional human field.
type vultrObj struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type vultrDomain struct {
	Domain string `json:"domain"`
}

type vultrRule struct {
	ID int `json:"id"` // firewall rule id is an integer
}
