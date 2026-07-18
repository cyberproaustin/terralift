resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${local.name}-api"
  retention_in_days = 7
  tags              = { Name = "${local.name}-lg-lambda" }
}

resource "aws_cloudwatch_log_group" "ecs" {
  name              = "/ecs/${local.name}-app"
  retention_in_days = 7
  tags              = { Name = "${local.name}-lg-ecs" }
}

resource "aws_cloudwatch_metric_alarm" "lambda_errors" {
  alarm_name          = "${local.name}-lambda-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 5
  alarm_description   = "tlmega-mega Lambda error count"

  dimensions = {
    FunctionName = aws_lambda_function.api.function_name
  }

  alarm_actions = [aws_sns_topic.events.arn]
  tags          = { Name = "${local.name}-alarm-lambda-errors" }
}

resource "aws_cloudwatch_dashboard" "main" {
  dashboard_name = "${local.name}-overview"
  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          title   = "Lambda Errors"
          view    = "timeSeries"
          region  = var.region
          metrics = [["AWS/Lambda", "Errors", "FunctionName", aws_lambda_function.api.function_name]]
        }
      }
    ]
  })
}
