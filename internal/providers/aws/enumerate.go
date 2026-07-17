package aws

import (
	"context"
	"strings"
	"time"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// reQueryAll is the Resource Explorer query that returns every indexed resource.
// An empty query string matches all resources the account's index can see;
// with a promoted aggregator index that is cross-region.
const reQueryAll = ""

// enumerate builds the canonical inventory from AWS Resource Explorer: a
// cross-region floor (ARN + type + region + tags), classified into per-region
// (or "global") containers. IAM/exposure enrichers are layered on top.
func enumerate(ctx context.Context, run *core.Run) (*model.Inventory, error) {
	account := run.Scope.ID
	if account == "" {
		got, err := stsAccount(ctx)
		if err != nil {
			return nil, err
		}
		account = got
		run.Scope = model.Scope{Type: model.ScopeAccount, ID: account}
	}
	run.Log.Info("Enumerate", "AWS Resource Explorer: account=%s", account)

	rows, err := reSearch(ctx, "", reQueryAll)
	if err != nil {
		return nil, err
	}

	inv := &model.Inventory{
		Cloud:       "aws",
		Scope:       run.Scope,
		GeneratedAt: time.Now().UTC(),
		Resources:   make(map[string]*model.Resource, len(rows)),
		Containers:  map[string]*model.Container{},
	}
	for _, r := range rows {
		res := reToResource(r)
		if res.ID == "" {
			continue
		}
		inv.Resources[strings.ToLower(res.ID)] = res
	}
	if want := containerSet(run.Config.Containers); want != nil {
		dropped := 0
		for id, res := range inv.Resources {
			if !want[strings.ToLower(res.Container)] {
				delete(inv.Resources, id)
				dropped++
			}
		}
		run.Log.Info("Enumerate", "region/container filter %v: kept %d, dropped %d", run.Config.Containers, len(inv.Resources), dropped)
	}

	mapped, regions := 0, map[string]bool{}
	for _, res := range inv.Resources {
		if res.TFType != "" {
			mapped++
		}
		regions[res.Container] = true
	}
	run.Log.Info("Enumerate", "floor: %d resources (%d tf-mapped, %d unmapped) across %d container(s)",
		len(inv.Resources), mapped, len(inv.Resources)-mapped, len(regions))

	enrichDefaults(ctx, run, inv)

	inv.Counts.Resources = len(inv.Resources)
	return inv, nil
}

func reToResource(r reResource) *model.Resource {
	return &model.Resource{
		ID:         r.ARN,
		Name:       arnName(r.ARN),
		NativeType: r.ResourceType,
		TFType:     awsTypeToTFType(r.ResourceType),
		Container:  containerFor(r),
		Location:   r.Region,
		Tags:       tagsFromProperties(r.Properties),
		Source:     "resource-explorer",
	}
}

// containerSet lowercases a region/container filter into a lookup set, or nil
// when empty (meaning "all containers").
func containerSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[strings.ToLower(n)] = true
	}
	return set
}
