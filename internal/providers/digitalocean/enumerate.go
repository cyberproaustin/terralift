package digitalocean

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumerate builds the inventory for a DigitalOcean account: mostly flat account-level
// collections, plus three that fan out (domains→records, databases→sub-resources,
// kubernetes clusters→embedded node pools). One flat container = the account. Every
// list is best-effort — a 403/404 (feature/permission absent) is skipped at Verbose,
// anything else is Warned (enumeration may be silently incomplete).
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	acct := run.Scope.ID
	run.Log.Info("Enumerate", "DigitalOcean API: account=%s", acct)

	inv := &model.Inventory{
		Cloud:       "digitalocean",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{acct: {ID: acct, Name: acct, Type: model.ScopeTenant}},
	}

	hardFails := 0
	list(run, &hardFails, "droplets", func() error {
		ds, err := doList[doDroplet](ctx, "/droplets", "droplets")
		for _, d := range ds {
			add(inv, "droplet/"+strconv.Itoa(d.ID), d.Name, "digitalocean:droplet", acct, map[string]any{"droplet_id": strconv.Itoa(d.ID)})
		}
		return err
	})

	list(run, &hardFails, "domains", func() error {
		doms, err := doList[doDomain](ctx, "/domains", "domains")
		for _, dom := range doms {
			add(inv, "domain/"+dom.Name, dom.Name, "digitalocean:domain", acct, map[string]any{"name": dom.Name})
			recs, rerr := doList[doRecord](ctx, "/domains/"+dom.Name+"/records", "domain_records")
			if rerr != nil {
				logSub(run, "records", dom.Name, rerr)
				continue
			}
			for _, rec := range recs {
				add(inv, "record/"+dom.Name+"/"+strconv.Itoa(rec.ID), dom.Name+"-"+rec.Type,
					"digitalocean:record", acct, map[string]any{"domain": dom.Name, "record_id": strconv.Itoa(rec.ID), "type": rec.Type})
			}
		}
		return err
	})

	list(run, &hardFails, "firewalls", func() error {
		fs, err := doList[doUUIDNamed](ctx, "/firewalls", "firewalls")
		for _, f := range fs {
			add(inv, "firewall/"+f.ID, f.Name, "digitalocean:firewall", acct, map[string]any{"firewall_id": f.ID})
		}
		return err
	})

	list(run, &hardFails, "vpcs", func() error {
		vs, err := doList[doUUIDNamed](ctx, "/vpcs", "vpcs")
		for _, v := range vs {
			add(inv, "vpc/"+v.ID, v.Name, "digitalocean:vpc", acct, map[string]any{"vpc_id": v.ID})
		}
		return err
	})

	list(run, &hardFails, "ssh keys", func() error {
		ks, err := doList[doSSHKey](ctx, "/account/keys", "ssh_keys")
		for _, k := range ks {
			add(inv, "ssh_key/"+strconv.Itoa(k.ID), k.Name, "digitalocean:ssh_key", acct, map[string]any{"ssh_key_id": strconv.Itoa(k.ID)})
		}
		return err
	})

	list(run, &hardFails, "projects", func() error {
		ps, err := doList[doUUIDNamed](ctx, "/projects", "projects")
		for _, p := range ps {
			add(inv, "project/"+p.ID, p.Name, "digitalocean:project", acct, map[string]any{"project_id": p.ID})
		}
		return err
	})

	list(run, &hardFails, "load balancers", func() error {
		lbs, err := doList[doUUIDNamed](ctx, "/load_balancers", "load_balancers")
		for _, lb := range lbs {
			add(inv, "loadbalancer/"+lb.ID, lb.Name, "digitalocean:loadbalancer", acct, map[string]any{"lb_id": lb.ID})
		}
		return err
	})

	// reserved_ip and floating_ip are the same object under two names; adopt only
	// reserved_ip (current) to avoid managing an IP twice.
	list(run, &hardFails, "reserved ips", func() error {
		ips, err := doList[doIP](ctx, "/reserved_ips", "reserved_ips")
		for _, ip := range ips {
			add(inv, "reserved_ip/"+ip.IP, ip.IP, "digitalocean:reserved_ip", acct, map[string]any{"ip": ip.IP})
		}
		return err
	})
	list(run, &hardFails, "reserved ipv6", func() error {
		ips, err := doList[doIP](ctx, "/reserved_ipv6", "reserved_ipv6s")
		for _, ip := range ips {
			add(inv, "reserved_ipv6/"+ip.IP, ip.IP, "digitalocean:reserved_ipv6", acct, map[string]any{"ip": ip.IP})
		}
		return err
	})

	list(run, &hardFails, "certificates", func() error {
		cs, err := doList[doCert](ctx, "/certificates", "certificates")
		for _, c := range cs {
			// Both types are surfaced; custom certs are excluded from adoption at export
			// (write-only private key), like Cloudflare custom_ssl.
			add(inv, "certificate/"+c.Name, c.Name, "digitalocean:certificate", acct, map[string]any{"name": c.Name, "cert_type": c.Type})
		}
		return err
	})

	list(run, &hardFails, "cdn endpoints", func() error {
		es, err := doList[doUUIDNamed](ctx, "/cdn/endpoints", "endpoints")
		for _, e := range es {
			add(inv, "cdn/"+e.ID, "cdn-"+e.ID, "digitalocean:cdn", acct, map[string]any{"cdn_id": e.ID})
		}
		return err
	})

	list(run, &hardFails, "container registry", func() error {
		reg, err := doGetOne[doRegistry](ctx, "/registry", "registry")
		if err == nil && reg.Name != "" {
			add(inv, "container_registry/"+reg.Name, reg.Name, "digitalocean:container_registry", acct, map[string]any{"name": reg.Name})
		}
		return err
	})

	list(run, &hardFails, "kubernetes clusters", func() error {
		cs, err := doList[doK8sCluster](ctx, "/kubernetes/clusters", "kubernetes_clusters")
		for _, c := range cs {
			add(inv, "kubernetes_cluster/"+c.ID, c.Name, "digitalocean:kubernetes_cluster", acct, map[string]any{"cluster_id": c.ID})
			for _, np := range c.NodePools {
				if hasTag(np.Tags, "terraform:default-node-pool") {
					continue // belongs to the cluster resource; provider refuses to import it
				}
				add(inv, "kubernetes_node_pool/"+np.ID, c.Name+"-"+np.Name, "digitalocean:kubernetes_node_pool", acct, map[string]any{"pool_id": np.ID})
			}
		}
		return err
	})

	list(run, &hardFails, "database clusters", func() error {
		cs, err := doList[doUUIDNamed](ctx, "/databases", "databases")
		for _, c := range cs {
			add(inv, "database_cluster/"+c.ID, c.Name, "digitalocean:database_cluster", acct, map[string]any{"cluster_id": c.ID})
			enumDBSubs(ctx, run, inv, acct, c)
		}
		return err
	})

	list(run, &hardFails, "volumes", func() error {
		vs, err := doList[doUUIDNamed](ctx, "/volumes", "volumes")
		for _, v := range vs {
			add(inv, "volume/"+v.ID, v.Name, "digitalocean:volume", acct, map[string]any{"volume_id": v.ID})
		}
		return err
	})

	list(run, &hardFails, "tags", func() error {
		ts, err := doList[doTag](ctx, "/tags", "tags")
		for _, t := range ts {
			add(inv, "tag/"+t.Name, t.Name, "digitalocean:tag", acct, map[string]any{"name": t.Name})
		}
		return err
	})

	// If nothing was found AND lists failed with real (non-403/404) errors, this is a
	// systemic failure (revoked token, network drop) — surface it rather than shipping
	// an empty inventory that looks like an empty account.
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("digitalocean enumeration failed on %d resource type(s) and found nothing — check DIGITALOCEAN_TOKEN and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

// enumDBSubs enumerates a database cluster's dbs, users, connection pools, and
// replicas, skipping the DO-managed defaults (defaultdb / doadmin).
func enumDBSubs(ctx context.Context, run *core.Run, inv *model.Inventory, acct string, c doUUIDNamed) {
	subs := []struct {
		what, path, key, native, tf string
	}{
		{"dbs", "/databases/%s/dbs", "dbs", "digitalocean:database_db", "db"},
		{"users", "/databases/%s/users", "users", "digitalocean:database_user", "user"},
		{"pools", "/databases/%s/pools", "pools", "digitalocean:database_connection_pool", "pool"},
		{"replicas", "/databases/%s/replicas", "replicas", "digitalocean:database_replica", "replica"},
	}
	for _, s := range subs {
		items, err := doList[doDBSub](ctx, fmt.Sprintf(s.path, c.ID), s.key)
		if err != nil {
			logSub(run, s.what, c.Name, err)
			continue
		}
		for _, it := range items {
			// DO-managed defaults, not adoptable — scoped to the right sub-type so a
			// pool/replica legitimately named "doadmin" isn't wrongly skipped.
			if (s.what == "dbs" && it.Name == "defaultdb") || (s.what == "users" && it.Name == "doadmin") {
				continue
			}
			add(inv, s.tf+"/"+c.ID+"/"+it.Name, c.Name+"-"+it.Name, s.native, acct,
				map[string]any{"cluster_id": c.ID, "name": it.Name})
		}
	}
}

// add records a resource, resolving its Terraform type. Keyed by the raw (kind-
// namespaced, case-preserving) id.
func add(inv *model.Inventory, id, name, native, container string, props map[string]any) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: container, Source: "do-api", Properties: props,
	}
}

