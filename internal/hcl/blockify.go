package hcl

import (
	"regexp"
	"strings"
)

// Blockify rewrites `terraform plan -generate-config-out`'s attribute-list syntax
// for block-typed attributes (`attr = [{ ... }]`) into repeated block syntax
// (`attr { ... }`) for each attribute named in attrs. The attribute-list form
// fails `terraform plan` with "Incorrect attribute value type" for a block type,
// because its object literal omits optional keys, whereas a block accepts the
// omissions. Scoped to the named attributes so an object-typed attribute (e.g. an
// IAM policy `Statement = [{...}]`) is never touched. Returns the rewritten text
// and the number of attribute lists converted.
func Blockify(hclText string, attrs []string) (string, int) {
	if len(attrs) == 0 {
		return hclText, 0
	}
	quoted := make([]string, len(attrs))
	for i, a := range attrs {
		quoted[i] = regexp.QuoteMeta(a)
	}
	openRe := regexp.MustCompile(`^(\s*)(` + strings.Join(quoted, "|") + `)\s*=\s*\[\{\s*$`)

	lines := strings.Split(hclText, "\n")
	out := make([]string, 0, len(lines))
	n := 0
	for i := 0; i < len(lines); i++ {
		m := openRe.FindStringSubmatch(lines[i])
		if m == nil {
			out = append(out, lines[i])
			continue
		}
		indent, attr := m[1], m[2]
		out = append(out, indent+attr+" {")
		n++
		for i++; i < len(lines); i++ {
			switch strings.TrimSpace(lines[i]) {
			case "}]": // end of the list
				out = append(out, indent+"}")
			case "}, {", "},{": // next object in the same list -> a new block
				out = append(out, indent+"}")
				out = append(out, indent+attr+" {")
				continue
			default:
				out = append(out, lines[i])
				continue
			}
			break // only the "}]" case falls through to here
		}
	}
	return strings.Join(out, "\n"), n
}
