package aws

import (
	"strings"
	"testing"
)

// These characterization tests pin curateBlock's per-type behavior so later
// refactors of the curation engine cannot silently change the generated HCL.

func curate(t *testing.T, typ string, body ...string) (string, int) {
	t.Helper()
	block := append([]string{"resource \"" + typ + "\" \"x\" {"}, body...)
	block = append(block, "}")
	out, ev := curateBlock(typ, block, "")
	return strings.Join(out, "\n"), len(ev)
}

func TestCurateSSMParameterPlaceholder(t *testing.T) {
	out, ev := curate(t, "aws_ssm_parameter", `  name = "/x"`, `  type = "String"`)
	if !strings.Contains(out, `value = "REPLACE_ME"`) || !strings.Contains(out, "ignore_changes = [value]") {
		t.Errorf("SSM placeholder not injected:\n%s", out)
	}
	if ev != 1 {
		t.Errorf("want 1 placeholder event, got %d", ev)
	}
}

func TestCurateKMSKeyBypass(t *testing.T) {
	out, _ := curate(t, "aws_kms_key", `  policy = "x"`)
	if !strings.Contains(out, "bypass_policy_lockout_safety_check = true") ||
		!strings.Contains(out, "ignore_changes = [bypass_policy_lockout_safety_check]") {
		t.Errorf("KMS bypass not injected:\n%s", out)
	}
}

func TestCurateECSServiceDropsIAMRole(t *testing.T) {
	out, _ := curate(t, "aws_ecs_service", `  iam_role = "arn:aws:iam::x:role/r"`, `  desired_count = 1`)
	if strings.Contains(out, "iam_role") {
		t.Errorf("iam_role not dropped:\n%s", out)
	}
	if !strings.Contains(out, "ignore_changes = [wait_for_steady_state]") {
		t.Errorf("ECS ignore_changes not injected:\n%s", out)
	}
}

func TestCurateVPCEndpointDropsGatewayDNSOptions(t *testing.T) {
	out, _ := curate(t, "aws_vpc_endpoint",
		`  vpc_endpoint_type = "Gateway"`,
		`  dns_options {`,
		`    dns_record_ip_type = "ipv4"`,
		`  }`,
	)
	if strings.Contains(out, "dns_options") {
		t.Errorf("Gateway endpoint dns_options not dropped:\n%s", out)
	}
	if !strings.Contains(out, `vpc_endpoint_type = "Gateway"`) {
		t.Errorf("dropped too much:\n%s", out)
	}
}

func TestCurateLBListenerDropsForward(t *testing.T) {
	out, _ := curate(t, "aws_lb_listener",
		`  target_group_arn = "arn:aws:elasticloadbalancing:x"`,
		`  forward {`,
		`    target_group { arn = "y" }`,
		`  }`,
	)
	if strings.Contains(out, "forward {") {
		t.Errorf("conflicting forward block not dropped:\n%s", out)
	}
	if !strings.Contains(out, "target_group_arn") {
		t.Errorf("shorthand target_group_arn should be kept:\n%s", out)
	}
}

func TestZeroOverEmitPrune(t *testing.T) {
	// Invalid over-emitted zeros must be dropped; real (non-zero) values kept, and an
	// unrelated legitimate zero (e.g. desired_capacity = 0) must NOT match.
	for _, l := range []string{"  concurrent_build_limit = 0", "      timeout_in_minutes = 0"} {
		if !zeroOverEmit.MatchString(l) {
			t.Errorf("should drop: %q", l)
		}
	}
	for _, l := range []string{"  concurrent_build_limit = 5", "  timeout_in_minutes = 60", "  desired_capacity = 0", "  min_size = 0"} {
		if zeroOverEmit.MatchString(l) {
			t.Errorf("should keep: %q", l)
		}
	}
}