// list runs a best-effort account-level enumeration closure. 403/404 (feature/
// permission absent) is Verbose; anything else is Warn and increments *fails (so a
// systemic failure — revoked token, network drop — can be distinguished from a
// genuinely empty account rather than silently returning nothing).
func list(run *core.Run, fails *int, what string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *doAPIError
	if errors.As(err, &apiErr) && (apiErr.Status == 403 || apiErr.Status == 404) {
		run.Log.Verbose("Enumerate", "list %s skipped (feature/permission absent): %v", what, err)
		return
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

// logSub logs a nested sub-resource failure at Verbose (403/404) or Warn.
func logSub(run *core.Run, what, parent string, err error) {
	var apiErr *doAPIError
	if errors.As(err, &apiErr) && (apiErr.Status == 403 || apiErr.Status == 404) {
		run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
		return
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// --- API response shapes ---------------------------------------------------

type doDroplet struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type doDomain struct {
	Name string `json:"name"`
}

type doRecord struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// doUUIDNamed covers the many resources with a string (uuid) id + a name.
type doUUIDNamed struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type doSSHKey struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type doIP struct {
	IP string `json:"ip"`
}

type doCert struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type doRegistry struct {
	Name string `json:"name"`
}

type doDBSub struct {
	Name string `json:"name"`
}

type doTag struct {
	Name string `json:"name"`
}

type doK8sCluster struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	NodePools []struct {
		ID   string   `json:"id"`
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	} `json:"node_pools"`
}
