// Package hcl provides shared, structure-aware redaction of secret material from
// generated Terraform HCL. It is used by every provider's export so the
// control-plane-only guarantee is enforced identically across clouds (the three
// providers previously each had a different, partly-broken scrub).
//
// IMPORTANT (see docs/DESIGN-DECISIONS.md, ADR-001): this redactor scrubs ONLY
// unambiguous single secret values (top-level password/secret_string/*_access_key/
// etc. and scoped rules). It intentionally does NOT blank application config maps
// (app_settings, env vars) — those SHIP and are flagged in reports/secrets-review.md.
// Do not "fix" a plaintext secret in a config map by blanking the map here.
package hcl

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	resourceHeaderRe = regexp.MustCompile(`^resource\s+"([^"]+)"\s+"[^"]+"\s*\{`)
	heredocOpenRe    = regexp.MustCompile(`<<-?(\w+)`)
	attrRe           = regexp.MustCompile(`^(\s*)([A-Za-z0-9_]+)(\s*)=\s*(.*)$`)
	lifecycleOpenRe  = regexp.MustCompile(`^\s*lifecycle\s*\{`)
	ignoreChangesRe  = regexp.MustCompile(`ignore_changes\s*=\s*\[`)
)

const scrubMark = ` # TerraLift: scrubbed data-plane value`

// Rule redacts a data-plane value inside a specific resource type.
type Rule struct {
	Type           string // resource type, e.g. "aws_lambda_function"
	Attr           string // the attribute to redact
	Kind           Kind
	OnlyIfContains string // optional: only apply when the block contains this substring
}

// Redaction records one scrubbed secret so the operator can be TOLD what was
// removed from their repo and must be supplied out-of-band (a value that silently
// vanished would blindside anyone cutting over to IaC).
type Redaction struct {
	Resource string // resource type, e.g. "aws_db_instance" ("" if outside a resource block)
	Attr     string // the attribute that was scrubbed
	Action   string // "removed" (attribute deleted; value now unmanaged) | "blanked" (set to "" + ignore_changes)
}

// Kind is how Attr carries its secret.
type Kind int

const (
	// Scalar: `attr = "secret"` (or a heredoc). Blanked to "" and, because the
	// attribute is required, protected with lifecycle.ignore_changes so a later
	// apply does not overwrite the real value with the blank.
	Scalar Kind = iota
	// MapBlock: `attr = { KEY = "secret" … }`. The whole block is REMOVED (these
	// attributes are optional — dropping them leaves the live values unmanaged).
	MapBlock
	// JSONEnv: a JSON string/heredoc (e.g. ECS container_definitions) whose
	// `environment[].value` entries are secret. Values are blanked in-place and the
	// attribute is protected with ignore_changes.
	JSONEnv
)

// Redact removes secret material from HCL text. secretAttrs is the exact list of
// attribute names whose value is always secret anywhere (removed, heredoc-aware).
// rules target specific resource types. Returns the redacted HCL and one Redaction
// per scrubbed secret (for the operator-facing redactions report).
func Redact(src string, secretAttrs []string, rules []Rule) (string, []Redaction) {
	lines := strings.Split(src, "\n")
	events := removeSecretAttrs(lines, secretAttrs)

	byType := map[string][]Rule{}
	for _, r := range rules {
		byType[r.Type] = append(byType[r.Type], r)
	}
	// Process blocks; some rules mutate/remove lines and inject a lifecycle block.
	for _, b := range blockRanges(lines) {
		ignore := map[string]bool{}
		for _, r := range byType[b.typ] {
			if r.OnlyIfContains != "" && !rangeContains(lines, b, r.OnlyIfContains) {
				continue
			}
			switch r.Kind {
			case Scalar:
				if blankScalar(lines, b, r.Attr) > 0 {
					events = append(events, Redaction{b.typ, r.Attr, "blanked"})
					ignore[r.Attr] = true
				}
			case MapBlock:
				if blankMapValues(lines, b, r.Attr) > 0 {
					events = append(events, Redaction{b.typ, r.Attr, "removed"})
				}
			case JSONEnv:
				if blankJSONEnv(lines, b, r.Attr) > 0 {
					events = append(events, Redaction{b.typ, r.Attr, "blanked"})
					ignore[r.Attr] = true
				}
			}
		}
		if len(ignore) > 0 {
			injectIgnoreChanges(lines, b, ignore)
		}
	}
	return strings.Join(lines, "\n"), events
}

type block struct {
	typ        string
	start, end int
}

func blockRanges(lines []string) []block {
	var out []block
	inBlock, depth := false, 0
	cur := block{}
	for i, l := range lines {
		if !inBlock {
			if m := resourceHeaderRe.FindStringSubmatch(l); m != nil {
				cur = block{typ: m[1], start: i}
				depth = braceDelta(l)
				if depth <= 0 {
					cur.end = i
					out = append(out, cur)
				} else {
					inBlock = true
				}
			}
			continue
		}
		depth += braceDelta(l)
		if depth <= 0 {
			cur.end = i
			out = append(out, cur)
			inBlock = false
		}
	}
	return out
}

