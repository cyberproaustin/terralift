package aws

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
	"github.com/cyberproaustin/terralift/internal/model"
)

// mockAWS substitutes a fake runAws that returns canned JSON keyed by the
// longest (most specific) command substring that matches, restoring the real one
// when the test ends. Not for use with t.Parallel (runAws is a shared package var).
func mockAWS(t *testing.T, responses map[string]string) {
	t.Helper()
	orig := runAws
	t.Cleanup(func() { runAws = orig })
	runAws = func(_ context.Context, v any, args ...string) error {
		joined := strings.Join(args, " ")
		best := ""
		for key := range responses {
			if strings.Contains(joined, key) && len(key) > len(best) {
				best = key
			}
		}
		if best == "" {
			return nil // unmatched command -> leave v as its zero value
		}
		return json.Unmarshal([]byte(responses[best]), v)
	}
}

func testRun() *core.Run {
	return &core.Run{Log: core.NewLogger(core.ParseLevel("error"))}
}

func tfTypesIn(inv *model.Inventory) map[string]bool {
	out := map[string]bool{}
	for _, r := range inv.Resources {
		out[r.TFType] = true
	}
	return out
}

func TestEnumSecurityHub(t *testing.T) {
	mockAWS(t, map[string]string{
		"describe-hub":          `{"HubArn":"arn:aws:securityhub:us-east-1:123456789012:hub/default"}`,
		"get-enabled-standards": `{"StandardsSubscriptions":[{"StandardsSubscriptionArn":"arn:aws:securityhub:us-east-1:123456789012:subscription/cis-aws-foundations-benchmark/v/1.2.0"}]}`,
	})
	inv := &model.Inventory{Resources: map[string]*model.Resource{"seed": {Location: "us-east-1"}}}
	enumSecurityHub(context.Background(), testRun(), inv)
	got := tfTypesIn(inv)
	if !got["aws_securityhub_account"] || !got["aws_securityhub_standards_subscription"] {
		t.Errorf("expected securityhub account + subscription, got %v", got)
	}
}

func TestEnumOrganizations(t *testing.T) {
	mockAWS(t, map[string]string{
		"describe-organization":  `{"Organization":{"Id":"o-abc","MasterAccountId":"123456789012"}}`,
		"list-roots":             `{"Roots":[{"Id":"r-1"}]}`,
		"--parent-id r-1":        `{"OrganizationalUnits":[{"Id":"ou-1","Name":"ou-one"}]}`,
		"--parent-id ou-1":       `{"OrganizationalUnits":[]}`,
		"SERVICE_CONTROL_POLICY": `{"Policies":[{"Id":"p-1","Name":"scp","AwsManaged":false},{"Id":"p-full","Name":"FullAWSAccess","AwsManaged":true}]}`,
		"list-accounts":          `{"Accounts":[{"Id":"123456789012","Name":"mgmt"},{"Id":"999888777666","Name":"member"}]}`,
	})
	inv := &model.Inventory{Resources: map[string]*model.Resource{}}
	enumOrganizations(context.Background(), testRun(), inv)
	got := tfTypesIn(inv)
	for _, want := range []string{"aws_organizations_organization", "aws_organizations_organizational_unit", "aws_organizations_policy", "aws_organizations_account"} {
		if !got[want] {
			t.Errorf("missing %s; got %v", want, got)
		}
	}
	if inv.Resources["p-full"] != nil {
		t.Error("AWS-managed policy should be skipped")
	}
	if inv.Resources["123456789012"] != nil {
		t.Error("management account should be skipped")
	}
	if inv.Resources["999888777666"] == nil {
		t.Error("member account should be onboarded")
	}
}

