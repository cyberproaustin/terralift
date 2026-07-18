package aws

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedactGeneratedHCL is a thin integration check that the AWS rules are wired
// to the shared redactor; the redaction logic itself is covered by internal/hcl.
func TestRedactGeneratedHCL(t *testing.T) {
	src := `resource "aws_ssm_parameter" "secure" {
  type  = "SecureString"
  value = "SECURE-do-not-capture"
}

resource "aws_ssm_parameter" "plain" {
  type  = "String"
  value = "benign-config"
}

resource "aws_lambda_function" "fn" {
  environment {
    variables = {
      TOKEN = "LAMBDA-SECRET-do-not-capture"
    }
  }
}

resource "aws_db_instance" "db" {
  password = "DB-PLAINTEXT-do-not-capture"
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "generated.tf")
	os.WriteFile(p, []byte(src), 0o644)
	if events := redactGeneratedHCL(p); len(events) < 2 {
		t.Fatalf("expected >=2 redactions, got %d", len(events))
	}
	out, _ := os.ReadFile(p)
	got := string(out)
	// Unambiguous single secrets (SecureString param value, DB password) are removed.
	for _, secret := range []string{"SECURE-do-not-capture", "DB-PLAINTEXT-do-not-capture"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret leaked: %q\n%s", secret, got)
		}
	}
	// App CONFIG (Lambda env) SHIPS — it is the whole point of onboarding to IaC and
	// is flagged in reports/secrets-review.md instead of being wiped.
	if !strings.Contains(got, "LAMBDA-SECRET-do-not-capture") {
		t.Errorf("lambda env var was wrongly wiped (config must ship):\n%s", got)
	}
	if !strings.Contains(got, "benign-config") {
		t.Errorf("plain String value wrongly blanked:\n%s", got)
	}
	if !strings.Contains(got, "ignore_changes = [value]") {
		t.Errorf("SecureString not ignore_changes-protected:\n%s", got)
	}
}
