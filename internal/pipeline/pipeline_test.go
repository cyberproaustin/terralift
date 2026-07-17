package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/core"
)

func TestStripBackendBlocks(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantRemov  int
		mustAbsent string
		mustHave   string
	}{
		{
			name: "single-line local backend (aztfexport)",
			in: `terraform {
  backend "local" {}

  required_providers {
    azurerm = { source = "hashicorp/azurerm" }
  }
}`,
			wantRemov:  1,
			mustAbsent: `backend "local"`,
			mustHave:   "required_providers",
		},
		{
			name: "multi-line backend block",
			in: `terraform {
  backend "azurerm" {
    resource_group_name = "rg"
    key                 = "x"
  }
  required_providers {}
}`,
			wantRemov:  1,
			mustAbsent: "resource_group_name",
			mustHave:   "required_providers",
		},
		{
			name:      "no backend block",
			in:        "terraform {\n  required_providers {}\n}",
			wantRemov: 0,
			mustHave:  "required_providers",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, n := stripBackendBlocks(c.in)
			if n != c.wantRemov {
				t.Errorf("removed = %d, want %d", n, c.wantRemov)
			}
			if c.mustAbsent != "" && contains(out, c.mustAbsent) {
				t.Errorf("output still contains %q:\n%s", c.mustAbsent, out)
			}
			if c.mustHave != "" && !contains(out, c.mustHave) {
				t.Errorf("output missing %q:\n%s", c.mustHave, out)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestMigrateStack(t *testing.T) {
	dst := t.TempDir()
	os.WriteFile(filepath.Join(dst, "generated.tf"), []byte(`resource "aws_subnet" "s" {
  availability_zone = "us-east-1a"
  cidr_block        = "10.0.1.0/24"
}
`), 0o644)
	os.WriteFile(filepath.Join(dst, "providers.tf"), []byte("provider \"aws\" {\n  region = \"us-east-1\"\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dst, "import.tf"), []byte("import { to = aws_subnet.s\n id = \"subnet-1\" }\n"), 0o644)

	run := &core.Run{Log: core.NewLogger(core.LevelError)}
	migrateStack(run, dst, []string{"generated.tf"},
		map[string]string{"region": "region", "availability_zone": "availability_zone"})

	gen, _ := os.ReadFile(filepath.Join(dst, "generated.tf"))
	prov, _ := os.ReadFile(filepath.Join(dst, "providers.tf"))
	if !strings.Contains(string(gen), "availability_zone = var.availability_zone") {
		t.Errorf("availability_zone not varized:\n%s", gen)
	}
	if !strings.Contains(string(prov), "region = var.region") {
		t.Errorf("region not varized:\n%s", prov)
	}
	if _, err := os.Stat(filepath.Join(dst, "import.tf")); !os.IsNotExist(err) {
		t.Error("import.tf should be dropped in migration mode")
	}
	vars, err := os.ReadFile(filepath.Join(dst, "variables.tf"))
	if err != nil || !strings.Contains(string(vars), `variable "region"`) || !strings.Contains(string(vars), `variable "name_prefix"`) {
		t.Errorf("variables.tf missing expected declarations: %v\n%s", err, vars)
	}
	if _, err := os.Stat(filepath.Join(dst, "terraform.tfvars.example")); err != nil {
		t.Error("terraform.tfvars.example not written")
	}
}

func TestOracleStackCopy(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "generated.tf"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(src, "backend.tf"), []byte("terraform { backend \"s3\" {} }"), 0o644)
	scratch, err := oracleStackCopy(src)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(scratch)
	if _, err := os.Stat(filepath.Join(scratch, "generated.tf")); err != nil {
		t.Error("generated.tf not copied")
	}
	if _, err := os.Stat(filepath.Join(scratch, "backend.tf")); !os.IsNotExist(err) {
		t.Error("backend.tf must NOT be copied (oracle plans with local state)")
	}
}
