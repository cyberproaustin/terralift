# KMS CMK, plus the secrets-handling key comparison: a Secrets Manager
# secret + an SSM SecureString param (SECURE — TerraLift ships a reference)
# vs. an SSM String param holding a secret-looking value (INSECURE — TerraLift
# flags it). See MANIFEST.md for the complete map.

resource "aws_kms_key" "main" {
  description             = "${local.name} general-purpose CMK"
  deletion_window_in_days = 7
  enable_key_rotation     = true
  tags                    = { Name = "${local.name}-kms" }
}

resource "aws_kms_alias" "main" {
  name          = "alias/${local.name}"
  target_key_id = aws_kms_key.main.key_id
}

# --- SECURE: Secrets Manager, referenced (not literal) by the ECS task -----

resource "aws_secretsmanager_secret" "app_db" {
  name                    = "${local.name}-app-db"
  recovery_window_in_days = 0 # allow immediate delete on teardown
  tags                    = { Name = "${local.name}-secret-app-db" }
}

resource "aws_secretsmanager_secret_version" "app_db" {
  secret_id = aws_secretsmanager_secret.app_db.id
  secret_string = jsonencode({
    username = "app_svc"
    password = "SECURE-managed-do-not-capture-9f3aQ2"
    host     = "tlmega-app-db.internal.tlmega.example"
    port     = 5432
  })
}

# --- SECURE: SSM SecureString ----------------------------------------------

resource "aws_ssm_parameter" "app_api_secret" {
  name   = "/${local.name}/app/api-secret"
  type   = "SecureString"
  value  = "SECURE-managed-do-not-capture-k7Lp9x"
  key_id = aws_kms_key.main.key_id
  tags   = { Name = "${local.name}-ssm-api-secret" }
}

# --- INSECURE: plain String param holding a secret-looking value -----------

resource "aws_ssm_parameter" "legacy_db_password" {
  name  = "/${local.name}/legacy/db-password"
  type  = "String"
  value = "INSECURE-plaintext-do-not-ship-m4Tq8w"
  tags  = { Name = "${local.name}-ssm-legacy-db-password" }
}
