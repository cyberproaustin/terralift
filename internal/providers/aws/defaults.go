package aws

import (
	"context"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enrichDefaults marks AWS-managed default networking so the export can exclude
// it — the default VPC (and its subnets/IGW), plus every VPC's auto-created
// default security group, default network ACL, and main route table. These are
// created automatically, can't be managed as regular resources, and importing
// them would break a destroy/rebuild round-trip. Detection needs describe calls
// (the ARN-only Resource Explorer floor can't tell a default apart). Best-effort:
// a describe failure just leaves those resources unmarked.
func enrichDefaults(ctx context.Context, run *core.Run, inv *model.Inventory) {
	regions := map[string]bool{}
	for _, r := range inv.Resources {
		if r.Container != "global" && r.Location != "" {
			regions[r.Location] = true
		}
	}

	def := map[string]bool{}
	add := func(ids []string) {
		for _, id := range ids {
			if id != "" {
				def[id] = true
			}
		}
	}
	for region := range regions {
		vpcs, _ := describeIDs(ctx, region, "ec2", "describe-vpcs", "--filters", "Name=is-default,Values=true", "--query", "Vpcs[].VpcId")
		add(vpcs)
		// Every VPC's auto-created default SG / NACL / main route table.
		sgs, _ := describeIDs(ctx, region, "ec2", "describe-security-groups", "--filters", "Name=group-name,Values=default", "--query", "SecurityGroups[].GroupId")
		add(sgs)
		nacls, _ := describeIDs(ctx, region, "ec2", "describe-network-acls", "--filters", "Name=default,Values=true", "--query", "NetworkAcls[].NetworkAclId")
		add(nacls)
		rts, _ := describeIDs(ctx, region, "ec2", "describe-route-tables", "--filters", "Name=association.main,Values=true", "--query", "RouteTables[].RouteTableId")
		add(rts)
		if len(vpcs) > 0 {
			v := strings.Join(vpcs, ",")
			subnets, err1 := describeIDs(ctx, region, "ec2", "describe-subnets", "--filters", "Name=vpc-id,Values="+v, "--query", "Subnets[].SubnetId")
			add(subnets)
			igws, err2 := describeIDs(ctx, region, "ec2", "describe-internet-gateways", "--filters", "Name=attachment.vpc-id,Values="+v, "--query", "InternetGateways[].InternetGatewayId")
			add(igws)
			// A partial describe failure would silently leave default subnets/IGWs
			// unmarked → imported → broken round-trip. Surface it loudly.
			if err1 != nil || err2 != nil {
				run.Log.Warn("Enumerate", "defaults: region %s has a default VPC but a child describe failed (subnets/IGWs may be mis-imported): %v %v", region, err1, err2)
			}
		}
	}

	marked := 0
	for _, r := range inv.Resources {
		if def[arnName(r.ID)] {
			if r.Properties == nil {
				r.Properties = map[string]any{}
			}
			r.Properties["tl_default"] = true
			marked++
		}
	}
	run.Log.Info("Enumerate", "defaults: marked %d AWS-managed default resource(s) for exclusion", marked)
}

// describeIDs runs an `aws ec2 describe-*` with a --query that projects a flat
// list of ids, in the given region. Returns (nil, err) on failure so callers can
// decide whether a partial failure is worth surfacing.
func describeIDs(ctx context.Context, region string, args ...string) ([]string, error) {
	var ids []string
	full := append(append([]string{}, args...), "--region", region)
	if err := runAws(ctx, &ids, full...); err != nil {
		return nil, err
	}
	return ids, nil
}

// isDefault reports whether a resource was marked AWS-managed-default.
func isDefault(r *model.Resource) bool {
	v, ok := r.Properties["tl_default"].(bool)
	return ok && v
}
