package hcl

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestScanAddrs(t *testing.T) {
	src := `# header comment
resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
}

resource "aws_subnet" "app" {
  vpc_id = aws_vpc.main.id
}
`
	got := ScanAddrs(src)
	for _, want := range []string{"aws_vpc.main", "aws_subnet.app"} {
		if !got[want] {
			t.Errorf("ScanAddrs missing %q; got %v", want, got)
		}
	}
	if len(got) != 2 {
		t.Errorf("ScanAddrs got %d addrs, want 2: %v", len(got), got)
	}
}

func TestScanAddrsFileMissing(t *testing.T) {
	if got := ScanAddrsFile(filepath.Join(t.TempDir(), "nope.tf")); len(got) != 0 {
		t.Errorf("missing file should yield empty set, got %v", got)
	}
}

func TestScanAddrsDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.tf"), []byte(`resource "x" "one" {}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.tf"), []byte(`resource "y" "two" {}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte(`resource "z" "three" {}`), 0o644)
	got := ScanAddrsDir(dir)
	if !got["x.one"] || !got["y.two"] {
		t.Errorf("ScanAddrsDir should union .tf files, got %v", got)
	}
	if got["z.three"] {
		t.Errorf("ScanAddrsDir should ignore non-.tf files, got %v", got)
	}
}

func TestImportBlock(t *testing.T) {
	got := ImportBlock("aws_vpc.main", "vpc-123")
	want := "import {\n  to = aws_vpc.main\n  id = \"vpc-123\"\n}\n\n"
	if got != want {
		t.Errorf("ImportBlock = %q, want %q", got, want)
	}
}

func TestPrune(t *testing.T) {
	rules := []*regexp.Regexp{
		regexp.MustCompile(`^\s*[A-Za-z0-9_]+\s*=\s*(null|\[\]|\{\})\s*$`),
		regexp.MustCompile(`^\s*#\s*__generated__ by Terraform`),
	}
	src := `resource "x" "y" {
  keep    = "value"
  empty   = null
  list    = []
  # __generated__ by Terraform
  another = 0
}`
	out, n := Prune(src, rules)
	if n != 3 {
		t.Errorf("Prune removed %d lines, want 3", n)
	}
	if want := "= null"; contains(out, want) {
		t.Errorf("pruned output still contains %q:\n%s", want, out)
	}
	if !contains(out, `keep    = "value"`) || !contains(out, "another = 0") {
		t.Errorf("Prune dropped a line it should have kept:\n%s", out)
	}
}

func TestPruneNoRules(t *testing.T) {
	src := "a\nb\nc"
	if out, n := Prune(src, nil); n != 0 || out != src {
		t.Errorf("Prune with no rules should be identity, got %q (%d)", out, n)
	}
}

func TestTail(t *testing.T) {
	if got := Tail("a\nb\nc\nd\n", 2); got != "c\nd" {
		t.Errorf("Tail = %q, want %q", got, "c\nd")
	}
	if got := Tail("only", 5); got != "only" {
		t.Errorf("Tail of short input = %q, want %q", got, "only")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}())
}
