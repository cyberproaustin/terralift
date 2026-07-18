package aws

import (
	"encoding/json"
	"strings"
)

// awsTypeToTF maps an AWS Resource Explorer resource type ("service:resource")
// to its aws_* Terraform type. Best-effort and incremental; "" => coverage gap.
var awsTypeToTF = map[string]string{
	// networking
	"ec2:vpc":               "aws_vpc",
	"ec2:subnet":            "aws_subnet",
	"ec2:security-group":    "aws_security_group",
	"ec2:route-table":       "aws_route_table",
	"ec2:internet-gateway":  "aws_internet_gateway",
	"ec2:natgateway":        "aws_nat_gateway",
	"ec2:elastic-ip":        "aws_eip",
	"ec2:network-acl":       "aws_network_acl",
	"ec2:network-interface": "aws_network_interface",
	"ec2:vpc-endpoint":      "aws_vpc_endpoint",
	"ec2:dhcp-options":      "aws_vpc_dhcp_options",
	"ec2:prefix-list":       "aws_ec2_managed_prefix_list",
	"ec2:launch-template":   "aws_launch_template",
	"ec2:vpc-flow-log":      "aws_flow_log",
	// compute / storage
	"ec2:instance":    "aws_instance",
	"ec2:volume":      "aws_ebs_volume",
	"ec2:snapshot":    "aws_ebs_snapshot",
	"ec2:key-pair":    "aws_key_pair",
	"ec2:image":       "aws_ami",
	"s3:bucket":       "aws_s3_bucket",
	"lambda:function": "aws_lambda_function",
	// data
	"rds:db":              "aws_db_instance",
	"rds:cluster":         "aws_rds_cluster",
	"rds:subgrp":          "aws_db_subnet_group",
	"dynamodb:table":      "aws_dynamodb_table",
	"elasticache:cluster": "aws_elasticache_cluster",
	// messaging
	"sns:topic":   "aws_sns_topic",
	"sqs:queue":   "aws_sqs_queue",
	"events:rule": "aws_cloudwatch_event_rule",
	// containers
	"ecs:cluster":    "aws_ecs_cluster",
	"ecs:service":    "aws_ecs_service",
	"ecr:repository": "aws_ecr_repository",
	"eks:cluster":    "aws_eks_cluster",
	// edge / lb / dns. NOTE: for loadbalancer, reToResource resolves aws_lb vs the
	// Classic aws_elb from the ARN (app/net/gwy => aws_lb), which is authoritative;
	// these entries are the fallback + the suffixed forms Resource Explorer may report.
	"elasticloadbalancing:loadbalancer":     "aws_elb", // Classic (bare); v2 via ARN
	"elasticloadbalancing:loadbalancer/app": "aws_lb",  // ALB
	"elasticloadbalancing:loadbalancer/net": "aws_lb",  // NLB
	"elasticloadbalancing:loadbalancer/gwy": "aws_lb",  // GWLB
	"elasticloadbalancing:targetgroup":      "aws_lb_target_group",
	"elasticloadbalancing:listener":         "aws_lb_listener",
	"elasticloadbalancing:listener/app":     "aws_lb_listener",
	"elasticloadbalancing:listener/net":     "aws_lb_listener",
	"elasticloadbalancing:listener/gwy":     "aws_lb_listener",
	"cloudfront:distribution":               "aws_cloudfront_distribution",
	"route53:hostedzone":                    "aws_route53_zone",
	"apigateway:restapis":                   "aws_api_gateway_rest_api",
	// security / ops
	"iam:role":              "aws_iam_role",
	"iam:policy":            "aws_iam_policy",
	"iam:user":              "aws_iam_user",
	"iam:group":             "aws_iam_group",
	"iam:instance-profile":  "aws_iam_instance_profile",
	"kms:key":               "aws_kms_key",
	"kms:alias":             "aws_kms_alias",
	"secretsmanager:secret": "aws_secretsmanager_secret",
	"ssm:parameter":         "aws_ssm_parameter",
	"logs:log-group":        "aws_cloudwatch_log_group",
	"cloudwatch:alarm":      "aws_cloudwatch_metric_alarm",
	"events:event-bus":      "aws_cloudwatch_event_bus",
	"athena:workgroup":      "aws_athena_workgroup",
	"athena:datacatalog":    "aws_athena_data_catalog",
	"xray:sampling-rule":    "aws_xray_sampling_rule",
}

// awsManagedDefault reports whether a resource is an AWS-created default/singleton
// that should not be onboarded (analogous to the default VPC): the account's
// Resource Explorer infra, the default EventBridge bus, the "primary" Athena
// workgroup, the "Default" X-Ray rule, and MemoryDB/ElastiCache built-in defaults.
func awsManagedDefault(nativeType, name string) bool {
	switch nativeType {
	case "resource-explorer-2:view", "resource-explorer-2:index":
		return true
	case "events:event-bus":
		return name == "default"
	case "athena:workgroup":
		return name == "primary"
	case "xray:sampling-rule":
		return name == "Default"
	case "elasticache:user", "memorydb:user":
		return name == "default"
	case "memorydb:acl":
		return name == "open-access"
	case "memorydb:parametergroup":
		return strings.HasPrefix(name, "default.")
	case "apprunner:autoscalingconfiguration":
		return true // only AWS's DefaultConfiguration exists unless a user makes one
	}
	return false
}

func awsTypeToTFType(reType string) string {
	k := strings.ToLower(reType)
	if t, ok := awsTypeToTF[k]; ok {
		return t
	}
	return awsTypeToTFExtra[k] // full native-resource sweep (coverage.go)
}

// globalServices are AWS services whose resources are not region-scoped; their
// resources land in a single "global" stack rather than a per-region stack.
var globalServices = map[string]bool{
	"iam":               true,
	"cloudfront":        true,
	"route53":           true,
	"route53domains":    true,
	"waf":               true,
	"globalaccelerator": true,
	"organizations":     true,
}

// containerFor returns the layout container for a resource: its region, or
// "global" for a global service (or when the region is empty/aws-global).
func containerFor(r reResource) string {
	if globalServices[strings.ToLower(r.Service)] || r.Region == "" || r.Region == "aws-global" {
		return "global"
	}
	return r.Region
}

// arnName derives a human resource name from an ARN's last segment. ARNs are
// `arn:partition:service:region:account:resourceType/name` or
// `...:resourceType:name` or `...:name`; the trailing id after the last / or :
// is the resource's own name.
func arnName(arn string) string {
	s := arn
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	} else if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// tagsFromProperties extracts the tag map from a Resource Explorer property bag.
// The "tags" property is a JSON array of {Key, Value} objects.
func tagsFromProperties(props []reProperty) map[string]string {
	for _, p := range props {
		if !strings.EqualFold(p.Name, "tags") {
			continue
		}
		var kvs []struct {
			Key   string `json:"Key"`
			Value string `json:"Value"`
		}
		if err := json.Unmarshal(p.Data, &kvs); err != nil {
			return nil
		}
		out := make(map[string]string, len(kvs))
		for _, kv := range kvs {
			out[kv.Key] = kv.Value
		}
		return out
	}
	return nil
}
