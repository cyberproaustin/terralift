package aws

import (
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func TestDeriveImportID(t *testing.T) {
	cases := []struct {
		tfType, arn, want string
	}{
		// default: ARN's last segment (the bare id/name)
		{"aws_vpc", "arn:aws:ec2:us-east-1:123:vpc/vpc-0abc", "vpc-0abc"},
		{"aws_s3_bucket", "arn:aws:s3:::my-bucket", "my-bucket"},
		{"aws_iam_role", "arn:aws:iam::123:role/my-role", "my-role"},
		{"aws_security_group", "arn:aws:ec2:us-east-1:123:security-group/sg-1", "sg-1"},
		// full-ARN imports
		{"aws_sns_topic", "arn:aws:sns:us-east-1:123:my-topic", "arn:aws:sns:us-east-1:123:my-topic"},
		{"aws_iam_policy", "arn:aws:iam::123:policy/my-pol", "arn:aws:iam::123:policy/my-pol"},
		{"aws_lb", "arn:aws:elasticloadbalancing:us-east-1:123:loadbalancer/app/x/abc", "arn:aws:elasticloadbalancing:us-east-1:123:loadbalancer/app/x/abc"},
		// special forms
		{"aws_kms_alias", "arn:aws:kms:us-east-1:123:alias/my-key", "alias/my-key"},
		{"aws_sqs_queue", "arn:aws:sqs:us-east-1:123456789012:my-queue", "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue"},
	}
	for _, c := range cases {
		got := deriveImportID(&model.Resource{TFType: c.tfType, ID: c.arn})
		if got != c.want {
			t.Errorf("deriveImportID(%s, %s) = %q, want %q", c.tfType, c.arn, got, c.want)
		}
	}
}

func TestExcludedReason(t *testing.T) {
	// AWS-managed identities are excluded, not gapped.
	for _, arn := range []string{
		"arn:aws:iam::123:role/aws-service-role/autoscaling.amazonaws.com/AWSServiceRoleForAutoScaling",
		"arn:aws:iam::aws:policy/AdministratorAccess",
	} {
		if excludedReason(&model.Resource{ID: arn}) == "" {
			t.Errorf("%s should be excluded", arn)
		}
	}
	// User resources are not excluded.
	for _, arn := range []string{
		"arn:aws:iam::123:role/my-app-role",
		"arn:aws:s3:::my-bucket",
	} {
		if excludedReason(&model.Resource{ID: arn}) != "" {
			t.Errorf("%s should NOT be excluded", arn)
		}
	}
}

func TestAwsGeneratedID(t *testing.T) {
	// Auto-generated ids (rewire targets) match; human names do not.
	for _, id := range []string{"vpc-0bdc1f4c36d581928", "sg-0e1b6c40", "subnet-05441eb3841a34739", "rtb-0abc12345"} {
		if !awsGeneratedID.MatchString(id) {
			t.Errorf("%q should match awsGeneratedID", id)
		}
	}
	for _, id := range []string{"terralift-seed-events", "my-bucket", "terralift-seed-app", "AdministratorAccess"} {
		if awsGeneratedID.MatchString(id) {
			t.Errorf("%q should NOT match awsGeneratedID (would self-rewire a name)", id)
		}
	}
}

func TestDeriveImportIDHierarchical(t *testing.T) {
	cases := []struct{ tfType, arn, want string }{
		// slashed names must keep their full path (arnName truncation was the bug)
		{"aws_cloudwatch_log_group", "arn:aws:logs:us-east-1:123:log-group:/aws/lambda/fn", "/aws/lambda/fn"},
		{"aws_cloudwatch_log_group", "arn:aws:logs:us-east-1:123:log-group:/aws/lambda/fn:*", "/aws/lambda/fn"},
		{"aws_ssm_parameter", "arn:aws:ssm:us-east-1:123:parameter/my/app/db", "/my/app/db"},
		{"aws_ecs_service", "arn:aws:ecs:us-east-1:123:service/my-cluster/my-svc", "my-cluster/my-svc"},
		{"aws_kms_alias", "arn:aws:kms:us-east-1:123:alias/team/my-key", "alias/team/my-key"},
		{"aws_cloudwatch_event_rule", "arn:aws:events:us-east-1:123:rule/my-rule", "default/my-rule"},
		{"aws_cloudwatch_event_rule", "arn:aws:events:us-east-1:123:rule/my-bus/my-rule", "my-bus/my-rule"},
	}
	for _, c := range cases {
		if got := deriveImportID(&model.Resource{TFType: c.tfType, ID: c.arn}); got != c.want {
			t.Errorf("deriveImportID(%s, %s) = %q, want %q", c.tfType, c.arn, got, c.want)
		}
	}
}

func TestSqsURLPartition(t *testing.T) {
	if got := sqsURL("arn:aws-cn:sqs:cn-north-1:123:q"); got != "https://sqs.cn-north-1.amazonaws.com.cn/123/q" {
		t.Errorf("china sqsURL = %q", got)
	}
	if got := sqsURL("arn:aws:sqs:us-east-1:123:q"); got != "https://sqs.us-east-1.amazonaws.com/123/q" {
		t.Errorf("commercial sqsURL = %q", got)
	}
}
