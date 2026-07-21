package launchdarkly

import (
	"github.com/cyberproaustin/terralift/internal/model"
	"github.com/cyberproaustin/terralift/internal/util"
)

// deriveImportID returns the HCL-template-escaped Terraform import ID. LaunchDarkly composites
// use `/` at a DEPTH that tracks the fan-out scope: account-wide → bare; project-scoped →
// 2-part <project>/<key>; env-scoped → 3-part <project>/<env>/<leaf>. The 3-part ORDER is
// env-in-the-MIDDLE, and feature_flag_environment is the trap: its import id is
// <project>/<env>/<flag> (NOT its flag_id attribute <project>/<flag> with env appended). The
// depth + order are chosen by an explicit per-TF-type switch (never inferred); enumerate.go
// stores the parts in import order.
func deriveImportID(r *model.Resource) string {
	return util.EscapeHCLTemplate(rawImportID(r))
}

func rawImportID(r *model.Resource) string {
	p := func(k string) string { s, _ := r.Properties[k].(string); return s }
	switch r.TFType {
	case "launchdarkly_segment",
		"launchdarkly_destination",
		"launchdarkly_feature_flag_environment":
		// 3-part <project>/<env>/<leaf> — env in the MIDDLE.
		return p("a") + "/" + p("b") + "/" + p("c")
	case "launchdarkly_environment",
		"launchdarkly_feature_flag",
		"launchdarkly_metric":
		// 2-part <project_key>/<key>.
		return p("left") + "/" + p("right")
	case "launchdarkly_project",
		"launchdarkly_webhook",
		"launchdarkly_team",
		"launchdarkly_custom_role":
		// bare: project/team/custom_role by key, webhook by _id.
		return p("token")
	default:
		// A composite TFType added to enumerate.go but forgotten here must surface as a gap
		// (empty id → dropped), never silently emit a bare (un-importable) id.
		return ""
	}
}
