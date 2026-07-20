package aws

import (
	"context"
	"strings"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// enumSupplemental runs the per-service supplemental enumerators for resources
// Resource Explorer does NOT index. Each injects its own resource types into the
// inventory via direct describe/list calls. Add a new RE-blind service here by
// writing an enumerator and calling it.
func enumSupplemental(ctx context.Context, run *core.Run, inv *model.Inventory) {
	enumSecurityHub(ctx, run, inv)
	enumOrganizations(ctx, run, inv)
	enumIdentityStore(ctx, run, inv)
}

// tryAws runs a best-effort secondary describe/list, returning true on success. On
// failure it logs at Verbose and returns false, so a throttled or permission-denied
// call leaves a trace instead of silently under-enumerating while the phase still
// reports a success count.
func tryAws(ctx context.Context, run *core.Run, v any, args ...string) bool {
	if err := runAws(ctx, v, args...); err != nil {
		op := args
		if len(op) > 2 {
			op = op[:2]
		}
		run.Log.Verbose("Enumerate", "supplemental %q skipped: %v", strings.Join(op, " "), err)
		return false
	}
	return true
}

// enumIdentityStore injects IAM Identity Center identity-store resources, which
// Resource Explorer does not index: users, groups, and group memberships. No-op if
// Identity Center is not enabled. Each imports by the COMPOSITE id
// "identity-store-id/resource-id" (stored as the resource ID; see the byID overrides
// in importid.go). All global.
func enumIdentityStore(ctx context.Context, run *core.Run, inv *model.Inventory) {
	var inst struct {
		Instances []struct {
			IdentityStoreId string `json:"IdentityStoreId"`
		} `json:"Instances"`
	}
	if err := runAws(ctx, &inst, "sso-admin", "list-instances"); err != nil || len(inst.Instances) == 0 {
		return
	}
	store := inst.Instances[0].IdentityStoreId
	if store == "" {
		return
	}
	add := func(resourceID, name, tfType, nativeSuffix string) {
		id := store + "/" + resourceID // the composite Terraform import id
		inv.Resources[strings.ToLower(id)] = &model.Resource{
			ID: id, Name: name, NativeType: "identitystore:" + nativeSuffix,
			TFType: tfType, Container: "global", Source: "supplemental",
		}
	}
	added := 0

	var users struct {
		Users []struct {
			UserId   string `json:"UserId"`
			UserName string `json:"UserName"`
		} `json:"Users"`
	}
	if tryAws(ctx, run, &users, "identitystore", "list-users", "--identity-store-id", store) {
		for _, u := range users.Users {
			add(u.UserId, u.UserName, "aws_identitystore_user", "user")
			added++
		}
	}

	var groups struct {
		Groups []struct {
			GroupId     string `json:"GroupId"`
			DisplayName string `json:"DisplayName"`
		} `json:"Groups"`
	}
	if tryAws(ctx, run, &groups, "identitystore", "list-groups", "--identity-store-id", store) {
		for _, g := range groups.Groups {
			add(g.GroupId, g.DisplayName, "aws_identitystore_group", "group")
			added++
			var mems struct {
				GroupMemberships []struct {
					MembershipId string `json:"MembershipId"`
				} `json:"GroupMemberships"`
			}
			if tryAws(ctx, run, &mems, "identitystore", "list-group-memberships", "--identity-store-id", store, "--group-id", g.GroupId) {
				for _, m := range mems.GroupMemberships {
					add(m.MembershipId, g.DisplayName+"-membership", "aws_identitystore_group_membership", "group-membership")
					added++
				}
			}
		}
	}
	if added > 0 {
		run.Log.Info("Enumerate", "supplemental (Identity Center): %d resource(s)", added)
	}
}

// enumOrganizations injects AWS Organizations resources, which Resource Explorer
// does not index: the organization, its OUs (recursively), custom (non-AWS-managed)
// policies, and member accounts. All are global. No-op if the account is not part
// of an organization. The management account is skipped — it is the org owner, not
// a manageable member resource. All import by their native id (o-/ou-/p-/account id),
// which is stored as the resource ID, so no import-id override is needed.
func enumOrganizations(ctx context.Context, run *core.Run, inv *model.Inventory) {
	var org struct {
		Organization struct {
			Id              string `json:"Id"`
			MasterAccountId string `json:"MasterAccountId"`
		} `json:"Organization"`
	}
	if err := runAws(ctx, &org, "organizations", "describe-organization"); err != nil || org.Organization.Id == "" {
		return // not in an organization (or no access)
	}
	add := func(id, name, tfType, nativeSuffix string) {
		inv.Resources[strings.ToLower(id)] = &model.Resource{
			ID: id, Name: name, NativeType: "organizations:" + nativeSuffix,
			TFType: tfType, Container: "global", Source: "supplemental",
		}
	}
	added := 1
	add(org.Organization.Id, "organization", "aws_organizations_organization", "organization")

	var roots struct {
		Roots []struct {
			Id string `json:"Id"`
		} `json:"Roots"`
	}
	if tryAws(ctx, run, &roots, "organizations", "list-roots") {
		for _, root := range roots.Roots {
			added += enumOrgOUs(ctx, run, inv, root.Id, add)
		}
	}

	for _, ptype := range []string{"SERVICE_CONTROL_POLICY", "TAG_POLICY", "BACKUP_POLICY", "AISERVICES_OPT_OUT_POLICY"} {
		var pols struct {
			Policies []struct {
				Id         string `json:"Id"`
				Name       string `json:"Name"`
				AwsManaged bool   `json:"AwsManaged"`
			} `json:"Policies"`
		}
		if tryAws(ctx, run, &pols, "organizations", "list-policies", "--filter", ptype) {
			for _, p := range pols.Policies {
				if p.AwsManaged {
					continue // AWS-managed (e.g. FullAWSAccess) is not onboardable
				}
				add(p.Id, p.Name, "aws_organizations_policy", "policy")
				added++
				// One aws_organizations_policy_attachment per target the policy is
				// attached to; it imports by "target-id:policy-id".
				var targets struct {
					Targets []struct {
						TargetId string `json:"TargetId"`
					} `json:"Targets"`
				}
				if tryAws(ctx, run, &targets, "organizations", "list-targets-for-policy", "--policy-id", p.Id) {
					for _, tg := range targets.Targets {
						add(tg.TargetId+":"+p.Id, p.Name+"-"+tg.TargetId, "aws_organizations_policy_attachment", "policy-attachment")
						added++
					}
				}
			}
		}
	}

	var accts struct {
		Accounts []struct {
			Id   string `json:"Id"`
			Name string `json:"Name"`
		} `json:"Accounts"`
	}
	if tryAws(ctx, run, &accts, "organizations", "list-accounts") {
		for _, a := range accts.Accounts {
			if a.Id == org.Organization.MasterAccountId {
				continue // the management account is not a manageable member resource
			}
			add(a.Id, a.Name, "aws_organizations_account", "account")
			added++
		}
	}
	run.Log.Info("Enumerate", "supplemental (Organizations): %d resource(s)", added)
}

// enumOrgOUs recursively injects the OUs under parentID and returns the count added.
func enumOrgOUs(ctx context.Context, run *core.Run, inv *model.Inventory, parentID string, add func(id, name, tfType, nativeSuffix string)) int {
	var ous struct {
		OrganizationalUnits []struct {
			Id   string `json:"Id"`
			Name string `json:"Name"`
		} `json:"OrganizationalUnits"`
	}
	if !tryAws(ctx, run, &ous, "organizations", "list-organizational-units-for-parent", "--parent-id", parentID) {
		return 0
	}
	n := 0
	for _, ou := range ous.OrganizationalUnits {
		add(ou.Id, ou.Name, "aws_organizations_organizational_unit", "ou")
		n++
		n += enumOrgOUs(ctx, run, inv, ou.Id, add)
	}
	return n
}

// inventoryRegions returns the distinct AWS regions present in the floor (falling
// back to us-east-1), so a supplemental enumerator only queries the regions the
// account actually uses rather than sweeping every region.
func inventoryRegions(inv *model.Inventory) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range inv.Resources {
		loc := r.Location
		if loc == "" || loc == "aws-global" || seen[loc] {
			continue
		}
		seen[loc] = true
		out = append(out, loc)
	}
	if len(out) == 0 {
		return []string{"us-east-1"}
	}
	return out
}

