package aws

import (
	"strings"

	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the Terraform import ID for a resource. AWS import IDs
// are wildly inconsistent per type: most are the bare resource id (the ARN's
// last segment — vpc-…, sg-…, a bucket/role/function name), but some import by
// full ARN, some preserve an embedded-slash path, and a few need a reconstructed
// form. The result is HCL-template-escaped for parity with the other providers.
func deriveImportID(r *model.Resource) string {
	var id string
	if fn, ok := importIDOverride[r.TFType]; ok {
		id = fn(r)
	} else if fn, ok := importIDOverrideExtra[r.TFType]; ok {
		id = fn(r) // full-ARN imports from the native sweep (coverage.go)
	} else {
		id = arnName(r.ID)
	}
	return util.EscapeHCLTemplate(id)
}

// importIDOverride holds types whose import ID is NOT the ARN's last segment.
// Source: the hashicorp/aws provider per-resource "Import" docs.
var importIDOverride = map[string]func(r *model.Resource) string{
	// Import by full ARN.
	"aws_sns_topic":             byARN,
	"aws_secretsmanager_secret": byARN,
	"aws_iam_policy":            byARN,
	"aws_lb":                    byARN,
	"aws_lb_target_group":       byARN,
	"aws_lb_listener":           byARN,
	"aws_ecs_task_definition":   byARN, // imports by ARN, not the family:revision the ARN's last segment gives
	// A key pair imports by its NAME, but Resource Explorer reports it by id
	// (key-…). authorKeyPairs stashes the real KeyName on the resource once fetched.
	"aws_key_pair": func(r *model.Resource) string {
		if kn, _ := r.Properties["tl_keyname"].(string); kn != "" {
			return kn
		}
		return arnName(r.ID)
	},

	// Hierarchical / slashed names — the ARN's last segment truncates them, so
	// take the resource part (after the 5th ':') and strip the type token,
	// PRESERVING embedded slashes.
	"aws_cloudwatch_log_group": func(r *model.Resource) string { // -> /aws/lambda/x
		s := strings.TrimPrefix(arnResource(r.ID), "log-group:")
		return strings.TrimSuffix(s, ":*")
	},
	"aws_ssm_parameter": func(r *model.Resource) string { // -> /my/app/param
		return strings.TrimPrefix(arnResource(r.ID), "parameter")
	},
	"aws_ecs_service": func(r *model.Resource) string { // -> cluster/service
		return strings.TrimPrefix(arnResource(r.ID), "service/")
	},
	// KMS alias imports as the full "alias/…" (may contain slashes).
	"aws_kms_alias": func(r *model.Resource) string { return arnResource(r.ID) },
	// EventBridge rules import as "{bus}/{rule}" (bare bus "default").
	"aws_cloudwatch_event_rule": func(r *model.Resource) string {
		s := strings.TrimPrefix(arnResource(r.ID), "rule/")
		if strings.Contains(s, "/") { // custom bus already: bus/rule
			return s
		}
		return "default/" + s
	},

	// SQS imports by queue URL, reconstructed from the ARN.
	"aws_sqs_queue": func(r *model.Resource) string { return sqsURL(r.ID) },

	// A CloudFormation stack imports by its NAME; the ARN's last segment is the
	// stack UUID, so extract the name from the ARN path.
	"aws_cloudformation_stack": func(r *model.Resource) string { return cfnStackName(r.ID) },

	// SecurityHub is enabled per account+region; aws_securityhub_account imports by
	// the ACCOUNT ID, not the hub ARN (whose last segment is "default").
	"aws_securityhub_account": func(r *model.Resource) string {
		if p := arnParts(r.ID); len(p) >= 5 {
			return p[4] // account id
		}
		return arnName(r.ID)
	},

	// Identity Center resources import by the composite "identity-store-id/resource-id",
	// which the supplemental enumerator already stores as the resource ID.
	"aws_identitystore_user":             byARN,
	"aws_identitystore_group":            byARN,
	"aws_identitystore_group_membership": byARN,

	// An org policy attachment imports by "target-id:policy-id" (stored as the id).
	"aws_organizations_policy_attachment": byARN,
}

// cfnStackName extracts the stack NAME from a CloudFormation stack ARN
// (arn:aws:cloudformation:region:acct:stack/<name>/<uuid>). The ARN's last segment
// is the stack UUID, but the resource is named and imported by <name>.
func cfnStackName(arn string) string {
	parts := strings.Split(arnResource(arn), "/") // "stack/<name>/<uuid>"
	if len(parts) >= 2 {
		return parts[1]
	}
	return arnName(arn)
}

func byARN(r *model.Resource) string { return r.ID }

// arnParts splits an ARN into its 6 top-level fields:
// arn:partition:service:region:account-id:resource
func arnParts(arn string) []string { return strings.SplitN(arn, ":", 6) }

// arnResource returns the resource part of an ARN (everything after the 5th
// colon), which may itself contain slashes and colons.
func arnResource(arn string) string {
	p := arnParts(arn)
	if len(p) < 6 {
		return arnName(arn)
	}
	return p[5]
}

// sqsURL reconstructs the SQS queue URL (its import id) from the queue ARN.
// arn:<partition>:sqs:<region>:<account>:<name> -> https://sqs.<region>.<dns>/<account>/<name>
// where <dns> is amazonaws.com (or amazonaws.com.cn in the China partition).
func sqsURL(arn string) string {
	p := arnParts(arn)
	if len(p) < 6 {
		return arnName(arn)
	}
	partition, region, account, name := p[1], p[3], p[4], p[5]
	dns := "amazonaws.com"
	if partition == "aws-cn" {
		dns = "amazonaws.com.cn"
	}
	return "https://sqs." + region + "." + dns + "/" + account + "/" + name
}
