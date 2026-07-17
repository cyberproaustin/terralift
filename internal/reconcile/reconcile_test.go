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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
