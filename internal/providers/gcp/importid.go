package gcp

import "strings"

// importIDOverride handles the few types whose Terraform import ID is NOT simply
// the CAI asset-name path. Most google_* types accept the path after
// "//{service}.googleapis.com/" (and buckets accept the bare name), so the
// default covers them; add entries here only for genuine exceptions.
var importIDOverride = map[string]func(caiName, projectID string) string{}

// deriveImportID turns a Cloud Asset Inventory asset name into a Terraform
// import ID. Default: the path after the "//{service}.googleapis.com/" prefix.
//   //storage.googleapis.com/my-bucket                       -> my-bucket
//   //compute.googleapis.com/projects/p/global/networks/n    -> projects/p/global/networks/n
//   //iam.googleapis.com/projects/p/serviceAccounts/email    -> projects/p/serviceAccounts/email
func deriveImportID(caiName, tfType, projectID string) string {
	if fn, ok := importIDOverride[tfType]; ok {
		return fn(caiName, projectID)
	}
	s := strings.TrimPrefix(caiName, "//")
	if i := strings.Index(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
