package pipeline

import "testing"

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
