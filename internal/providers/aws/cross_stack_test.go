package aws

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func TestCrossStackRoles(t *testing.T) {
	inv := &model.Inventory{Resources: map[string]*model.Resource{
		"a": {ID: "arn:aws:iam::123456789012:role/app", NativeType: "iam:role", TFType: "aws_iam_role"},
		"b": {ID: "arn:aws:iam::123456789012:role/aws-service-role/ecs.amazonaws.com/AWSServiceRoleForECS", NativeType: "iam:role", TFType: "aws_iam_role"},
		"c": {ID: "arn:aws:s3:::bucket", NativeType: "s3:bucket", TFType: "aws_s3_bucket"},
	}}
	roles := crossStackRoles(inv)
	if roles["arn:aws:iam::123456789012:role/app"] != "app" {
		t.Errorf("onboarded role missing/wrong: %v", roles)
	}
	if len(roles) != 1 {
		t.Errorf("expected only the onboardable role (service-linked excluded, bucket ignored), got %v", roles)
	}
}

func TestRewireCrossStackRoleRefs(t *testing.T) {
	dir := t.TempDir()
	gen := filepath.Join(dir, "generated.tf")
	src := `resource "aws_sfn_state_machine" "sm" {
  role_arn = "arn:aws:iam::123:role/app"
}
resource "aws_codebuild_project" "cb" {
  service_role = "arn:aws:iam::123:role/notonboarded"
}`
	if err := os.WriteFile(gen, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{"arn:aws:iam::123:role/app": "app"}
	if n := rewireCrossStackRoleRefs(gen, roles); n != 1 {
		t.Errorf("rewired %d, want 1", n)
	}
	out, _ := os.ReadFile(gen)
	s := string(out)
	if !strings.Contains(s, "role_arn = data.aws_iam_role.app.arn") {
		t.Errorf("onboarded role_arn not rewired to the data source:\n%s", s)
	}
	if !strings.Contains(s, "data \"aws_iam_role\" \"app\" {\n  name = \"app\"\n}") {
		t.Errorf("data source not emitted:\n%s", s)
	}
	if !strings.Contains(s, `service_role = "arn:aws:iam::123:role/notonboarded"`) {
		t.Errorf("a role that is not onboarded must stay a literal ARN:\n%s", s)
	}
}
