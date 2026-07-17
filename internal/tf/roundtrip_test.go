package tf

import "testing"

func TestParseRoundTrip(t *testing.T) {
	plan := []byte(`{
      "format_version": "1.0",
      "resource_changes": [
        { "address": "google_a.x", "mode": "managed", "change": { "actions": ["no-op"] } },
        { "address": "google_b.y", "mode": "managed", "change": { "actions": ["update"] } },
        { "address": "data.google_c.z", "mode": "data", "change": { "actions": ["read"] } }
      ]
    }`)
	rt, err := ParseRoundTrip(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(rt.Clean) != 1 || rt.Clean[0] != "google_a.x" {
		t.Errorf("Clean = %v, want [google_a.x]", rt.Clean)
	}
	if len(rt.Drift) != 1 || rt.Drift[0].Address != "google_b.y" {
		t.Errorf("Drift = %v, want [google_b.y]", rt.Drift)
	}
	for _, c := range rt.Clean {
		if c == "data.google_c.z" {
			t.Errorf("data source should be ignored")
		}
	}
}
