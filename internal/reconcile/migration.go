package reconcile

import (
	"regexp"
	"strings"
)

// MigrationRule declares how to re-target generated HCL into a portable clone
// (migration mode). Each provider supplies its own rule: Azure re-targets
// resource_group_name + location; GCP re-targets project + location; etc.
type MigrationRule struct {
	// AttrToVar maps a top-level attribute to a variable name, e.g.
	// {"project":"project","location":"location"}. Matched lines become
	// `attr = var.<mapped>`.
	AttrToVar map[string]string
	// WrapName wraps `name = "x"` as "${var.name_prefix}x${var.name_suffix}" so
	// globally-unique names can be made unique in the target.
	WrapName bool
}

var attrLine = regexp.MustCompile(`^(\s*)([A-Za-z0-9_]+)(\s*)=(\s*)"([^"]*)"(.*)$`)

// ToMigration rewrites HCL for a clone: re-targeted attributes become variables
// and (optionally) resource names get a prefix/suffix wrap. Line-based and
// deterministic; the transform is a no-op on default (empty) prefix/suffix, so
// over-matching a nested block's `name` is harmless until a prefix is set.
func ToMigration(hcl string, rule MigrationRule) string {
	lines := strings.Split(hcl, "\n")
	for i, line := range lines {
		m := attrLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent, attr, s1, s2, val, tail := m[1], m[2], m[3], m[4], m[5], m[6]
		if v, ok := rule.AttrToVar[attr]; ok {
			lines[i] = indent + attr + s1 + "=" + s2 + "var." + v + tail
			continue
		}
		if rule.WrapName && attr == "name" {
			lines[i] = indent + attr + s1 + "=" + s2 + `"${var.name_prefix}` + val + `${var.name_suffix}"` + tail
		}
	}
	return strings.Join(lines, "\n")
}

// FirstAttr returns the first literal value of `attr = "value"` in the HCL,
// used to seed a migration variable's default (e.g. the source location).
func FirstAttr(hcl, attr string) string {
	for _, line := range strings.Split(hcl, "\n") {
		m := attrLine.FindStringSubmatch(line)
		if m != nil && m[2] == attr {
			return m[5]
		}
	}
	return ""
}
