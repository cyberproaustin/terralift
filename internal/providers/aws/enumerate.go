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
	// Supplemental enumeration: some services are NOT indexed by Resource Explorer
	// (e.g. SecurityHub, Access Analyzer), so they never appear in the floor above.
	// These enumerators inject such resources via direct describe/list calls.
	enumSupplemental(ctx, run, inv)
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
	enrichManagedENIs(ctx, run, inv)
	enrichRDSEngines(ctx, run, inv)
	enrichExposure(ctx, run, inv)
	enrichPrivateZones(ctx, run, inv)
	markCaller(ctx, run, inv)

	inv.Counts.Resources = len(inv.Resources)
	return inv, nil
}

// markCaller flags the IAM identity TerraLift is authenticating AS (the onboarding
// principal), so the export excludes it — it is not part of any onboarded workload
// and, being a pre-existing account resource, can't be recreated on a rebuild
// (CreateUser -> EntityAlreadyExists). Analogous to GCP excluding the operating project.
func markCaller(ctx context.Context, run *core.Run, inv *model.Inventory) {
	arn, err := stsCallerARN(ctx)
	if err != nil || arn == "" {
		return
	}
	for _, r := range inv.Resources {
		if strings.EqualFold(r.ID, arn) {
			if r.Properties == nil {
				r.Properties = map[string]any{}
			}
			r.Properties["tl_caller"] = true
			run.Log.Info("Enumerate", "excluding the onboarding principal itself: %s", arn)
		}
	}
}

// enrichRDSEngines disambiguates the shared rds:cluster and rds:db Resource Explorer
// types by database engine. DocumentDB and Neptune are reported under the same rds:*
// types as Aurora/RDS, so without the engine a DocumentDB cluster would be mapped to
// aws_rds_cluster, fail to import, and drop to a gap. One describe-db-clusters /
// describe-db-instances per region (only where such resources exist) resolves it.
func enrichRDSEngines(ctx context.Context, run *core.Run, inv *model.Inventory) {
	regions := map[string]bool{}
	for _, r := range inv.Resources {
		if r.NativeType == "rds:cluster" || r.NativeType == "rds:db" {
			reg := r.Location
			if reg == "" {
				reg = "us-east-1"
			}
			regions[reg] = true
		}
	}
	if len(regions) == 0 {
		return
	}
	engineByARN := map[string]string{}
	for reg := range regions {
		var cl struct {
			DBClusters []struct {
				Arn    string `json:"DBClusterArn"`
				Engine string `json:"Engine"`
			} `json:"DBClusters"`
		}
		if err := runAws(ctx, &cl, "rds", "describe-db-clusters", "--region", reg); err == nil {
			for _, c := range cl.DBClusters {
				engineByARN[strings.ToLower(c.Arn)] = c.Engine
			}
		}
		var db struct {
			DBInstances []struct {
				Arn    string `json:"DBInstanceArn"`
				Engine string `json:"Engine"`
			} `json:"DBInstances"`
		}
		if err := runAws(ctx, &db, "rds", "describe-db-instances", "--region", reg); err == nil {
			for _, d := range db.DBInstances {
				engineByARN[strings.ToLower(d.Arn)] = d.Engine
			}
		}
	}
	n := 0
	for _, r := range inv.Resources {
		switch eng := engineByARN[strings.ToLower(r.ID)]; {
		case r.NativeType == "rds:cluster" && eng == "docdb":
			r.TFType = "aws_docdb_cluster"
			n++
		case r.NativeType == "rds:cluster" && eng == "neptune":
			r.TFType = "aws_neptune_cluster"
			n++
		case r.NativeType == "rds:db" && eng == "docdb":
			r.TFType = "aws_docdb_cluster_instance"
			n++
		case r.NativeType == "rds:db" && eng == "neptune":
			r.TFType = "aws_neptune_cluster_instance"
			n++
		}
	}
	if n > 0 {
		run.Log.Info("Enumerate", "rds engine: reclassified %d DocumentDB/Neptune resource(s)", n)
	}
}

func reToResource(r reResource) *model.Resource {
	tf := awsTypeToTFType(r.ResourceType)
	// Load balancers: Resource Explorer's coarse resourceType cannot reliably
	// distinguish a Classic ELB from a modern ALB/NLB/GWLB, but the ARN's resource
	// path always can (loadbalancer/app|net|gwy/… vs loadbalancer/<name>). Resolve
	// from the ARN so neither Classic nor v2 load balancers silently fall into a gap.
	if strings.HasPrefix(strings.ToLower(r.ResourceType), "elasticloadbalancing:loadbalancer") {
		tf = lbTypeFromARN(r.ARN)
	}
	name := arnName(r.ARN)
	// A CloudFormation stack ARN ends in the stack UUID; the human name is the
	// path segment before it, which is what the born-correct label should use.
	if strings.EqualFold(r.ResourceType, "cloudformation:stack") {
		name = cfnStackName(r.ARN)
	}
	return &model.Resource{
		ID:         r.ARN,
		Name:       name,
		NativeType: r.ResourceType,
		TFType:     tf,
		Container:  containerFor(r),
		Location:   r.Region,
		Tags:       tagsFromProperties(r.Properties),
		Source:     "resource-explorer",
	}
}

// lbTypeFromARN classifies an elasticloadbalancing loadbalancer ARN into its TF
// type: the resource path segment after "loadbalancer/" is app (ALB), net (NLB),
// or gwy (GWLB) for the v2 aws_lb, and anything else (a bare name) is a Classic
// aws_elb.
func lbTypeFromARN(arn string) string {
	parts := arnParts(arn) // arn:aws:elasticloadbalancing:region:acct:loadbalancer/...
	if len(parts) < 6 {
		return "aws_lb"
	}
	seg := strings.Split(parts[5], "/")
	if len(seg) >= 2 {
		switch seg[1] {
		case "app", "net", "gwy":
			return "aws_lb"
		}
	}
	return "aws_elb" // loadbalancer/<name> — Classic ELB
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
