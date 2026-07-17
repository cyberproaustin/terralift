package gcp

import "testing"

func TestDeriveImportID(t *testing.T) {
	cases := []struct{ name, cai, tfType, want string }{
		// default path form
		{"bucket", "//storage.googleapis.com/my-bucket", "google_storage_bucket", "my-bucket"},
		{"network", "//compute.googleapis.com/projects/p/global/networks/n", "google_compute_network", "projects/p/global/networks/n"},
		{"sa", "//iam.googleapis.com/projects/p/serviceAccounts/e@p.iam.gserviceaccount.com", "google_service_account", "projects/p/serviceAccounts/e@p.iam.gserviceaccount.com"},
		{"topic", "//pubsub.googleapis.com/projects/p/topics/t", "google_pubsub_topic", "projects/p/topics/t"},
		{"secret", "//secretmanager.googleapis.com/projects/123/secrets/s", "google_secret_manager_secret", "projects/123/secrets/s"},
		// per-type overrides (generic path form would be WRONG here)
		{"project", "//cloudresourcemanager.googleapis.com/projects/my-proj", "google_project", "my-proj"},
		{"sql", "//sqladmin.googleapis.com/projects/p/instances/db1", "google_sql_database_instance", "p/db1"},
		{"dns", "//dns.googleapis.com/projects/p/managedZones/z1", "google_dns_managed_zone", "p/z1"},
	}
	for _, c := range cases {
		if got := deriveImportID(c.cai, c.tfType, "p"); got != c.want {
			t.Errorf("%s: deriveImportID(%q) = %q, want %q", c.name, c.cai, got, c.want)
		}
	}
}

func TestEscapeHCLTemplate(t *testing.T) {
	if got := escapeHCLTemplate("a${b}c%{d}"); got != "a$${b}c%%{d}" {
		t.Errorf("escapeHCLTemplate = %q", got)
	}
}
