package hcl

import "regexp"

// resourceBlockHeaderRe matches a `resource "type" "name" {` opener (trailing
// brace required — the walker needs the block to start on this line).
var resourceBlockHeaderRe = regexp.MustCompile(`^resource\s+"([^"]+)"\s+"([^"]+)"\s*\{`)

// BlockEditor transforms one `resource` block (block[0] is the header line,
// block[len-1] is its closing brace) into a replacement block, plus any redaction
// events produced. Return the block unchanged to leave it as-is.
type BlockEditor func(tfType string, block []string) ([]string, []Redaction)

// WalkResourceBlocks walks each top-level `resource "type" "name" { ... }` block
// in lines (brace-balanced via the string/comment-aware BraceDelta) and replaces
// it with edit(type, block). Lines outside any resource block pass through
// unchanged. Returns the rewritten lines and the concatenated redaction events.
//
// This is the shared engine behind per-type generated-HCL curation: a provider
// supplies the per-type edits; the block-walking mechanism lives here once.
func WalkResourceBlocks(lines []string, edit BlockEditor) ([]string, []Redaction) {
	var out []string
	var events []Redaction
	for i := 0; i < len(lines); i++ {
		m := resourceBlockHeaderRe.FindStringSubmatch(lines[i])
		if m == nil {
			out = append(out, lines[i])
			continue
		}
		depth, end := BraceDelta(lines[i]), i
		for j := i + 1; j < len(lines); j++ {
			depth += BraceDelta(lines[j])
			if depth <= 0 {
				end = j
				break
			}
		}
		block, evs := edit(m[1], lines[i:end+1])
		events = append(events, evs...)
		out = append(out, block...)
		i = end
	}
	return out, events
}
