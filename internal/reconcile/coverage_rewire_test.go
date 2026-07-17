package reconcile

import "testing"

func TestCoverage(t *testing.T) {
	// All enumerated resources exported, nothing excluded.
	enum := []string{"a", "b", "c", "d"}
	r := Coverage(enum, enum, nil, nil)
	if r.Covered != 4 || r.CoveragePct != 100 || r.Gap != 0 {
		t.Errorf("covered=%d pct=%v gap=%d, want 4/100/0", r.Covered, r.CoveragePct, r.Gap)
	}

	// enumerated {x,y,z}: x exported, y intentionally excluded, z is a real gap.
	// considered = 3 - 1(excluded) = 2; covered = 2 - 1(gap) = 1; pct = 50.
	r2 := Coverage(
		[]string{"x", "y", "z"},
		[]string{"x"},
		[]string{"y"},
		map[string]MissingResource{"z": {ID: "z", Type: "google_thing"}},
	)
	if r2.Excluded != 1 || r2.Considered != 2 || r2.Covered != 1 || r2.Gap != 1 {
		t.Errorf("excluded=%d considered=%d covered=%d gap=%d, want 1/2/1/1", r2.Excluded, r2.Considered, r2.Covered, r2.Gap)
	}
	if r2.CoveragePct != 50 {
		t.Errorf("pct=%v, want 50", r2.CoveragePct)
	}
	if len(r2.Missing) != 1 || r2.Missing[0].Type != "google_thing" {
		t.Errorf("missing wrong: %v", r2.Missing)
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