func TestCurateAutoScalingGroup(t *testing.T) {
	out, _ := curate(t, "aws_autoscaling_group",
		`  availability_zones  = ["us-east-1a"]`,
		`  vpc_zone_identifier = ["subnet-1"]`,
		`  launch_template {`,
		`    id      = "lt-1"`,
		`    name    = "my-lt"`,
		`    version = "$Latest"`,
		`  }`,
	)
	if strings.Contains(out, "availability_zones") {
		t.Errorf("availability_zones (conflicts vpc_zone_identifier) not dropped:\n%s", out)
	}
	if strings.Contains(out, `name    = "my-lt"`) {
		t.Errorf("nested launch_template name (conflicts id) not dropped:\n%s", out)
	}
	if !strings.Contains(out, `id      = "lt-1"`) || !strings.Contains(out, "vpc_zone_identifier") {
		t.Errorf("dropped too much:\n%s", out)
	}
	if !strings.Contains(out, "ignore_changes = [force_delete, force_delete_warm_pool, ignore_failed_scaling_activities, wait_for_capacity_timeout]") {
		t.Errorf("ASG ignore_changes not injected:\n%s", out)
	}
}

func TestCurateLaunchTemplateSGNames(t *testing.T) {
	out, _ := curate(t, "aws_launch_template",
		`  security_group_names   = []`,
		`  vpc_security_group_ids = []`,
	)
	if strings.Contains(out, "security_group_names") {
		t.Errorf("security_group_names (conflicts vpc_security_group_ids) not dropped:\n%s", out)
	}
	if !strings.Contains(out, "vpc_security_group_ids") {
		t.Errorf("vpc_security_group_ids should be kept:\n%s", out)
	}
}

func TestCurateSFNEncryptionBlock(t *testing.T) {
	out, _ := curate(t, "aws_sfn_state_machine",
		`  type = "STANDARD"`,
		`  encryption_configuration {`,
		`    kms_data_key_reuse_period_seconds = 0`,
		`    type                              = "AWS_OWNED_KEY"`,
		`  }`,
	)
	if strings.Contains(out, "encryption_configuration") || strings.Contains(out, "kms_data_key_reuse_period_seconds") {
		t.Errorf("AWS_OWNED_KEY encryption_configuration block not dropped:\n%s", out)
	}
	if !strings.Contains(out, `type = "STANDARD"`) {
		t.Errorf("dropped too much:\n%s", out)
	}
}

func TestReplaceSFNDefinition(t *testing.T) {
	block := []string{
		`resource "aws_sfn_state_machine" "sm" {`,
		`  definition = jsonencode({`,
		`    StartAt = "Pass"`,
		`    States  = { Pass = { Type = "Pass", End = true } }`,
		`  })`,
		`  name = "sm"`,
		`}`,
	}
	raw := `{"StartAt":"Pass","States":{"Pass":{"Type":"Pass","End":true}}}`
	out, replaced := replaceSFNDefinition(block, raw)
	if !replaced {
		t.Fatal("definition not replaced")
	}
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "jsonencode(") {
		t.Errorf("jsonencode not removed:\n%s", joined)
	}
	if !strings.Contains(joined, `definition = "{\"StartAt\":\"Pass\"`) {
		t.Errorf("literal definition not written:\n%s", joined)
	}
	if !strings.Contains(joined, `name = "sm"`) {
		t.Errorf("rest of block corrupted:\n%s", joined)
	}
}

func TestCurateVPCPeeringAutoAccept(t *testing.T) {
	out, _ := curate(t, "aws_vpc_peering_connection",
		`  vpc_id = "vpc-1"`,
		`  peer_vpc_id = "vpc-2"`,
		`  peer_region = "us-west-2"`,
	)
	if strings.Contains(out, "peer_region") {
		t.Errorf("same-account peer_region not dropped:\n%s", out)
	}
	if !strings.Contains(out, "auto_accept = true") || !strings.Contains(out, "ignore_changes = [auto_accept]") {
		t.Errorf("auto_accept not injected:\n%s", out)
	}
}
