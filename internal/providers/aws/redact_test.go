package aws

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactGeneratedHCL(t *testing.T) {
	src := `resource "aws_ssm_parameter" "secure" {
  name  = "/app/secure"
  type  = "SecureString"
  value = "SECURE-do-not-capture"
}

resource "aws_ssm_parameter" "plain" {
  name  = "/app/plain"
  type  = "String"
  value = "benign-config"
}

resource "aws_lambda_function" "fn" {
  function_name = "fn"
  environment {
    variables = {
      LOG_LEVEL = "info"
      API_TOKEN = "LAMBDA-SECRET-do-not-capture"
    }
  }
}

resource "aws_db_instance" "db" {
  password = "DB-PLAINTEXT-do-not-capture"
}

resource "aws_x" "heredoc" {
  private_key = <<-EOT
    SECRET-PEM-BODY-do-not-capture
    more-secret
  EOT
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "generated.tf")
	os.WriteFile(p, []byte(src), 0o644)

	n := redactGeneratedHCL(p)
	out, _ := os.ReadFile(p)
	got := string(out)

	// Nothing marked "do-not-capture" may survive.
	for _, secret := range []string{
		"SECURE-do-not-capture", "LAMBDA-SECRET-do-not-capture",
		"DB-PLAINTEXT-do-not-capture", "SECRET-PEM-BODY-do-not-capture", "more-secret",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("secret leaked after redact: %q\n%s", secret, got)
		}
	}
	// The plain String parameter's value must be PRESERVED (not a secret).
	if !strings.Contains(got, "benign-config") {
		t.Errorf("plain String value wrongly blanked:\n%s", got)
	}
	// Structure preserved: the env var KEY stays, the block headers stay.
	if !strings.Contains(got, "API_TOKEN") || !strings.Contains(got, `resource "aws_ssm_parameter" "secure"`) {
		t.Errorf("structure not preserved:\n%s", got)
	}
	if n < 4 {
		t.Errorf("expected >=4 redactions, got %d", n)
	}
}
