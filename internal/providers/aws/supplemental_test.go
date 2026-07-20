package aws

import (
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

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
