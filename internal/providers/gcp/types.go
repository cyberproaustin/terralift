package gcp

import (
	"strings"

	"github.com/cyberproaustin/terralift/internal/model"
)

// assetTypeToTF maps a Cloud Asset Inventory assetType to a Terraform google_*
// type. Best-effort and incremental; "" means unmapped -> coverage gap (honest,
// degrades gracefully). Seeded/common types first; extend as we cover more.
var assetTypeToTF = map[string]string{
	"storage.googleapis.com/Bucket":       "google_storage_bucket",
	"pubsub.googleapis.com/Topic":         "google_pubsub_topic",
	"pubsub.googleapis.com/Subscription":  "google_pubsub_subscription",
	"bigquery.googleapis.com/Dataset":     "google_bigquery_dataset",
	"bigquery.googleapis.com/Table":       "google_bigquery_table",
	"secretmanager.googleapis.com/Secret": "google_secret_manager_secret",
	"iam.googleapis.com/ServiceAccount":   "google_service_account",
	"compute.googleapis.com/Network":      "google_compute_network",
	"compute.googleapis.com/Subnetwork":   "google_compute_subnetwork",
	"compute.googleapis.com/Firewall":     "google_compute_firewall",
	"compute.googleapis.com/Instance":     "google_compute_instance",
	// Route intentionally unmapped: GCP auto-creates default/peering routes that
	// are not typical import targets (they regenerate). Add back behind a config
	// toggle if a user manages custom routes.
	"compute.googleapis.com/Address":              "google_compute_address",
	"compute.googleapis.com/Disk":                 "google_compute_disk",
	"sqladmin.googleapis.com/Instance":            "google_sql_database_instance",
	"container.googleapis.com/Cluster":            "google_container_cluster",
	"run.googleapis.com/Service":                  "google_cloud_run_v2_service",
	"cloudfunctions.googleapis.com/Function":      "google_cloudfunctions_function",
	"dns.googleapis.com/ManagedZone":              "google_dns_managed_zone",
	"cloudresourcemanager.googleapis.com/Project": "google_project",
}

func tfTypeFor(assetType string) string {
	if t, ok := assetTypeToTF[assetType]; ok {
		return t
	}
	return assetTypeToTFExtra[assetType] // full native-resource sweep (coverage.go)
}

// gcpScope renders a model.Scope as a CAI --scope value.
func gcpScope(s model.Scope) string {
	switch s.Type {
	case model.ScopeFolder:
		return "folders/" + s.ID
	case model.ScopeOrganization:
		return "organizations/" + s.ID
	default: // project
		return "projects/" + s.ID
	}
}

// projectID strips the "projects/" prefix ("projects/695024406364" -> number/id).
func projectID(project string) string {
	return strings.TrimPrefix(project, "projects/")
}
