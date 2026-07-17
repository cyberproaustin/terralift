package naming

import "testing"

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"rg-App.01", "rg_app_01"}, // lower-case + non-alnum -> _
		{"123abc", "r_123abc"},     // cannot start with a digit
		{"ASP-x--y", "asp_x_y"},    // collapse repeated underscores
		{"stbvaadmindev", "stbvaadmindev"},
		{"---", "resource"}, // empty after trim -> placeholder
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDedupe(t *testing.T) {
	// The regression case: a real name "foo_2" must not collide with the suffix
	// generated for the two "foo" clashes.
	addrs := []Address{
		{Type: "google_x", Base: "foo"},
		{Type: "google_x", Base: "foo"},
		{Type: "google_x", Base: "foo_2"},
		{Type: "google_y", Base: "foo"}, // different type -> its own namespace
	}
	got := Dedupe(addrs)
	want := []string{"foo", "foo_2", "foo_2_2", "foo"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Dedupe[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	// Uniqueness within a type.
	seen := map[string]bool{}
	for i, a := range addrs {
		key := a.Type + "." + got[i]
		if seen[key] {
			t.Errorf("duplicate final address %q", key)
		}
		seen[key] = true
	}
}