// BraceDelta counts { minus } outside of double-quoted strings and # comments, so
// a brace inside a string value ("a{b") doesn't corrupt depth tracking. Exported
// for reuse by other packages that walk HCL block structure line-by-line.
func BraceDelta(l string) int { return braceDelta(l) }

// braceDelta counts { minus } outside of double-quoted strings and # comments, so
// a brace inside a string value ("a{b") doesn't corrupt depth tracking.
func braceDelta(l string) int {
	depth, inStr, esc := 0, false, false
	for i := 0; i < len(l); i++ {
		c := l[i]
		switch {
		case esc:
			esc = false
		case c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '#':
			return depth // # comment — rest of line is not structure
		case c == '/' && i+1 < len(l) && l[i+1] == '/':
			return depth // // comment (valid HCL) — rest of line is not structure
		case c == '{':
			depth++
		case c == '}':
			depth--
		}
	}
	return depth
}

func rangeContains(lines []string, b block, needle string) bool {
	for i := b.start; i <= b.end && i < len(lines); i++ {
		if strings.Contains(lines[i], needle) {
			return true
		}
	}
	return false
}

// removeSecretAttrs deletes a `<name> = …` line whose name is in the set, but ONLY
// at a resource block's TOP LEVEL (depth 1). This is critical: the exact-name
// secrets (password, auth_token, client_secret, connection_string, …) are also
// common app-config KEY names, and app config is shipped intact — matching by bare
// name anywhere would silently delete an env var literally named CLIENT_SECRET.
// Real single-secret attributes (aws_db_instance.password, secret_string, …) live
// at depth 1, so restricting there scrubs them while leaving nested config maps
// alone. Heredoc bodies are consumed; the enclosing resource type is tracked (and
// reset on block exit) so each removal is reported accurately.
func removeSecretAttrs(lines []string, names []string) []Redaction {
	if len(names) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, s := range names {
		set[s] = true
	}
	var events []Redaction
	curType := ""
	depth := 0
	heredoc := ""
	for i := 0; i < len(lines); i++ {
		if heredoc != "" { // inside a heredoc body — never a top-level attr
			if strings.TrimSpace(lines[i]) == heredoc {
				heredoc = ""
			}
			continue
		}
		if hm := resourceHeaderRe.FindStringSubmatch(lines[i]); hm != nil {
			curType = hm[1]
		}
		if depth == 1 {
			m := attrRe.FindStringSubmatch(lines[i])
			if m != nil && set[strings.ToLower(m[2])] {
				val := m[4]
				lines[i] = "" // remove the attribute line
				events = append(events, Redaction{Resource: curType, Attr: m[2], Action: "removed"})
				if hm := heredocOpenRe.FindStringSubmatch(val); hm != nil {
					consumeHeredoc(lines, i, hm[1])
				}
				continue
			}
		}
		if hm := heredocOpenRe.FindStringSubmatch(lines[i]); hm != nil {
			heredoc = hm[1] // opener line has no unbalanced braces to count
			continue
		}
		depth += braceDelta(lines[i])
		if depth <= 0 {
			depth, curType = 0, "" // exited all blocks
		}
	}
	return events
}

// blankScalar blanks EVERY `attr = …` in a block to "" (heredoc-aware). Blanking
// all occurrences (not just the first) matters when the same attribute name
// repeats in sibling nested blocks — a first-match-only scrub would leak the rest.
func blankScalar(lines []string, b block, attr string) int {
	re := regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(attr) + `\s*=\s*(.*)$`)
	n := 0
	for i := b.start; i <= b.end && i < len(lines); i++ {
		m := re.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		lines[i] = m[1] + attr + ` = ""` + scrubMark
		if hm := heredocOpenRe.FindStringSubmatch(m[2]); hm != nil {
			consumeHeredoc(lines, i, hm[1])
		}
		n++
	}
	return n
}

// blankMapValues removes EVERY `attr = { … }` block (optional attributes: dropping
// them leaves the live values unmanaged rather than overwritten). All occurrences
// are removed so a repeated map in sibling nested blocks can't leak.
func blankMapValues(lines []string, b block, attr string) int {
	open := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(attr) + `\s*=\s*\{`)
	n := 0
	for i := b.start; i <= b.end && i < len(lines); i++ {
		if !open.MatchString(lines[i]) {
			continue
		}
		depth := braceDelta(lines[i])
		lines[i] = "" // drop the opener
		for j := i + 1; j <= b.end && j < len(lines); j++ {
			depth += braceDelta(lines[j])
			lines[j] = ""
			if depth <= 0 {
				break
			}
		}
		n++
	}
	return n
}

