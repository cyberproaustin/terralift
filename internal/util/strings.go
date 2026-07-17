// Package util holds small, generic string/slice helpers shared across phases.
package util

import (
	"regexp"
	"strings"
)

// SplitCSV normalizes a list that may contain comma-joined elements into a clean
// slice: accepts ["a","b"], ["a,b"], ["a, b ","c"]. Trims whitespace, drops empties.
// Lets CLI inputs accept both `--rg a,b` and `--rg a --rg b` interchangeably.
func SplitCSV(values []string) []string {
	out := make([]string, 0, len(values))
	for _, item := range values {
		for _, piece := range strings.Split(item, ",") {
			if p := strings.TrimSpace(piece); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// EscapeHCLTemplate neutralizes Terraform template markers so an untrusted value
// (e.g. an Azure custom-role display name) written into a double-quoted HCL string
// is treated as a literal, not evaluated as `${...}` interpolation or `%{...}`
// directive. Go's %q does NOT escape these. Apply before %q — %q leaves the added
// `$`/`%` untouched.
func EscapeHCLTemplate(s string) string {
	s = strings.ReplaceAll(s, "${", "$${")
	s = strings.ReplaceAll(s, "%{", "%%{")
	return s
}

var (
	pathSep  = regexp.MustCompile(`[\\/:*?"<>|]`)
	traverse = regexp.MustCompile(`\.\.+`)
)

// PathSegment neutralizes path-separator and traversal characters so a name is
// safe as a single filesystem segment. Identity for normal resource/scope names
// (defense-in-depth).
func PathSegment(name string) string {
	s := pathSep.ReplaceAllString(name, "_")
	s = traverse.ReplaceAllString(s, "_")
	if s = strings.TrimSpace(s); s == "" {
		return "segment"
	}
	return s
}
