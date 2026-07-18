package reconcile

import (
	"regexp"
	"sort"
	"strings"
)

// Rewire replaces literal cloud resource-ID strings in generated HCL with
// interpolated Terraform references, using an id->reference dictionary. The map
// VALUE is the complete replacement expression (e.g. "google_compute_network.vpc.id",
// "google_service_account.sa.email", "aws_vpc.main.id") — the caller chooses the
// attribute, because a literal id, self-link, service-account email, or static IP
// must resolve to a DIFFERENT attribute (.id / .self_link / .email / .address).
// Only QUOTED occurrences OUTSIDE a comment are rewired, so a `#`/`//` comment
// (including import.tf-style `# id = "..."`) is left intact. Matching is
// case-insensitive (cloud IDs are case-insensitive and tools vary).
//
// IDs are processed longest-first (then lexically) so the output is deterministic
// regardless of map iteration order, and a longer, more-specific id is rewired
// before any shorter id that shares its prefix. Returns the rewritten HCL and the
// number of references rewired.
func Rewire(hcl string, idToRef map[string]string) (string, int) {
	ids := make([]string, 0, len(idToRef))
	for id := range idToRef {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) > len(ids[j])
		}
		return ids[i] < ids[j]
	})
	pats := make([]*regexp.Regexp, len(ids))
	repls := make([]string, len(ids))
	for i, id := range ids {
		pats[i] = regexp.MustCompile(`(?i)"` + regexp.QuoteMeta(id) + `"`)
		repls[i] = idToRef[id]
	}

	count := 0
	lines := strings.Split(hcl, "\n")
	for li, line := range lines {
		code, comment := splitComment(line) // never rewire inside a comment
		for i, pat := range pats {
			code = pat.ReplaceAllStringFunc(code, func(string) string {
				count++
				return repls[i]
			})
		}
		lines[li] = code + comment
	}
	return strings.Join(lines, "\n"), count
}

// splitComment splits an HCL line into its code part and its trailing comment
// (from the first unquoted `#` or `//`), so replacements never touch comments.
func splitComment(line string) (code, comment string) {
	inStr, esc := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case esc:
			esc = false
		case c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '#':
			return line[:i], line[i:]
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return line[:i], line[i:]
		}
	}
	return line, ""
}