func TestEnumIdentityStore(t *testing.T) {
	mockAWS(t, map[string]string{
		"list-instances":         `{"Instances":[{"IdentityStoreId":"d-123"}]}`,
		"list-users":             `{"Users":[{"UserId":"u-1","UserName":"user1"}]}`,
		"list-groups":            `{"Groups":[{"GroupId":"g-1","DisplayName":"group1"}]}`,
		"list-group-memberships": `{"GroupMemberships":[{"MembershipId":"m-1"}]}`,
	})
	inv := &model.Inventory{Resources: map[string]*model.Resource{}}
	enumIdentityStore(context.Background(), testRun(), inv)
	// Composite IDs: identity-store-id/resource-id.
	if inv.Resources["d-123/u-1"] == nil || inv.Resources["d-123/u-1"].TFType != "aws_identitystore_user" {
		t.Errorf("user not injected with composite id: %v", inv.Resources)
	}
	got := tfTypesIn(inv)
	if !got["aws_identitystore_group"] || !got["aws_identitystore_group_membership"] {
		t.Errorf("missing group/membership: %v", got)
	}
}

func TestEnrichRDSEngines(t *testing.T) {
	mockAWS(t, map[string]string{
		"describe-db-clusters":  `{"DBClusters":[{"DBClusterArn":"arn:aws:rds:us-east-1:123:cluster:doc","Engine":"docdb"}]}`,
		"describe-db-instances": `{"DBInstances":[{"DBInstanceArn":"arn:aws:rds:us-east-1:123:db:doci","Engine":"docdb"}]}`,
	})
	inv := &model.Inventory{Resources: map[string]*model.Resource{
		"arn:aws:rds:us-east-1:123:cluster:doc": {ID: "arn:aws:rds:us-east-1:123:cluster:doc", NativeType: "rds:cluster", TFType: "aws_rds_cluster", Location: "us-east-1"},
		"arn:aws:rds:us-east-1:123:db:doci":     {ID: "arn:aws:rds:us-east-1:123:db:doci", NativeType: "rds:db", TFType: "aws_db_instance", Location: "us-east-1"},
	}}
	enrichRDSEngines(context.Background(), testRun(), inv)
	if got := inv.Resources["arn:aws:rds:us-east-1:123:cluster:doc"].TFType; got != "aws_docdb_cluster" {
		t.Errorf("docdb cluster reclassified to %q, want aws_docdb_cluster", got)
	}
	if got := inv.Resources["arn:aws:rds:us-east-1:123:db:doci"].TFType; got != "aws_docdb_cluster_instance" {
		t.Errorf("docdb instance reclassified to %q, want aws_docdb_cluster_instance", got)
	}
}

func TestStandardsSubName(t *testing.T) {
	arn := "arn:aws:securityhub:us-east-1:521595302924:subscription/cis-aws-foundations-benchmark/v/1.2.0"
	if got := standardsSubName(arn); got != "cis-aws-foundations-benchmark-v-1.2.0" {
		t.Errorf("standardsSubName = %q, want cis-aws-foundations-benchmark-v-1.2.0", got)
	}
}

func TestSecurityHubAccountImportID(t *testing.T) {
	// aws_securityhub_account imports by the ACCOUNT ID, not the hub ARN.
	r := &model.Resource{TFType: "aws_securityhub_account", ID: "arn:aws:securityhub:us-east-1:521595302924:hub/default"}
	if got := deriveImportID(r); got != "521595302924" {
		t.Errorf("import id = %q, want 521595302924", got)
	}
}

func TestInventoryRegions(t *testing.T) {
	inv := &model.Inventory{Resources: map[string]*model.Resource{
		"a": {Location: "us-east-1"},
		"b": {Location: "us-west-2"},
		"c": {Location: "us-east-1"},  // dup
		"d": {Location: ""},           // ignored
		"e": {Location: "aws-global"}, // ignored
	}}
	if got := inventoryRegions(inv); len(got) != 2 {
		t.Errorf("regions = %v, want 2 distinct", got)
	}
	// Empty inventory falls back to us-east-1 so a supplemental enumerator still runs.
	if got := inventoryRegions(&model.Inventory{Resources: map[string]*model.Resource{}}); len(got) != 1 || got[0] != "us-east-1" {
		t.Errorf("fallback = %v, want [us-east-1]", got)
	}
}
