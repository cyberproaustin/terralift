package reconcile

import "testing"

func TestResolvePrecedence(t *testing.T) {
	cases := []struct {
		name string
		in   PrecedenceInput
		want string
	}{
		{"not-writable", PrecedenceInput{InPlay: false, HasProviderSchema: true}, "drop"},
		{"no-schema", PrecedenceInput{InPlay: true, HasProviderSchema: false}, "route-to-raw"},
		{"version-skew", PrecedenceInput{InPlay: true, HasProviderSchema: true, ExportValue: "a", TruthValue: "b", ExportAPIVersion: "2024-01-01", TruthAPIVersion: "2023-01-01"}, "skip-version-skew"},
		{"differ", PrecedenceInput{InPlay: true, HasProviderSchema: true, ExportValue: "a", TruthValue: "b", ExportAPIVersion: "2024-01-01", TruthAPIVersion: "2024-01-01"}, "correct-to-truth"},
		{"agree", PrecedenceInput{InPlay: true, HasProviderSchema: true, ExportValue: "a", TruthValue: "a"}, "agree"},
	}
	for _, c := range cases {
		if got := ResolvePrecedence(c.in).Decision; got != c.want {
			t.Errorf("%s: ResolvePrecedence = %q, want %q", c.name, got, c.want)
		}
	}
}

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