// enumSecurityHub injects SecurityHub resources, which Resource Explorer does not
// index: the per-region aws_securityhub_account and its
// aws_securityhub_standards_subscription resources, via describe-hub +
// get-enabled-standards. No-op in regions where SecurityHub is not enabled.
func enumSecurityHub(ctx context.Context, run *core.Run, inv *model.Inventory) {
	added := 0
	for _, reg := range inventoryRegions(inv) {
		var hub struct {
			HubArn string `json:"HubArn"`
		}
		if err := runAws(ctx, &hub, "securityhub", "describe-hub", "--region", reg); err != nil || hub.HubArn == "" {
			continue // not enabled in this region (or no access)
		}
		inv.Resources[strings.ToLower(hub.HubArn)] = &model.Resource{
			ID:         hub.HubArn,
			Name:       "securityhub-" + reg,
			NativeType: "securityhub:hub",
			TFType:     "aws_securityhub_account",
			Container:  reg,
			Location:   reg,
			Source:     "supplemental",
		}
		added++

		var std struct {
			StandardsSubscriptions []struct {
				StandardsSubscriptionArn string `json:"StandardsSubscriptionArn"`
			} `json:"StandardsSubscriptions"`
		}
		if err := runAws(ctx, &std, "securityhub", "get-enabled-standards", "--region", reg); err != nil {
			continue
		}
		for _, s := range std.StandardsSubscriptions {
			arn := s.StandardsSubscriptionArn
			inv.Resources[strings.ToLower(arn)] = &model.Resource{
				ID:         arn,
				Name:       standardsSubName(arn),
				NativeType: "securityhub:standards-subscription",
				TFType:     "aws_securityhub_standards_subscription",
				Container:  reg,
				Location:   reg,
				Source:     "supplemental",
			}
			added++
		}
	}
	if added > 0 {
		run.Log.Info("Enumerate", "supplemental (SecurityHub): %d resource(s)", added)
	}
}

// standardsSubName derives a readable label from a standards-subscription ARN
// (arn:...:subscription/<standard>/v/<version>).
func standardsSubName(arn string) string {
	res := strings.TrimPrefix(arnResource(arn), "subscription/")
	return strings.ReplaceAll(res, "/", "-")
}
