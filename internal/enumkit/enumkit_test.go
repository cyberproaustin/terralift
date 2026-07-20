package enumkit

import (
	"errors"
	"testing"
)

func TestPaginateAccumulatesPages(t *testing.T) {
	pages := map[string]struct {
		items []int
		next  string
	}{
		"":   {[]int{1, 2}, "t1"},
		"t1": {[]int{3, 4}, "t2"},
		"t2": {[]int{5}, ""},
	}
	got, err := Paginate(func(token string) ([]int, string, error) {
		p := pages[token]
		return p.items, p.next, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 3, 4, 5}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestPaginateSinglePage(t *testing.T) {
	got, err := Paginate(func(string) ([]string, string, error) {
		return []string{"a"}, "", nil
	})
	if err != nil || len(got) != 1 || got[0] != "a" {
		t.Errorf("single page: got %v err %v", got, err)
	}
}

func TestPaginatePropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := Paginate(func(string) ([]int, string, error) {
		return nil, "", sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("want sentinel error, got %v", err)
	}
}

func TestPaginateStallsOnRepeatedToken(t *testing.T) {
	calls := 0
	_, err := Paginate(func(string) ([]int, string, error) {
		calls++
		if calls > 100 {
			t.Fatal("Paginate did not stop on a repeated token (would loop forever)")
		}
		return []int{calls}, "stuck", nil // same non-empty token every page
	})
	if err == nil {
		t.Fatal("want an error when the cursor never advances, got nil")
	}
}

func TestTypeMap(t *testing.T) {
	core := TypeMap{"ec2:instance": "aws_instance"}
	full := TypeMap{"ec2:vpc": "aws_vpc", "ec2:instance": "aws_instance_OVERRIDE"}
	merged := core.Merge(full)
	if merged.TF("ec2:vpc") != "aws_vpc" {
		t.Errorf("merge missing key: %v", merged)
	}
	if merged.TF("ec2:instance") != "aws_instance_OVERRIDE" {
		t.Errorf("later map should win on collision: %v", merged)
	}
	if merged.TF("ec2:unknown") != "" {
		t.Errorf("unmapped type should be empty string")
	}
	if !merged.Has("ec2:vpc") || merged.Has("nope") {
		t.Errorf("Has wrong: %v", merged)
	}
	// Merge must not mutate the receiver.
	if core.Has("ec2:vpc") {
		t.Errorf("Merge mutated the receiver")
	}
}
