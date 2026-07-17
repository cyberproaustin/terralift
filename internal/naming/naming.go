// Package naming provides born-correct Terraform resource-address naming:
// sanitize a cloud resource name into a valid TF label, and de-collide
// (type, name) addresses deterministically. Provider-agnostic — the same logic
// serves every cloud's export phase.
package naming

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	nonLabel   = regexp.MustCompile(`[^a-z0-9_]`)
	multiUnder = regexp.MustCompile(`_+`)
	startsOK   = regexp.MustCompile(`^[a-z_]`)
)

// Sanitize turns any name into a valid, conventional Terraform resource label:
// lower-case, only [a-z0-9_], collapsed underscores, never a leading digit.
func Sanitize(name string) string {
	s := strings.ToLower(name)
	s = nonLabel.ReplaceAllString(s, "_")
	s = multiUnder.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "resource"
	}
	if !startsOK.MatchString(s) {
		s = "r_" + s
	}
	return s
}

// Address is one resource's identity for de-collision: its TF type plus the
// sanitized base name we'd like to use.
type Address struct {
	Type string // e.g. google_storage_bucket
	Base string // sanitized base name
}

// Dedupe assigns a unique final name per (Type, name), suffixing _2, _3, ... on
// collision. It tracks FINAL taken addresses (not a per-base count) so a natural
// "foo_2" cannot collide with a generated suffix and emit a duplicate. The
// result is deterministic given the input order (callers sort by resource id).
func Dedupe(addrs []Address) []string {
	used := map[string]bool{}
	out := make([]string, len(addrs))
	for i, a := range addrs {
		cand := a.Base
		n := 1
		for used[a.Type+"."+cand] {
			n++
			cand = a.Base + "_" + strconv.Itoa(n)
		}
		used[a.Type+"."+cand] = true
		out[i] = cand
	}
	return out
}
