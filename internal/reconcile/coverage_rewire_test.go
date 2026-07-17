package reconcile

import "testing"

func TestCoverage(t *testing.T) {
	// Exporter is a superset: 8 enumerated, all exported, +25 child/implicit.
	enum := make([]string, 8)
	exp := make([]string, 0, 33)
	for i := 0; i < 8; i++ {
		id := string(rune('a' + i))
		enum[i] = id
		exp = append(exp, id)
	}
	for i := 0; i < 25; i++ {
		exp = append(exp, "child-"+string(rune('a'+i)))
	}
	r := Coverage(enum, exp, nil)
	if r.Covered != 8 || r.CoveragePct != 100 {
		t.Errorf("covered=%d pct=%v, want 8 / 100", r.Covered, r.CoveragePct)
	}
	if r.ExtraExported != 25 {
		t.Errorf("extraExported=%d, want 25", r.ExtraExported)
	}
	if len(r.Missing) != 0 {
		t.Errorf("missing=%d, want 0", len(r.Missing))
	}

	// Gap case: one enumerated resource not exported.
	r2 := Coverage([]string{"x", "y", "z"}, []string{"x", "y"},
		map[string]MissingResource{"z": {ID: "z", Type: "google_thing"}})
	if r2.Covered != 2 || len(r2.Missing) != 1 || r2.Missing[0].Type != "google_thing" {
		t.Errorf("gap case wrong: covered=%d missing=%v", r2.Covered, r2.Missing)
	}
	if r2.CoveragePct <= 66 || r2.CoveragePct >= 67 {
		t.Errorf("pct=%v, want ~66.7", r2.CoveragePct)
	}
}

func TestRewire(t *testing.T) {
	hcl := `resource "google_x" "a" {
  linked   = "//compute.googleapis.com/projects/p/zones/z/instances/i"
  # azure_id = //compute.googleapis.com/projects/p/zones/z/instances/i
}`
	dict := map[string]string{
		"//compute.googleapis.com/projects/p/zones/z/instances/i": "google_compute_instance.web",
	}
	out, n := Rewire(hcl, dict)
	if n != 1 {
		t.Errorf("rewired count = %d, want 1 (comment must not match)", n)
	}
	if !contains(out, "linked   = google_compute_instance.web.id") {
		t.Errorf("quoted literal not rewired:\n%s", out)
	}
	if !contains(out, `# azure_id = //compute`) {
		t.Errorf("comment should be left intact")
	}
}
