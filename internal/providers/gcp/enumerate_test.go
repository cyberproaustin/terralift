package gcp

import (
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func TestIsRegionLocation(t *testing.T) {
	cases := map[string]bool{
		"us-central1":   true,
		"europe-west1":  true,
		"global":        false,
		"GLOBAL":        false,
		"us-central1-a": false, // zone
		"":              false,
	}
	for loc, want := range cases {
		if got := isRegionLocation(loc); got != want {
			t.Errorf("isRegionLocation(%q) = %v, want %v", loc, got, want)
		}
	}
}

func TestCAIRegionalClassification(t *testing.T) {
	// CAI reports one asset type for global/regional/zonal variants; caiToResource
	// must resolve the concrete Terraform type from the location.
	cases := []struct {
		assetType, location, want string
	}{
		{"compute.googleapis.com/BackendService", "us-central1", "google_compute_region_backend_service"},
		{"compute.googleapis.com/BackendService", "global", "google_compute_backend_service"},
		{"compute.googleapis.com/HealthCheck", "us-central1", "google_compute_region_health_check"},
		{"compute.googleapis.com/HealthCheck", "global", "google_compute_health_check"},
		{"compute.googleapis.com/UrlMap", "us-central1", "google_compute_region_url_map"},
		{"compute.googleapis.com/TargetHttpProxy", "us-central1", "google_compute_region_target_http_proxy"},
		{"compute.googleapis.com/Disk", "us-central1", "google_compute_region_disk"},
		{"compute.googleapis.com/Disk", "us-central1-a", "google_compute_disk"},
		{"compute.googleapis.com/Address", "us-central1", "google_compute_address"},
		{"compute.googleapis.com/Address", "global", "google_compute_global_address"},
	}
	for _, c := range cases {
		r := caiResource{
			AssetType: c.assetType,
			Location:  c.location,
			Name:      "//compute.googleapis.com/projects/p/regions/us-central1/x/n",
		}
		if got := caiToResource(r).TFType; got != c.want {
			t.Errorf("%s @ %s -> %q, want %q", c.assetType, c.location, got, c.want)
		}
	}
}

func TestPrincipalType(t *testing.T) {
	cases := map[string]string{
		"allUsers":              "Public",
		"allAuthenticatedUsers": "Public",
		"user:a@b.com":          "User",
		"serviceAccount:x@y.iam.gserviceaccount.com": "ServiceAccount",
		"group:g@b.com": "Group",
		"domain:b.com":  "Domain",
		"weird":         "Unknown",
	}
	for in, want := range cases {
		if got := principalType(in); got != want {
			t.Errorf("principalType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsPrivilegedRole(t *testing.T) {
	for _, r := range []string{"roles/owner", "roles/editor", "roles/iam.securityAdmin", "roles/storage.admin"} {
		if !isPrivilegedRole(r) {
			t.Errorf("%s should be privileged", r)
		}
	}
	for _, r := range []string{"roles/viewer", "roles/storage.objectViewer"} {
		if isPrivilegedRole(r) {
			t.Errorf("%s should NOT be privileged", r)
		}
	}
}

func TestTfTypeAndScope(t *testing.T) {
	if tfTypeFor("storage.googleapis.com/Bucket") != "google_storage_bucket" {
		t.Error("bucket type map")
	}
	if tfTypeFor("unknown.googleapis.com/Thing") != "" {
		t.Error("unknown type should map to empty (coverage gap)")
	}
	if gcpScope(model.Scope{Type: model.ScopeOrganization, ID: "123"}) != "organizations/123" {
		t.Error("org scope")
	}
	if gcpScope(model.Scope{Type: model.ScopeProject, ID: "p"}) != "projects/p" {
		t.Error("project scope")
	}
}

func TestCaiToResource(t *testing.T) {
	r := caiResource{
		Name:               "//storage.googleapis.com/my-bucket",
		AssetType:          "storage.googleapis.com/Bucket",
		Project:            "projects/12345",
		Location:           "us",
		DisplayName:        "my-bucket",
		Labels:             map[string]string{"env": "prod"},
		VersionedResources: []caiVersionedResource{{Version: "v1", Resource: map[string]any{"name": "my-bucket"}}},
	}
	res := caiToResource(r)
	if res.ID != r.Name {
		t.Errorf("ID = %q", res.ID)
	}
	if res.TFType != "google_storage_bucket" {
		t.Errorf("TFType = %q", res.TFType)
	}
	if res.Container != "12345" {
		t.Errorf("Container = %q, want 12345", res.Container)
	}
	if res.Tags["env"] != "prod" {
		t.Errorf("Tags not mapped from labels")
	}
	if res.Properties["name"] != "my-bucket" {
		t.Errorf("Properties not taken from versionedResources[0].resource")
	}
	if res.Source != "cai" {
		t.Errorf("Source = %q", res.Source)
	}
}
