package gcp

import "testing"

func TestDeriveImportID(t *testing.T) {
	cases := []struct{ name, cai, tfType, want string }{
		{"bucket", "//storage.googleapis.com/my-bucket", "google_storage_bucket", "my-bucket"},
		{"network", "//compute.googleapis.com/projects/p/global/networks/n", "google_compute_network", "projects/p/global/networks/n"},
		{"sa", "//iam.googleapis.com/projects/p/serviceAccounts/e@p.iam.gserviceaccount.com", "google_service_account", "projects/p/serviceAccounts/e@p.iam.gserviceaccount.com"},
		{"topic", "//pubsub.googleapis.com/projects/p/topics/t", "google_pubsub_topic", "projects/p/topics/t"},
		{"secret", "//secretmanager.googleapis.com/projects/123/secrets/s", "google_secret_manager_secret", "projects/123/secrets/s"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.cai, c.tfType, "p"); got != c.want {
			t.Errorf("%s: deriveImportID(%q) = %q, want %q", c.name, c.cai, got, c.want)
		}
	}
}
