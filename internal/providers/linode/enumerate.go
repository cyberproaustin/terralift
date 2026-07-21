package linode

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

// enumerate builds the inventory for a Linode account: instances, DNS domains + records,
// firewalls, nodebalancers (+ configs + nodes, a two-level fan-out), volumes,
// stackscripts, LKE clusters, VPCs + subnets, private images, customized rDNS, ssh keys,
// object-storage buckets, and managed databases. One flat container. Best-effort per
// list (403/404 → Verbose; 401 → fatal; other errors → Warn + count).
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	acct := run.Scope.ID
	run.Log.Info("Enumerate", "Linode API: account=%s", acct)

	inv := &model.Inventory{
		Cloud:       "linode",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   map[string]*model.Resource{},
		Containers:  map[string]*model.Container{acct: {ID: acct, Name: acct, Type: model.ScopeTenant}},
	}
	hardFails := 0
	var fatal error

	list(run, &hardFails, &fatal, "instances", func() error {
		xs, err := linodeList[linodeObj](ctx, "/linode/instances", "")
		for _, x := range xs {
			id := itoa(x.ID)
			add(inv, "instance/"+id, nm(x.Label, "instance", id), "linode:instance", acct, map[string]any{"id": id})
		}
		return err
	})

	list(run, &hardFails, &fatal, "domains", func() error {
		ds, err := linodeList[linodeDomain](ctx, "/domains", "")
		for _, d := range ds {
			did := itoa(d.ID)
			add(inv, "domain/"+did, nm(d.Domain, "domain", did), "linode:domain", acct, map[string]any{"id": did})
			recs, rerr := linodeList[linodeObj](ctx, "/domains/"+did+"/records", "")
			if rerr != nil {
				logSub(run, "records", d.Domain, rerr)
				continue
			}
			for _, rec := range recs {
				rid := itoa(rec.ID)
				add(inv, "record/"+did+"/"+rid, nm(rec.Name, "record", rid), "linode:domain_record", acct,
					map[string]any{"domain_id": did, "record_id": rid})
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "firewalls", func() error {
		xs, err := linodeList[linodeObj](ctx, "/networking/firewalls", "")
		for _, x := range xs {
			id := itoa(x.ID)
			add(inv, "firewall/"+id, nm(x.Label, "firewall", id), "linode:firewall", acct, map[string]any{"id": id})
		}
		return err
	})

	list(run, &hardFails, &fatal, "nodebalancers", func() error {
		nbs, err := linodeList[linodeObj](ctx, "/nodebalancers", "")
		for _, nb := range nbs {
			nbID := itoa(nb.ID)
			add(inv, "nodebalancer/"+nbID, nm(nb.Label, "nodebalancer", nbID), "linode:nodebalancer", acct, map[string]any{"id": nbID})
			cfgs, cerr := linodeList[linodeObj](ctx, "/nodebalancers/"+nbID+"/configs", "")
			if cerr != nil {
				logSub(run, "nb configs", nbID, cerr)
				continue
			}
			for _, cfg := range cfgs {
				cfgID := itoa(cfg.ID)
				add(inv, "nb_config/"+nbID+"/"+cfgID, "nbconfig-"+cfgID, "linode:nodebalancer_config", acct,
					map[string]any{"nodebalancer_id": nbID, "config_id": cfgID})
				nodes, nerr := linodeList[linodeObj](ctx, "/nodebalancers/"+nbID+"/configs/"+cfgID+"/nodes", "")
				if nerr != nil {
					logSub(run, "nb nodes", cfgID, nerr)
					continue
				}
				for _, n := range nodes {
					nID := itoa(n.ID)
					add(inv, "nb_node/"+nbID+"/"+cfgID+"/"+nID, nm(n.Label, "nbnode", nID), "linode:nodebalancer_node", acct,
						map[string]any{"nodebalancer_id": nbID, "config_id": cfgID, "node_id": nID})
				}
			}
		}
		return err
	})

	list(run, &hardFails, &fatal, "volumes", func() error {
		xs, err := linodeList[linodeObj](ctx, "/volumes", "")
		for _, x := range xs {
			id := itoa(x.ID)
			add(inv, "volume/"+id, nm(x.Label, "volume", id), "linode:volume", acct, map[string]any{"id": id})
		}
		return err
	})

	// Only account-owned stackscripts (X-Filter mine:true; is_public==false backstop).
	list(run, &hardFails, &fatal, "stackscripts", func() error {
		xs, err := linodeList[linodeObj](ctx, "/linode/stackscripts", `{"mine": true}`)
		for _, x := range xs {
			if x.IsPublic {
				continue
			}
			id := itoa(x.ID)
			add(inv, "stackscript/"+id, nm(x.Label, "stackscript", id), "linode:stackscript", acct, map[string]any{"id": id})
		}
		return err
	})

	list(run, &hardFails, &fatal, "lke clusters", func() error {
		xs, err := linodeList[linodeObj](ctx, "/lke/clusters", "")
		for _, x := range xs {
			id := itoa(x.ID)
			add(inv, "lke_cluster/"+id, nm(x.Label, "lke", id), "linode:lke_cluster", acct, map[string]any{"id": id})
		}
		return err
	})

	list(run, &hardFails, &fatal, "vpcs", func() error {
		vs, err := linodeList[linodeObj](ctx, "/vpcs", "")
		for _, v := range vs {
			vid := itoa(v.ID)
			add(inv, "vpc/"+vid, nm(v.Label, "vpc", vid), "linode:vpc", acct, map[string]any{"id": vid})
			subs, serr := linodeList[linodeObj](ctx, "/vpcs/"+vid+"/subnets", "")
			if serr != nil {
				logSub(run, "vpc subnets", vid, serr)
				continue
			}
			for _, s := range subs {
				sid := itoa(s.ID)
				add(inv, "vpc_subnet/"+vid+"/"+sid, nm(s.Label, "subnet", sid), "linode:vpc_subnet", acct,
					map[string]any{"vpc_id": vid, "subnet_id": sid})
			}
		}
		return err
	})

	// Only account-created (private) images (X-Filter is_public:false; backstop below).
	list(run, &hardFails, &fatal, "images", func() error {
		imgs, err := linodeList[linodeImage](ctx, "/images", `{"is_public": false}`)
		for _, img := range imgs {
			if img.IsPublic {
				continue
			}
			add(inv, "image/"+img.ID, nm(img.Label, "image", img.ID), "linode:image", acct, map[string]any{"id": img.ID})
		}
		return err
	})

	// Only customized rDNS (skip the default *.ip.linodeusercontent.com / *.members.linode.com PTRs).
	list(run, &hardFails, &fatal, "rdns", func() error {
		ips, err := linodeList[linodeIP](ctx, "/networking/ips", "")
		for _, ip := range ips {
			if ip.RDNS == "" || strings.HasSuffix(ip.RDNS, ".ip.linodeusercontent.com") || strings.HasSuffix(ip.RDNS, ".members.linode.com") {
				continue
			}
			add(inv, "rdns/"+ip.Address, ip.Address, "linode:rdns", acct, map[string]any{"address": ip.Address})
		}
		return err
	})

	list(run, &hardFails, &fatal, "ssh keys", func() error {
		ks, err := linodeList[linodeObj](ctx, "/profile/sshkeys", "")
		for _, k := range ks {
			id := itoa(k.ID)
			add(inv, "sshkey/"+id, nm(k.Label, "sshkey", id), "linode:sshkey", acct, map[string]any{"id": id})
		}
		return err
	})

	list(run, &hardFails, &fatal, "object storage buckets", func() error {
		bs, err := linodeList[linodeBucket](ctx, "/object-storage/buckets", "")
		for _, b := range bs {
			add(inv, "bucket/"+b.Region+"/"+b.Label, b.Label, "linode:object_storage_bucket", acct,
				map[string]any{"region": b.Region, "label": b.Label})
		}
		return err
	})

	list(run, &hardFails, &fatal, "mysql databases", func() error {
		xs, err := linodeList[linodeObj](ctx, "/databases/mysql/instances", "")
		for _, x := range xs {
			id := itoa(x.ID)
			add(inv, "database_mysql/"+id, nm(x.Label, "mysql", id), "linode:database_mysql", acct, map[string]any{"id": id})
		}
		return err
	})
	list(run, &hardFails, &fatal, "postgresql databases", func() error {
		xs, err := linodeList[linodeObj](ctx, "/databases/postgresql/instances", "")
		for _, x := range xs {
			id := itoa(x.ID)
			add(inv, "database_postgresql/"+id, nm(x.Label, "postgresql", id), "linode:database_postgresql", acct, map[string]any{"id": id})
		}
		return err
	})

	if fatal != nil {
		return nil, fatal
	}
	if len(inv.Resources) == 0 && hardFails > 0 {
		return nil, fmt.Errorf("linode enumeration failed on %d resource type(s) and found nothing — check LINODE_TOKEN and network connectivity", hardFails)
	}

	inv.Counts.Resources = len(inv.Resources)
	inv.Counts.Containers = len(inv.Containers)
	run.Log.Info("Enumerate", "inventory: %d resources", len(inv.Resources))
	return inv, nil
}

func add(inv *model.Inventory, id, name, native, container string, props map[string]any) {
	inv.Resources[id] = &model.Resource{
		ID: id, Name: name, NativeType: native, TFType: tfType(native),
		Container: container, Source: "linode-api", Properties: props,
	}
}

func nm(label, kind, id string) string {
	if label != "" {
		return label
	}
	return kind + "-" + id
}

func itoa(i int) string { return strconv.Itoa(i) }

func list(run *core.Run, fails *int, fatal *error, what string, fn func() error) {
	err := fn()
	if err == nil {
		return
	}
	var apiErr *linodeAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 403, 404:
			run.Log.Verbose("Enumerate", "list %s skipped (feature/permission absent): %v", what, err)
			return
		case 401:
			if *fatal == nil {
				*fatal = fmt.Errorf("linode authentication failed during enumeration (token revoked/invalid): %w", err)
			}
		}
	}
	*fails++
	run.Log.Warn("Enumerate", "list %s failed — enumeration may be incomplete: %v", what, err)
}

func logSub(run *core.Run, what, parent string, err error) {
	var apiErr *linodeAPIError
	if errors.As(err, &apiErr) && (apiErr.Status == 403 || apiErr.Status == 404) {
		run.Log.Verbose("Enumerate", "list %s for %s skipped: %v", what, parent, err)
		return
	}
	run.Log.Warn("Enumerate", "list %s for %s failed — may be incomplete: %v", what, parent, err)
}

// --- API response shapes ---------------------------------------------------

// linodeObj covers the many resources with an int id + optional label/name.
type linodeObj struct {
	ID       int    `json:"id"`
	Label    string `json:"label"`
	Name     string `json:"name"` // domain records use "name"
	IsPublic bool   `json:"is_public"`
}

type linodeDomain struct {
	ID     int    `json:"id"`
	Domain string `json:"domain"`
}

type linodeImage struct {
	ID       string `json:"id"` // "private/<n>"
	Label    string `json:"label"`
	IsPublic bool   `json:"is_public"`
}

type linodeIP struct {
	Address string `json:"address"`
	RDNS    string `json:"rdns"`
}

type linodeBucket struct {
	Region string `json:"region"`
	Label  string `json:"label"`
}
