package reconcile

import "testing"

func TestToMigration(t *testing.T) {
	hcl := `resource "google_storage_bucket" "data" {
  name     = "appdata01"
  project  = "src-project"
  location = "US"
}`
	rule := MigrationRule{
		AttrToVar: map[string]string{"project": "project", "location": "location"},
		WrapName:  true,
	}
	out := ToMigration(hcl, rule)
	checks := []string{
		"project  = var.project",
		"location = var.location",
		`name     = "${var.name_prefix}appdata01${var.name_suffix}"`,
	}
	for _, want := range checks {
		if !contains(out, want) {
			t.Errorf("ToMigration missing %q\n---\n%s", want, out)
		}
	}
	if contains(out, `"src-project"`) {
		t.Errorf("ToMigration left source project literal in output")
	}
	if got := FirstAttr(hcl, "location"); got != "US" {
		t.Errorf("FirstAttr(location) = %q, want US", got)
	}
}

// TestToMigrationNestedAndIdempotent guards two regressions: (1) a `name` inside a
// NESTED block must not be wrapped (only the resource-top-level name), and (2)
// running ToMigration twice must not double-wrap.
func TestToMigrationNestedAndIdempotent(t *testing.T) {
	src := `resource "azurerm_linux_web_app" "app" {
  name                = "myapp01"
  resource_group_name = "src-rg"
  site_config {
    application_stack {
      name = "NODE"
    }
  }
  app_settings = {
    name = "should-not-wrap"
  }
}`
	rule := MigrationRule{
		AttrToVar: map[string]string{"resource_group_name": "resource_group_name"},
		WrapName:  true,
	}
	out := ToMigration(src, rule)
	if !contains(out, `name                = "${var.name_prefix}myapp01${var.name_suffix}"`) {
		t.Errorf("top-level name not wrapped:\n%s", out)
	}
	if !contains(out, `resource_group_name = var.resource_group_name`) {
		t.Errorf("rg not re-targeted:\n%s", out)
	}
	// Nested-block names must be untouched.
	if !contains(out, `name = "NODE"`) || !contains(out, `name = "should-not-wrap"`) {
		t.Errorf("nested-block name was wrongly wrapped:\n%s", out)
	}
	// Idempotent: a second pass must not double-wrap.
	if again := ToMigration(out, rule); again != out {
		t.Errorf("ToMigration not idempotent:\nfirst:\n%s\nsecond:\n%s", out, again)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestRewireSkipsComments(t *testing.T) {
	in := `resource "x" "y" {
  bucket = "my-id"
  # was: bucket = "my-id"
}`
	out, n := Rewire(in, map[string]string{"my-id": "google_x.y.id"})
	if n != 1 {
		t.Fatalf("expected exactly 1 rewire (code only), got %d\n%s", n, out)
	}
	if !contains(out, `bucket = google_x.y.id`) {
		t.Errorf("code occurrence not rewired:\n%s", out)
	}
	if !contains(out, `# was: bucket = "my-id"`) {
		t.Errorf("comment occurrence must be left intact:\n%s", out)
	}
}
