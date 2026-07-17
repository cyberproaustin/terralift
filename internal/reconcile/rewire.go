package reconcile

import "regexp"

// Rewire replaces literal cloud resource-ID strings in generated HCL with
// interpolated <tfAddress>.id references, using an id->address dictionary.
// Only QUOTED occurrences are rewired, so "# id = ..." comments are left intact.
// Matching is case-insensitive (cloud IDs are case-insensitive and tools vary).
// Returns the rewritten HCL and the number of references rewired.
func Rewire(hcl string, idToAddress map[string]string) (string, int) {
	count := 0
	for id, addr := range idToAddress {
		pat := regexp.MustCompile(`(?i)"` + regexp.QuoteMeta(id) + `"`)
		repl := addr + ".id"
		hcl = pat.ReplaceAllStringFunc(hcl, func(string) string {
			count++
			return repl
		})
	}
	return hcl, count
}
