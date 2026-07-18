package gcp

import (
	"regexp"
	"strings"

	"github.com/cyberproaustin/terralift/internal/util"
)

// importIDOverride handles types whose Terraform import ID is NOT the plain CAI
// asset-name path. Most google_* types accept the path after
// "//{service}.googleapis.com/" (buckets accept the bare name), but a few differ
// and the generic form would produce import blocks that fail at apply.
var importIDOverride = map[string]func(caiName, projectID string) string{
	// google_project imports by the bare project id, not "projects/<number>".
	"google_project": func(cai, _ string) string { return lastSegment(cai) },
	// These import as "{{project}}/{{name}}", not "projects/{{project}}/.../{{name}}".
	"google_sql_database_instance": projectSlashName,
	"google_dns_managed_zone":      projectSlashName,
	// google_logging_metric imports by the bare metric name, not the full
	// "projects/{{project}}/metrics/{{name}}" path (which fails as non-existent).
	"google_logging_metric": func(cai, _ string) string { return lastSegment(cai) },
	// A pubsub schema's CAI name carries a "@revision" suffix that its canonical id
	// omits; strip it so the imported .id matches how other resources reference the
	// schema (e.g. a topic's schema_settings.schema, which has no revision).
	"google_pubsub_schema": func(cai, _ string) string {
		id := stripService(cai)
		if at := strings.LastIndex(id, "@"); at > 0 {
			id = id[:at]
		}
		return id
	},
}

// projectNumberPrefix matches a leading "projects/<number>/" segment. CAI encodes
// the owning project by NUMBER, but some resource imports (e.g. Cloud Tasks) reject
// the number form with a 403 and accept only the project ID.
var projectNumberPrefix = regexp.MustCompile(`^projects/[0-9]+/`)

// deriveImportID turns a Cloud Asset Inventory asset name into a Terraform
// import ID. Default: the path after the "//{service}.googleapis.com/" prefix.
// Per-type overrides handle the exceptions above. The result is HCL-escaped so a
// literal "${" / "%{" in an id can never be interpreted as template syntax when
// written into a .tf file (defense-in-depth; GCP ids don't contain these).
func deriveImportID(caiName, tfType, projectID string) string {
	var id string
	if fn, ok := importIDOverride[tfType]; ok {
		id = fn(caiName, projectID)
	} else {
		id = stripService(caiName)
	}
	// Normalize a leading "projects/<number>/" to the scope's project ID. In a
	// single-project onboarding the numeric project in an asset's own name is always
	// this project, and the ID form is accepted where the number form is rejected.
	if projectID != "" {
		id = projectNumberPrefix.ReplaceAllString(id, "projects/"+projectID+"/")
	}
	return escapeHCLTemplate(id)
}

// stripService returns the path after "//{service}.googleapis.com/".
func stripService(caiName string) string {
	s := strings.TrimPrefix(caiName, "//")
	if i := strings.Index(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func lastSegment(s string) string { return s[strings.LastIndex(s, "/")+1:] }

// projectSlashName turns ".../projects/P/.../NAME" into "P/NAME". Because it strips
// the "projects/" prefix, the caller's projectNumberPrefix normalization can't reach
// it, so it normalizes the project here: in a single-project scope the owning project
// IS the scope project, so the ID form is used (CAI often supplies the number, which
// some imports reconcile as a spurious replacement against the provider-default id).
func projectSlashName(caiName, projectID string) string {
	parts := strings.Split(stripService(caiName), "/")
	if len(parts) >= 2 && parts[0] == "projects" {
		proj := parts[1]
		if projectID != "" {
			proj = projectID
		}
		return proj + "/" + parts[len(parts)-1]
	}
	return lastSegment(caiName)
}

// escapeHCLTemplate neutralizes Terraform template markers so a value written
// into a double-quoted HCL string is treated literally.
func escapeHCLTemplate(s string) string { return util.EscapeHCLTemplate(s) }
