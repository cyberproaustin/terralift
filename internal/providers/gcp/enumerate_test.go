package gcp

import (
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func TestPrincipalType(t *testing.T) {
	cases := map[string]string{
		"allUsers":              "Public",
		"allAuthenticatedUsers": "Public",
		"user:a@b.com":          "User",
		"serviceAccount:x@y.iam.gserviceaccount.com": "ServiceAccount",
		"group:g@b.com":                              "Group",
		"domain:b.com":                               "Domain",
		"weird":                                      "Unknown",
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
