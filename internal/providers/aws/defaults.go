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

// enrichManagedENIs marks network interfaces that are created and owned by an AWS
// service (RequesterManaged: ELB/NLB, VPC endpoints, NAT, RDS, Lambda, …) or that
// are attached to an instance (managed by that aws_instance). These are never
// onboarded standalone: generate-config-out emits them with invalid config
// (interface_type = "network_load_balancer", conflicting prefix args) that breaks
// `terraform plan`. Only a genuinely standalone, unattached user ENI survives.
func enrichManagedENIs(ctx context.Context, run *core.Run, inv *model.Inventory) {
	regions := map[string]bool{}
	for _, r := range inv.Resources {
		if r.NativeType == "ec2:network-interface" && r.Location != "" {
			regions[r.Location] = true
		}
	}
	managed := map[string]bool{}
	for region := range regions {
		ids, err := describeIDs(ctx, region, "ec2", "describe-network-interfaces",
			"--query", "NetworkInterfaces[?RequesterManaged==`true` || Attachment.InstanceId!=`null`].NetworkInterfaceId")
		if err != nil {
			run.Log.Warn("Enumerate", "ENI classification failed in %s (service ENIs may be mis-imported): %v", region, err)
			continue
		}
		for _, id := range ids {
			managed[id] = true
		}
	}
	n := 0
	for _, r := range inv.Resources {
		if r.NativeType == "ec2:network-interface" && managed[arnName(r.ID)] {
			if r.Properties == nil {
				r.Properties = map[string]any{}
			}
			r.Properties["tl_managed_eni"] = true
			n++
		}
	}
	if n > 0 {
		run.Log.Info("Enumerate", "marked %d service/instance-managed ENI(s) for exclusion", n)
	}
}

// isManagedENI reports whether an ENI was marked service/instance-managed.
func isManagedENI(r *model.Resource) bool {
	v, ok := r.Properties["tl_managed_eni"].(bool)
	return ok && v
}

// enrichExposure populates the public-reachability signals the hygiene/lockdown
// report needs — Resource Explorer's ARN floor carries none. Covers the two most
// common (and highest-impact) AWS exposures: an S3 bucket made public via its
// policy or ACL, and a security group that allows ingress from 0.0.0.0/0.
func enrichExposure(ctx context.Context, run *core.Run, inv *model.Inventory) {
	// S3 buckets — a per-bucket check (buckets are account-global).
	for _, r := range inv.Resources {
		if r.NativeType != "s3:bucket" {
			continue
		}
		if note := s3PublicNote(ctx, arnName(r.ID)); note != "" {
			r.Exposure.IsPubliclyExposed = true
			r.Exposure.Notes = append(r.Exposure.Notes, note)
		}
	}
	// Security groups with a 0.0.0.0/0 ingress rule, per region.
	regions := map[string]bool{}
	for _, r := range inv.Resources {
		if r.NativeType == "ec2:security-group" && r.Location != "" {
			regions[r.Location] = true
		}
	}
	openSG := map[string]bool{}
	for region := range regions {
		ids, err := describeIDs(ctx, region, "ec2", "describe-security-groups",
			"--query", "SecurityGroups[?IpPermissions[?IpRanges[?CidrIp=='0.0.0.0/0']]].GroupId")
		if err != nil {
			run.Log.Warn("Enumerate", "SG exposure check failed in %s: %v", region, err)
			continue
		}
		for _, id := range ids {
			openSG[id] = true
		}
	}
	exposed := 0
	for _, r := range inv.Resources {
		if r.NativeType == "ec2:security-group" && openSG[arnName(r.ID)] {
			r.Exposure.IsPubliclyExposed = true
			r.Exposure.Notes = append(r.Exposure.Notes, "security group allows ingress from 0.0.0.0/0")
		}
		if r.Exposure.IsPubliclyExposed {
			exposed++
		}
	}
	if exposed > 0 {
		run.Log.Info("Enumerate", "exposure: %d publicly-reachable resource(s) flagged", exposed)
	}
}

// enrichPrivateZones re-homes a PRIVATE Route53 hosted zone from the "global"
// container into its associated VPC's region. Route53 is a global service, but a
// private zone is tied to a VPC in a region; leaving it global splits it from its
// VPC across two stacks, so its `vpc_id` stays a literal that breaks on rebuild.
func enrichPrivateZones(ctx context.Context, run *core.Run, inv *model.Inventory) {
	for _, r := range inv.Resources {
		if r.NativeType != "route53:hostedzone" {
			continue
		}
		var resp struct {
			HostedZone struct {
				Config struct {
					PrivateZone bool `json:"PrivateZone"`
				} `json:"Config"`
			} `json:"HostedZone"`
			VPCs []struct {
				VPCRegion string `json:"VPCRegion"`
			} `json:"VPCs"`
		}
		if err := runAws(ctx, &resp, "route53", "get-hosted-zone", "--id", arnName(r.ID)); err != nil {
			continue
		}
		if resp.HostedZone.Config.PrivateZone && len(resp.VPCs) > 0 && resp.VPCs[0].VPCRegion != "" {
			r.Container = resp.VPCs[0].VPCRegion
			r.Location = resp.VPCs[0].VPCRegion
		}
	}
}

// s3PublicNote returns a reason string if the bucket is public (via policy status
// or an AllUsers/AuthenticatedUsers ACL grant), else "".
func s3PublicNote(ctx context.Context, bucket string) string {
	var isPublic bool
	if err := runAws(ctx, &isPublic, "s3api", "get-bucket-policy-status", "--bucket", bucket, "--query", "PolicyStatus.IsPublic"); err == nil && isPublic {
		return "S3 bucket is public via its bucket policy"
	}
	var uris []string
	if err := runAws(ctx, &uris, "s3api", "get-bucket-acl", "--bucket", bucket, "--query", "Grants[?Grantee.URI!=null].Grantee.URI"); err == nil {
		for _, u := range uris {
			if strings.Contains(u, "AllUsers") || strings.Contains(u, "AuthenticatedUsers") {
				return "S3 bucket has a public ACL grant (AllUsers/AuthenticatedUsers)"
			}
		}
	}
	return ""
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
