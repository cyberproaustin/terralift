package hcl

import (
	"strings"
	"testing"
)

func TestWalkResourceBlocksEditsByType(t *testing.T) {
	src := `provider "aws" {
  region = "us-east-1"
}

resource "aws_kms_key" "k" {
  policy = "x"
}

resource "aws_vpc" "v" {
  cidr_block = "10.0.0.0/16"
}`
	out, events := WalkResourceBlocks(strings.Split(src, "\n"), func(typ string, block []string) ([]string, []Redaction) {
		if typ != "aws_kms_key" {
			return block, nil
		}
		// inject a line before the closing brace
		nb := append([]string{}, block[:len(block)-1]...)
		nb = append(nb, "  injected = true")
		nb = append(nb, block[len(block)-1])
		return nb, []Redaction{{Resource: typ, Attr: "x", Action: "test"}}
	})
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "injected = true") {
		t.Errorf("edit not applied to aws_kms_key:\n%s", joined)
	}
	if strings.Count(joined, "injected = true") != 1 {
		t.Errorf("edit applied to the wrong number of blocks:\n%s", joined)
	}
	if !strings.Contains(joined, `provider "aws"`) || !strings.Contains(joined, `cidr_block = "10.0.0.0/16"`) {
		t.Errorf("non-target lines/blocks were altered:\n%s", joined)
	}
	if len(events) != 1 {
		t.Errorf("got %d events, want 1", len(events))
	}
}

func TestWalkResourceBlocksNestedBraces(t *testing.T) {
	// A block containing nested braces must be delimited correctly.
	src := `resource "aws_ecs_service" "s" {
  load_balancer {
    container_name = "app"
  }
}
resource "aws_vpc" "v" {}`
	seen := map[string]bool{}
	_, _ = WalkResourceBlocks(strings.Split(src, "\n"), func(typ string, block []string) ([]string, []Redaction) {
		seen[typ] = true
		return block, nil
	})
	if !seen["aws_ecs_service"] || !seen["aws_vpc"] {
		t.Errorf("walker did not visit both blocks: %v", seen)
	}
}
