package reconcile

import (
	"regexp"
	"strings"

	"github.com/cyberproaustin/terralift/internal/hcl"
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

var (
	attrLine      = regexp.MustCompile(`^(\s*)([A-Za-z0-9_]+)(\s*)=(\s*)"([^"]*)"(.*)$`)
	heredocOpenRe = regexp.MustCompile(`<<-?(\w+)`)
	// deepScopeAttrs are scope-pinning attributes distinctive enough to re-target at
	// ANY nesting depth (unlike bare project/region/location, which could collide with
	// a user's config key inside a nested block). subnetwork_project sits inside a
	// network_interface block but is unambiguously the source project.
	deepScopeAttrs = map[string]bool{"subnetwork_project": true}
)

// ToMigration rewrites HCL for a clone: re-targeted attributes become variables
// and (optionally) resource names get a prefix/suffix wrap. Only attributes at a
// block's top level (depth 1 — directly inside a resource/provider block) are
// touched; nested-block attributes (a `name` inside `ip_restriction {}`, an
// app_settings key, etc.) are left alone so a set name_prefix doesn't corrupt
// them. Heredoc bodies are skipped entirely. Idempotent: an already-wrapped name
// or an already-var-targeted attribute is not rewritten again.
func ToMigration(src string, rule MigrationRule) string {
	lines := strings.Split(src, "\n")
	depth := 0
	heredoc := ""
	for i, line := range lines {
		if heredoc != "" { // inside a heredoc body — never rewrite, never count braces
			if strings.TrimSpace(line) == heredoc {
				heredoc = ""
			}
			continue
		}
		if m := attrLine.FindStringSubmatch(line); m != nil {
			indent, attr, s1, s2, val, tail := m[1], m[2], m[3], m[4], m[5], m[6]
			v, isScope := rule.AttrToVar[attr]
			switch {
			case isScope && (depth == 1 || deepScopeAttrs[attr]):
				lines[i] = indent + attr + s1 + "=" + s2 + "var." + v + tail
			case depth == 1 && rule.WrapName && attr == "name" && !strings.Contains(val, "var.name_prefix"):
				lines[i] = indent + attr + s1 + "=" + s2 + `"${var.name_prefix}` + val + `${var.name_suffix}"` + tail
			}
		}
		if hm := heredocOpenRe.FindStringSubmatch(line); hm != nil {
			heredoc = hm[1]
			continue
		}
		depth += hcl.BraceDelta(line)
	}
	return strings.Join(lines, "\n")
}

// FirstAttr returns the first literal value of a resource-top-level `attr = "value"`
// in the HCL, used to seed a migration variable's default (e.g. the source
// location). Depth-restricted like ToMigration so a same-named attribute inside a
// nested block (app_settings, tags, …) can't seed the wrong default.
func FirstAttr(src, attr string) string {
	depth := 0
	heredoc := ""
	for _, line := range strings.Split(src, "\n") {
		if heredoc != "" {
			if strings.TrimSpace(line) == heredoc {
				heredoc = ""
			}
			continue
		}
		if depth == 1 {
			if m := attrLine.FindStringSubmatch(line); m != nil && m[2] == attr {
				return m[5]
			}
		}
		if hm := heredocOpenRe.FindStringSubmatch(line); hm != nil {
			heredoc = hm[1]
			continue
		}
		depth += hcl.BraceDelta(line)
	}
	return ""
}
