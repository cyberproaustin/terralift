package gcp

import (
	"strings"
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func TestIsUserMember(t *testing.T) {
	for _, m := range []string{
		"serviceAccount:x@developer.gserviceaccount.com",
		"serviceAccount:service-1@gcp-sa-pubsub.iam.gserviceaccount.com",
		"projectOwner:p", "projectEditor:p", "projectViewer:p",
	} {
		if isUserMember(m) {
			t.Errorf("%s should NOT be a user member", m)
		}
	}
	for _, m := range []string{"allUsers", "user:a@b.com", "serviceAccount:app@p.iam.gserviceaccount.com", "group:g@b.com"} {
		if !isUserMember(m) {
			t.Errorf("%s should be a user member", m)
		}
	}
}

func TestIamResourceFor(t *testing.T) {
	if tf, attr, _, _ := iamResourceFor("//cloudresourcemanager.googleapis.com/projects/p"); tf != "google_project_iam_member" || attr != "project" {
		t.Errorf("project: %s/%s", tf, attr)
	}
	if tf, attr, _, imp := iamResourceFor("//storage.googleapis.com/mybucket"); tf != "google_storage_bucket_iam_member" || attr != "bucket" || imp != "b/mybucket" {
		t.Errorf("bucket: %s/%s/%s", tf, attr, imp)
	}
}

func TestGenerateIAM(t *testing.T) {
	inv := &model.Inventory{IAM: []model.IAMBinding{
		{Scope: "//storage.googleapis.com/pub", Role: "roles/storage.objectViewer", PrincipalID: "allUsers"},
		{Scope: "//storage.googleapis.com/pub", Role: "roles/storage.legacyBucketOwner", PrincipalID: "projectOwner:p"}, // filtered (legacy + projectOwner)
		{Scope: "//cloudresourcemanager.googleapis.com/projects/p", Role: "roles/viewer", PrincipalID: "serviceAccount:app@p.iam.gserviceaccount.com"},
	}}
	hcl, imp, n := generateIAM(inv, map[string]string{"//storage.googleapis.com/pub": "google_storage_bucket.pub"})
	if n != 2 {
		t.Fatalf("n=%d, want 2 (default ACL filtered out)", n)
	}
	if !strings.Contains(hcl, "google_storage_bucket.pub.name") {
		t.Error("bucket owner should be a reference, not a literal")
	}
	if !strings.Contains(hcl, `member = "allUsers"`) {
		t.Error("missing allUsers binding")
	}
	if !strings.Contains(imp, "roles/viewer") {
		t.Error("missing import block")
	}
}
