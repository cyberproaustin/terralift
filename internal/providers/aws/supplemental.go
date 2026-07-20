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
