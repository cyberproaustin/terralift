package hcl

import (
	"regexp"
	"strings"
)

// Prune drops every line of hclText that matches ANY of rules, returning the
// pruned text and the number of lines removed. It is the shared line-level
// filter behind each provider's pruneGeneratedHCL: `terraform plan
// -generate-config-out` over-emits attributes with unset/default/invalid values
// (null, [], {}, *_UNSPECIFIED enums, zeroed halves of mutually-exclusive
// pairs), which are noise at best and break the plan at worst. The provider
// supplies the rule set; the mechanism is identical across clouds.
//
// Matching is per-line and order-independent (logical OR), exactly as the
// hand-written loops were, so a rule set moved here produces byte-identical
// output.
func Prune(hclText string, rules []*regexp.Regexp) (string, int) {
	lines := strings.Split(hclText, "\n")
	out := make([]string, 0, len(lines))
	n := 0
	for _, l := range lines {
		drop := false
		for _, re := range rules {
			if re.MatchString(l) {
				drop = true
				break
			}
		}
		if drop {
			n++
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n"), n
}
