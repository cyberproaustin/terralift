package reconcile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSecrets(t *testing.T) {
	dir := t.TempDir()
	// A stack that SHIPS app config; some entries look like secrets.
	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`resource "azurerm_linux_web_app" "app" {
  app_settings = {
    LOG_LEVEL                    = "info"
    STORAGE_CONNECTION           = "DefaultEndpointsProtocol=https;AccountName=x;AccountKey=abc123def456ghi789jkl==;EndpointSuffix=core.windows.net"
    APPINSIGHTS_INSTRUMENTATIONKEY = "11112222-3333-4444-5555-666677778888"
    API_KEY                      = "sk-livexyz1234567890abcdefghijklmnopqrstuv"
    UPSTREAM_URL                 = "https://api.example.com"
    DB_REF                       = var.db_password
  }
}
`), 0o644)
	// Structural files must be skipped.
	os.WriteFile(filepath.Join(dir, "variables.tf"), []byte(`variable "db_password" { sensitive = true }`), 0o644)

	rep := ScanSecrets(dir)
	if rep.Files != 1 {
		t.Fatalf("expected 1 scanned file (variables.tf skipped), got %d", rep.Files)
	}
	flagged := map[string]bool{}
	for _, f := range rep.Findings {
		flagged[f.Key] = true
		if f.Resource != "azurerm_linux_web_app.app" {
			t.Errorf("wrong enclosing resource for %s: %q", f.Key, f.Resource)
		}
	}
	// Connection string (value pattern) + instrumentation key (key name) + API_KEY
	// (key name + entropy) must be flagged.
	for _, k := range []string{"STORAGE_CONNECTION", "APPINSIGHTS_INSTRUMENTATIONKEY", "API_KEY"} {
		if !flagged[k] {
			t.Errorf("expected %s to be flagged", k)
		}
	}
	// Benign config and a var reference must NOT be flagged.
	for _, k := range []string{"LOG_LEVEL", "UPSTREAM_URL", "DB_REF"} {
		if flagged[k] {
			t.Errorf("false positive: %s should not be flagged", k)
		}
	}
}

// TestScanSecretsShapes covers the three shapes secrets hide in beyond a plain
// `key = "value"` line: JSON blobs (ECS container_definitions), split name/value
// block pairs (Cloud Run env), and a secret inside a jsonencode() argument.
func TestScanSecretsShapes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ecs.tf"), []byte(`resource "aws_ecs_task_definition" "td" {
  container_definitions = "[{\"name\":\"c\",\"environment\":[{\"name\":\"DB_PASSWORD\",\"value\":\"hunter2plaintext\"},{\"name\":\"LOG\",\"value\":\"info\"}]}]"
}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "run.tf"), []byte(`resource "google_cloud_run_v2_service" "svc" {
  template {
    containers {
      env {
        name  = "API_SECRET"
        value = "sk-live-abc123def456ghi789"
      }
      env {
        name  = "REGION"
        value = "us-central1"
      }
    }
  }
}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "he.tf"), []byte(`resource "x" "y" {
  policy = <<-EOT
    [{"env":[{"name":"CONN","value":"DefaultEndpointsProtocol=https;AccountKey=zzzz1111"}]}]
  EOT
}
`), 0o644)

	rep := ScanSecrets(dir)
	got := map[string]bool{}
	for _, f := range rep.Findings {
		got[f.Key] = true
	}
	// JSON blob env secret + benign LOG untouched.
	if !got["DB_PASSWORD"] {
		t.Errorf("ECS JSON env secret DB_PASSWORD not flagged: %+v", rep.Findings)
	}
	if got["LOG"] {
		t.Errorf("benign ECS env LOG wrongly flagged")
	}
	// Split name/value pair (secret name) + benign REGION untouched.
	if !got["API_SECRET"] {
		t.Errorf("split name/value secret API_SECRET not flagged: %+v", rep.Findings)
	}
	if got["REGION"] {
		t.Errorf("benign split env REGION wrongly flagged")
	}
	// Heredoc JSON with a connection string (value pattern) — flagged via CONN.
	if !got["CONN"] {
		t.Errorf("heredoc JSON connection string not flagged: %+v", rep.Findings)
	}
}

// TestScanSecretsNoSchemaFalsePositives guards that Terraform schema attributes
// whose NAME contains a keyword but whose VALUE is a benign enum/bool are NOT flagged.
func TestScanSecretsNoSchemaFalsePositives(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "r.tf"), []byte(`resource "aws_instance" "i" {
  http_tokens       = "required"
  get_password_data = false
  api_key_source    = "HEADER"
  real_secret       = "Zx7#kT9pLmNq2wSdF8gH1jK"
}
`), 0o644)
	rep := ScanSecrets(dir)
	flagged := map[string]bool{}
	for _, f := range rep.Findings {
		flagged[f.Key] = true
	}
	for _, k := range []string{"http_tokens", "get_password_data", "api_key_source"} {
		if flagged[k] {
			t.Errorf("false positive on TF schema attr %q", k)
		}
	}
	if !flagged["real_secret"] {
		t.Errorf("a real secret-shaped value should still be flagged: %+v", rep.Findings)
	}
}
