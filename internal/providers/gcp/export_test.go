package gcp

import (
	"testing"

	"github.com/cyberproaustin/terralift/internal/model"
)

func TestNotImportable(t *testing.T) {
	sa := func(email string) *model.Resource {
		return &model.Resource{TFType: "google_service_account", Properties: map[string]any{"email": email}}
	}
	// Google-managed SAs are not user import targets.
	managed := []string{
		"695024406364-compute@developer.gserviceaccount.com",
		"my-proj@appspot.gserviceaccount.com",
		"service-695024406364@gcp-sa-pubsub.iam.gserviceaccount.com",
		"695024406364@cloudservices.gserviceaccount.com",
	}
	for _, e := range managed {
		if !notImportable(sa(e)) {
			t.Errorf("%s should be not-importable (Google-managed)", e)
		}
	}
	// A user-created SA IS importable.
	if notImportable(sa("terralift-app@terralift-lab-24252.iam.gserviceaccount.com")) {
		t.Errorf("user SA should be importable")
	}
	// Non-SA resources are unaffected.
	if notImportable(&model.Resource{TFType: "google_storage_bucket"}) {
		t.Errorf("bucket should be importable")
	}
}
