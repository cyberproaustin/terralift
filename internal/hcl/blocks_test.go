package hcl

import (
	"strings"
	"testing"
)

func TestDropNestedBlock(t *testing.T) {
	src := `resource "aws_vpc_endpoint" "e" {
  vpc_endpoint_type = "Gateway"
  dns_options {
    dns_record_ip_type = "ipv4"
  }
  policy = "x"
}`
	out := strings.Join(DropNestedBlock(strings.Split(src, "\n"), "dns_options"), "\n")
	if strings.Contains(out, "dns_options") || strings.Contains(out, "dns_record_ip_type") {
		t.Errorf("dns_options block not fully removed:\n%s", out)
	}
	if !strings.Contains(out, `policy = "x"`) || !strings.Contains(out, `vpc_endpoint_type = "Gateway"`) {
		t.Errorf("DropNestedBlock removed a sibling it should have kept:\n%s", out)
	}
}

func TestDropBlocksByNameNested(t *testing.T) {
	// A block that itself contains a nested block must be removed whole.
	src := `outer {
  read_pool_auto_scale_config {
    enabled = false
    inner {
      x = 0
    }
  }
  keep = 1
}`
	out := strings.Join(DropBlocksByName(strings.Split(src, "\n"), map[string]bool{"read_pool_auto_scale_config": true}), "\n")
	if strings.Contains(out, "read_pool_auto_scale_config") || strings.Contains(out, "inner {") {
		t.Errorf("nested block not fully removed:\n%s", out)
	}
	if !strings.Contains(out, "keep = 1") {
		t.Errorf("DropBlocksByName removed the wrong content:\n%s", out)
	}
}

func TestDropBlocksByNameNoMatch(t *testing.T) {
	src := []string{"resource \"x\" \"y\" {", "  a = 1", "}"}
	out := DropBlocksByName(src, map[string]bool{"nope": true})
	if strings.Join(out, "\n") != strings.Join(src, "\n") {
		t.Errorf("DropBlocksByName with no match should be identity, got %v", out)
	}
}
