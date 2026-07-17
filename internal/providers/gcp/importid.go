package gcp

import "strings"

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
}

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

// projectSlashName turns ".../projects/P/.../NAME" into "P/NAME".
func projectSlashName(caiName, _ string) string {
	parts := strings.Split(stripService(caiName), "/")
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1] + "/" + parts[len(parts)-1]
	}
	return lastSegment(caiName)
}

// escapeHCLTemplate neutralizes Terraform template markers so a value written
// into a double-quoted HCL string is treated literally.
func escapeHCLTemplate(s string) string {
	s = strings.ReplaceAll(s, "${", "$${")
	s = strings.ReplaceAll(s, "%{", "%%{")
	return s
}
