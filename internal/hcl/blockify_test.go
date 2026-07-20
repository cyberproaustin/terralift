package hcl

import (
	"strings"
	"testing"
)

func TestBlockifySingle(t *testing.T) {
	src := `resource "aws_route_table" "r" {
  route = [{
    cidr_block = "10.0.0.0/16"
    gateway_id = "igw-1"
  }]
}`
	out, n := Blockify(src, []string{"route", "ingress", "egress"})
	if n != 1 {
		t.Errorf("converted %d lists, want 1", n)
	}
	if strings.Contains(out, "route = [{") || strings.Contains(out, "}]") {
		t.Errorf("attribute-list syntax not converted:\n%s", out)
	}
	if !strings.Contains(out, "route {") {
		t.Errorf("block syntax not produced:\n%s", out)
	}
}

func TestBlockifyMultipleObjects(t *testing.T) {
	src := `resource "aws_security_group" "s" {
  ingress = [{
    from_port = 80
  }, {
    from_port = 443
  }]
}`
	out, n := Blockify(src, []string{"ingress"})
	if n != 1 {
		t.Errorf("converted %d openers, want 1", n)
	}
	if strings.Count(out, "ingress {") != 2 {
		t.Errorf("two objects should yield two blocks:\n%s", out)
	}
}

func TestBlockifyLeavesObjectAttrsAlone(t *testing.T) {
	// A policy Statement list must NOT be blockified (it is an object attribute).
	src := `  Statement = [{
    Effect = "Allow"
  }]`
	out, n := Blockify(src, []string{"route", "ingress", "egress"})
	if n != 0 || out != src {
		t.Errorf("unlisted attribute should be untouched, got %d:\n%s", n, out)
	}
}
