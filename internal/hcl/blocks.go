package hcl

import "strings"

// DropBlocksByName removes every nested block whose opening label is a key in
// names, including any braces nested inside it. A block opener is a line that,
// trimmed, is `<label> {`; the block ends at its brace-balanced close.
//
// Brace matching uses raw `{`/`}` counting (not the string-aware BraceDelta) to
// exactly reproduce the hand-written walkers this replaces (GCP's
// dropOverEmitBlocks, AWS's dropNestedBlock). Generated-config output places one
// block opener per line, so the raw count and the string-aware count agree; the
// distinction only matters for a lone brace inside a string literal, which these
// callers never encounter on a block-opener line.
func DropBlocksByName(lines []string, names map[string]bool) []string {
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if name := strings.TrimSpace(strings.TrimSuffix(t, "{")); strings.HasSuffix(t, "{") && names[name] {
			depth, j := 1, i+1
			for ; j < len(lines) && depth > 0; j++ {
				depth += strings.Count(lines[j], "{") - strings.Count(lines[j], "}")
			}
			i = j - 1 // outer loop's i++ lands past the closing brace
			continue
		}
		out = append(out, lines[i])
	}
	return out
}

// DropNestedBlock is the single-label convenience form of DropBlocksByName.
func DropNestedBlock(lines []string, name string) []string {
	return DropBlocksByName(lines, map[string]bool{name: true})
}