// blankJSONEnv finds EVERY `attr = "<json>"` / heredoc, parses it, blanks every
// environment[].value, and drops any `secrets` list. Best-effort: if it can't
// parse, it falls back to blanking the scalar so nothing leaks. All occurrences in
// the block are processed so a repeated attr in sibling nested blocks can't leak.
func blankJSONEnv(lines []string, b block, attr string) int {
	re := regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(attr) + `\s*=\s*(.*)$`)
	n := 0
	for i := b.start; i <= b.end && i < len(lines); i++ {
		m := re.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		// Collect the value (single-line JSON string or heredoc body).
		var raw string
		endLine := i
		if hm := heredocOpenRe.FindStringSubmatch(m[2]); hm != nil {
			var body []string
			for j := i + 1; j <= b.end && j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == hm[1] {
					endLine = j
					break
				}
				body = append(body, lines[j])
			}
			raw = strings.Join(body, "\n")
		} else {
			raw = strings.TrimSpace(m[2])
			if s, err := strconv.Unquote(raw); err == nil {
				raw = s
			}
		}
		redacted, ok := blankJSONContainerEnv(raw)
		for j := i; j <= endLine; j++ {
			lines[j] = ""
		}
		if !ok { // couldn't parse -> blank the whole attribute so nothing leaks
			lines[i] = m[1] + attr + ` = jsonencode([])` + scrubMark
		} else {
			lines[i] = m[1] + attr + " = " + strconv.Quote(redacted) + scrubMark
		}
		n++
		i = endLine // resume after the processed value
	}
	return n
}

// blankJSONContainerEnv blanks environment[].value and removes secrets[] in an
// ECS container-definitions JSON array. Returns (json, ok).
func blankJSONContainerEnv(raw string) (string, bool) {
	var defs []map[string]any
	if err := json.Unmarshal([]byte(raw), &defs); err != nil {
		return "", false
	}
	for _, d := range defs {
		if env, ok := d["environment"].([]any); ok {
			for _, e := range env {
				if kv, ok := e.(map[string]any); ok {
					if _, has := kv["value"]; has {
						kv["value"] = ""
					}
				}
			}
		}
		delete(d, "secrets")
	}
	out, err := json.Marshal(defs)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// injectIgnoreChanges makes sure every attr in attrs is covered by a
// `lifecycle { ignore_changes = […] }` guard, so a later apply won't overwrite the
// blanked (real) value with "". It MERGES into an existing lifecycle block rather
// than bailing — a pre-existing lifecycle (e.g. prevent_destroy) must not leave a
// blanked REQUIRED secret unprotected, which would let the next apply push the
// empty string over the live credential.
func injectIgnoreChanges(lines []string, b block, attrs map[string]bool) {
	names := make([]string, 0, len(attrs))
	for a := range attrs {
		names = append(names, a)
	}
	sort.Strings(names)

	lcOpen, lcEnd := -1, -1
	depth := 0
	for i := b.start; i <= b.end && i < len(lines); i++ {
		if lcOpen == -1 {
			if lifecycleOpenRe.MatchString(lines[i]) {
				lcOpen = i
				depth = braceDelta(lines[i])
				if depth <= 0 {
					lcEnd = i
				}
			}
			continue
		}
		if lcEnd == -1 {
			depth += braceDelta(lines[i])
			if depth <= 0 {
				lcEnd = i
			}
		}
	}

	if lcOpen == -1 { // no lifecycle block — insert a fresh one before the closing brace
		lc := "  lifecycle {\n    ignore_changes = [" + strings.Join(names, ", ") + "]\n  }"
		lines[b.end] = lc + "\n" + lines[b.end]
		return
	}

	// Only add names not already mentioned anywhere in the existing lifecycle block.
	existing := strings.Join(lines[lcOpen:min(lcEnd+1, len(lines))], "\n")
	var missing []string
	for _, n := range names {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(n) + `\b`).MatchString(existing) {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return
	}
	// Find an ignore_changes line to merge into; else add one after `lifecycle {`.
	for i := lcOpen; i <= lcEnd && i < len(lines); i++ {
		loc := ignoreChangesRe.FindStringIndex(lines[i])
		if loc == nil {
			continue
		}
		if br := strings.Index(lines[i][loc[1]:], "]"); br >= 0 { // single-line list: `= [a, b]`
			at := loc[1] + br
			sep := ", "
			if strings.TrimSpace(lines[i][loc[1]:at]) == "" {
				sep = ""
			}
			lines[i] = lines[i][:at] + sep + strings.Join(missing, ", ") + lines[i][at:]
		} else { // multi-line list: insert entries right after the `[`
			lines[i] = lines[i] + "\n      " + strings.Join(missing, ",\n      ") + ","
		}
		return
	}
	lines[lcOpen] = lines[lcOpen] + "\n    ignore_changes = [" + strings.Join(missing, ", ") + "]"
}

func consumeHeredoc(lines []string, openIdx int, tag string) {
	for j := openIdx + 1; j < len(lines); j++ {
		done := strings.TrimSpace(lines[j]) == tag
		lines[j] = ""
		if done {
			return
		}
	}
}
