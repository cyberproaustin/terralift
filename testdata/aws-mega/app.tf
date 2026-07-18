# Application layer: a Lambda function with a rich (20-var) environment{}
# block mixing benign config with plaintext secrets, fronted by an API
# Gateway REST API with an API key + usage plan. See MANIFEST.md for the
# insecure/secure secrets map.

data "archive_file" "lambda" {
  type        = "zip"
  source_dir  = "${path.module}/files/lambda"
  output_path = "${path.module}/files/lambda.zip"
}

resource "aws_lambda_function" "api" {
  function_name    = "${local.name}-api"
  role             = aws_iam_role.lambda_exec.arn
  handler          = "index.handler"
  runtime          = "python3.12"
  filename         = data.archive_file.lambda.output_path
  source_code_hash = data.archive_file.lambda.output_base64sha256
  timeout          = 10
  memory_size      = 128

  environment {
    variables = {
      APP_ENV            = "production"
      LOG_LEVEL          = "info"
      REGION             = var.region
      SERVICE_NAME       = "${local.name}-api"
      FEATURE_FLAG_BETA  = "false"
      CACHE_TTL_SECONDS  = "60"
      MAX_RETRIES        = "3"
      RATE_LIMIT_PER_MIN = "120"
      DOWNSTREAM_URL     = "https://internal-api.tlmega.example"
      DYNAMODB_TABLE     = aws_dynamodb_table.sessions.name
      SNS_TOPIC_ARN      = aws_sns_topic.events.arn
      SQS_QUEUE_URL      = aws_sqs_queue.jobs.url
      SMTP_HOST          = "smtp.tlmega.example"
      SMTP_PORT          = "587"
      SUPPORT_EMAIL      = "support@tlmega.example"
      REQUEST_TIMEOUT_MS = "5000"
      ENABLE_TRACING     = "true"
      # INSECURE: literal DB password shipped in plaintext (flagged by
      # TerraLift secrets-review — key name matches the secret heuristic).
      DB_PASSWORD = "Zx7#kT4mN9qR-tlmega-plaintext"
      # INSECURE: connection string with an embedded credential (flagged by
      # the value-pattern heuristic: "Password=").
      LEGACY_DB_CONNECTION_STRING = "Server=tlmega-legacy.internal;Database=app;User Id=app_svc;Password=Hn3$Lp8vXe2!;"
    }
  }

  tags = { Name = "${local.name}-lambda-api" }
}

resource "aws_api_gateway_rest_api" "main" {
  name = "${local.name}-api"
  tags = { Name = "${local.name}-api" }
}

resource "aws_api_gateway_resource" "proxy" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_rest_api.main.root_resource_id
  path_part   = "{proxy+}"
}

resource "aws_api_gateway_method" "proxy" {
  rest_api_id      = aws_api_gateway_rest_api.main.id
  resource_id      = aws_api_gateway_resource.proxy.id
  http_method      = "ANY"
  authorization    = "NONE"
  api_key_required = true
}

resource "aws_api_gateway_integration" "lambda" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.proxy.id
  http_method             = aws_api_gateway_method.proxy.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.api.invoke_arn
}

resource "aws_api_gateway_deployment" "main" {
  rest_api_id = aws_api_gateway_rest_api.main.id

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.proxy.id,
      aws_api_gateway_method.proxy.id,
      aws_api_gateway_integration.lambda.id,
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }

  depends_on = [aws_api_gateway_integration.lambda]
}

resource "aws_api_gateway_stage" "prod" {
  deployment_id = aws_api_gateway_deployment.main.id
  rest_api_id   = aws_api_gateway_rest_api.main.id
  stage_name    = "prod"
  tags          = { Name = "${local.name}-stage-prod" }
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*/*"
}

# INSECURE: AWS auto-generates a real API key value here (the `value`
# attribute) — TerraLift must redact it, not ship it, when exporting.
resource "aws_api_gateway_api_key" "client" {
  name = "${local.name}-client-key"
  tags = { Name = "${local.name}-client-key" }
}

resource "aws_api_gateway_usage_plan" "main" {
  name = "${local.name}-usage-plan"

  api_stages {
    api_id = aws_api_gateway_rest_api.main.id
    stage  = aws_api_gateway_stage.prod.stage_name
  }

  throttle_settings {
    burst_limit = 5
    rate_limit  = 10
  }

  tags = { Name = "${local.name}-usage-plan" }
}

resource "aws_api_gateway_usage_plan_key" "main" {
  key_id        = aws_api_gateway_api_key.client.id
  key_type      = "API_KEY"
  usage_plan_id = aws_api_gateway_usage_plan.main.id
}
