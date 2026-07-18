package hcl

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	src := `resource "aws_db_instance" "db" {
  password = "PLAINTEXT-do-not-capture"
  engine   = "postgres"
}

resource "aws_iam_x" "pem" {
  private_key = <<-EOT
    -----BEGIN KEY-----
    SECRET-BODY-do-not-capture
    -----END KEY-----
  EOT
  name = "x"
}

resource "aws_ssm_parameter" "sec" {
  name  = "/p"
  type  = "SecureString"
  value = "SECURE-do-not-capture"
}

resource "aws_ssm_parameter" "plain" {
  type  = "String"
  value = "benign-config"
}

resource "aws_lambda_function" "fn" {
  function_name = "fn"
  environment {
    variables = {
      LOG   = "info"
      TOKEN = "LAMBDA-do-not-capture"
    }
  }
}

resource "aws_ecs_task_definition" "td" {
  container_definitions = "[{\"name\":\"c\",\"environment\":[{\"name\":\"DB\",\"value\":\"ECS-do-not-capture\"}]}]"
}
`
	rules := []Rule{
		{Type: "aws_ssm_parameter", Attr: "value", Kind: Scalar, OnlyIfContains: `"SecureString"`},
		{Type: "aws_lambda_function", Attr: "variables", Kind: MapBlock},
		{Type: "aws_ecs_task_definition", Attr: "container_definitions", Kind: JSONEnv},
	}
	out, events := Redact(src, []string{"password", "private_key"}, rules)

	for _, secret := range []string{
		"PLAINTEXT-do-not-capture", "SECRET-BODY-do-not-capture", "SECURE-do-not-capture",
		"LAMBDA-do-not-capture", "ECS-do-not-capture",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("secret leaked: %q\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "benign-config") {
		t.Errorf("plain String value wrongly blanked:\n%s", out)
	}
	// SecureString value blanked + protected with ignore_changes.
	if !strings.Contains(out, `value = ""`) || !strings.Contains(out, "ignore_changes = [value]") {
		t.Errorf("SecureString not protected:\n%s", out)
	}
	// Lambda env block removed entirely (optional -> unmanaged, not overwritten).
	if strings.Contains(out, "TOKEN") {
		t.Errorf("lambda env not removed:\n%s", out)
	}
	// ECS env blanked in-place + ignore_changes.
	if !strings.Contains(out, "ignore_changes = [container_definitions]") {
		t.Errorf("ecs container_definitions not protected:\n%s", out)
	}
	// Heredoc terminator must not dangle.
	if strings.Contains(out, "-----END KEY-----") {
		t.Errorf("heredoc body survived:\n%s", out)
	}
	if len(events) < 5 {
		t.Errorf("expected >=5 redactions, got %d", len(events))
	}
	// Events must name the resource type + attr + action so the report is useful.
	wantEvent := func(resType, attr, action string) {
		for _, e := range events {
			if e.Resource == resType && e.Attr == attr && e.Action == action {
				return
			}
		}
		t.Errorf("missing redaction event {%s %s %s} in %+v", resType, attr, action, events)
	}
	wantEvent("aws_db_instance", "password", "removed")
	wantEvent("aws_ssm_parameter", "value", "blanked")
	wantEvent("aws_lambda_function", "variables", "removed")
	wantEvent("aws_ecs_task_definition", "container_definitions", "blanked")
}

// TestRemoveSecretAttrsDepthOnly guards the config-ships mandate: an exact-name
// "secret" that is actually an app-config map KEY (nested, depth 2) must survive,
// while the same name as a real top-level resource attribute (depth 1) is removed.
func TestRemoveSecretAttrsDepthOnly(t *testing.T) {
	src := `resource "aws_db_instance" "db" {
  password = "TOP-LEVEL-SECRET-do-not-capture"
  engine   = "postgres"
}

resource "aws_lambda_function" "fn" {
  environment {
    variables = {
      CLIENT_SECRET = "CONFIG-ships-keep-me"
      AUTH_TOKEN    = "CONFIG-ships-keep-me-too"
    }
  }
}
`
	out, _ := Redact(src, []string{"password", "client_secret", "auth_token"}, nil)
	if strings.Contains(out, "TOP-LEVEL-SECRET-do-not-capture") {
		t.Errorf("top-level password not removed:\n%s", out)
	}
	// The env-var keys collide with exact-name secrets but are config → must ship.
	if !strings.Contains(out, "CLIENT_SECRET") || !strings.Contains(out, "AUTH_TOKEN") {
		t.Errorf("app-config env keys wrongly deleted (config must ship):\n%s", out)
	}
}

// TestBlankAllOccurrences guards that a repeated secret attr in sibling nested
// blocks is fully scrubbed, not just the first.
func TestBlankAllOccurrences(t *testing.T) {
	src := `resource "azurerm_container_group" "g" {
  container {
    secure_environment_variables = {
      K = "SECRET-ONE-do-not-capture"
    }
  }
  container {
    secure_environment_variables = {
      K = "SECRET-TWO-do-not-capture"
    }
  }
}
`
	rules := []Rule{{Type: "azurerm_container_group", Attr: "secure_environment_variables", Kind: MapBlock}}
	out, _ := Redact(src, nil, rules)
	for _, s := range []string{"SECRET-ONE-do-not-capture", "SECRET-TWO-do-not-capture"} {
		if strings.Contains(out, s) {
			t.Errorf("sibling-block secret survived: %q\n%s", s, out)
		}
	}
}

// TestIgnoreChangesMergesExistingLifecycle guards that a blanked REQUIRED secret is
// protected even when the resource already has a lifecycle block.
func TestIgnoreChangesMergesExistingLifecycle(t *testing.T) {
	src := `resource "azurerm_mssql_server" "s" {
  administrator_login_password = "PLAINTEXT-do-not-capture"
  lifecycle {
    prevent_destroy = true
  }
}
`
	rules := []Rule{{Type: "azurerm_mssql_server", Attr: "administrator_login_password", Kind: Scalar}}
	out, _ := Redact(src, nil, rules)
	if strings.Contains(out, "PLAINTEXT-do-not-capture") {
		t.Errorf("password not blanked:\n%s", out)
	}
	if !strings.Contains(out, "prevent_destroy") {
		t.Errorf("existing lifecycle clobbered:\n%s", out)
	}
	if !strings.Contains(out, "ignore_changes") || !strings.Contains(out, "administrator_login_password") {
		t.Errorf("ignore_changes not merged into existing lifecycle:\n%s", out)
	}
}

func TestBraceDeltaIgnoresStrings(t *testing.T) {
	if d := braceDelta(`  key = "team{env/state"`); d != 0 {
		t.Errorf("brace in string counted: delta=%d", d)
	}
	if d := braceDelta(`resource "x" "y" {`); d != 1 {
		t.Errorf("real open brace: delta=%d", d)
	}
	if d := braceDelta(`  x = 1 # comment with }`); d != 0 {
		t.Errorf("brace in comment counted: delta=%d", d)
	}
}
