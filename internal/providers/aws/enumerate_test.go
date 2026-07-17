package aws

import (
	"encoding/json"
	"testing"
)

func TestAwsTypeToTFType(t *testing.T) {
	cases := map[string]string{
		"s3:bucket":          "aws_s3_bucket",
		"EC2:Instance":       "aws_instance", // case-insensitive
		"ec2:security-group": "aws_security_group",
		"iam:role":           "aws_iam_role",
		"unknown:thing":      "",
	}
	for in, want := range cases {
		if got := awsTypeToTFType(in); got != want {
			t.Errorf("awsTypeToTFType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContainerFor(t *testing.T) {
	cases := []struct {
		svc, region, want string
	}{
		{"iam", "aws-global", "global"},       // global service
		{"cloudfront", "us-east-1", "global"}, // global service ignores region
		{"ec2", "us-west-2", "us-west-2"},     // regional
		{"s3", "eu-west-1", "eu-west-1"},      // s3 is regional
		{"lambda", "", "global"},              // empty region -> global bucket
	}
	for _, c := range cases {
		got := containerFor(reResource{Service: c.svc, Region: c.region})
		if got != c.want {
			t.Errorf("containerFor(%s,%s) = %q, want %q", c.svc, c.region, got, c.want)
		}
	}
}

func TestArnName(t *testing.T) {
	cases := map[string]string{
		"arn:aws:s3:::my-logs-bucket":                         "my-logs-bucket",
		"arn:aws:ec2:us-east-1:123456789012:vpc/vpc-0abc123":  "vpc-0abc123",
		"arn:aws:iam::123456789012:role/my-app-role":          "my-app-role",
		"arn:aws:dynamodb:us-east-1:123456789012:table/Users": "Users",
		"arn:aws:sns:us-east-1:123456789012:my-topic":         "my-topic",
	}
	for in, want := range cases {
		if got := arnName(in); got != want {
			t.Errorf("arnName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTagsFromProperties(t *testing.T) {
	data, _ := json.Marshal([]map[string]string{{"Key": "env", "Value": "dev"}, {"Key": "team", "Value": "core"}})
	props := []reProperty{{Name: "tags", Data: data}}
	tags := tagsFromProperties(props)
	if tags["env"] != "dev" || tags["team"] != "core" {
		t.Errorf("tagsFromProperties = %v", tags)
	}
	if tagsFromProperties(nil) != nil {
		t.Error("no properties should yield nil tags")
	}
}

func TestReToResource(t *testing.T) {
	tags, _ := json.Marshal([]map[string]string{{"Key": "env", "Value": "prod"}})
	r := reResource{
		ARN:          "arn:aws:ec2:us-west-2:123456789012:security-group/sg-0abc",
		ResourceType: "ec2:security-group",
		Service:      "ec2",
		Region:       "us-west-2",
		Properties:   []reProperty{{Name: "tags", Data: tags}},
	}
	res := reToResource(r)
	if res.TFType != "aws_security_group" {
		t.Errorf("TFType = %q", res.TFType)
	}
	if res.Name != "sg-0abc" || res.Container != "us-west-2" || res.Location != "us-west-2" {
		t.Errorf("bad mapping: %+v", res)
	}
	if res.Tags["env"] != "prod" {
		t.Errorf("tags = %v", res.Tags)
	}
	if res.Source != "resource-explorer" {
		t.Errorf("source = %q", res.Source)
	}
}
