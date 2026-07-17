package util

import "testing"

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   []string
		want int
	}{
		{[]string{"rg1", "rg2"}, 2},        // array stays two
		{[]string{"rg1,rg2"}, 2},           // comma-joined string splits
		{[]string{"rg1, rg2 ", "rg3"}, 3},  // mixed + trims
		{[]string{"rg1", "", "  "}, 1},     // drops empties
	}
	for _, c := range cases {
		if got := len(SplitCSV(c.in)); got != c.want {
			t.Errorf("SplitCSV(%v) len = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPathSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"rg-app-eus2-dev", "rg-app-eus2-dev"}, // real name is identity
		{`a/b\c:d`, "a_b_c_d"},                 // separators neutralized
		{"..", "_"},                            // traversal collapsed
	}
	for _, c := range cases {
		if got := PathSegment(c.in); got != c.want {
			t.Errorf("PathSegment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
