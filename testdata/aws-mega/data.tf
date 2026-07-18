# Data / integration layer: DynamoDB (on-demand), SNS -> SQS fan-out,
# EventBridge custom bus + rule, and a Kinesis stream in ON_DEMAND mode
# (no idle per-shard cost).

resource "aws_dynamodb_table" "sessions" {
  name         = "${local.name}-sessions"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "session_id"

  attribute {
    name = "session_id"
    type = "S"
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }

  tags = { Name = "${local.name}-dynamodb-sessions" }
}

resource "aws_dynamodb_table" "orders" {
  name         = "${local.name}-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"
  range_key    = "created_at"

  attribute {
    name = "order_id"
    type = "S"
  }
  attribute {
    name = "created_at"
    type = "S"
  }
  attribute {
    name = "customer_id"
    type = "S"
  }

  global_secondary_index {
    name            = "customer-index"
    hash_key        = "customer_id"
    projection_type = "ALL"
  }

  tags = { Name = "${local.name}-dynamodb-orders" }
}

resource "aws_sns_topic" "events" {
  name = "${local.name}-events"
  tags = { Name = "${local.name}-sns-events" }
}

resource "aws_sns_topic_policy" "events" {
  arn = aws_sns_topic.events.arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "events.amazonaws.com" }
      Action    = "sns:Publish"
      Resource  = aws_sns_topic.events.arn
    }]
  })
}

resource "aws_sqs_queue" "jobs" {
  name                      = "${local.name}-jobs"
  message_retention_seconds = 86400
  tags                      = { Name = "${local.name}-sqs-jobs" }
}

resource "aws_sqs_queue_policy" "jobs" {
  queue_url = aws_sqs_queue.jobs.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "sns.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.jobs.arn
      Condition = { ArnEquals = { "aws:SourceArn" = aws_sns_topic.events.arn } }
    }]
  })
}

resource "aws_sns_topic_subscription" "jobs" {
  topic_arn = aws_sns_topic.events.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.jobs.arn
}

resource "aws_cloudwatch_event_bus" "main" {
  name = "${local.name}-bus"
  tags = { Name = "${local.name}-bus" }
}

resource "aws_cloudwatch_event_rule" "order_created" {
  name           = "${local.name}-order-created"
  event_bus_name = aws_cloudwatch_event_bus.main.name

  event_pattern = jsonencode({
    source      = ["tlmega.orders"]
    detail-type = ["OrderCreated"]
  })

  tags = { Name = "${local.name}-rule-order-created" }
}

resource "aws_cloudwatch_event_target" "order_created_sns" {
  rule           = aws_cloudwatch_event_rule.order_created.name
  event_bus_name = aws_cloudwatch_event_bus.main.name
  arn            = aws_sns_topic.events.arn
}

resource "aws_kinesis_stream" "activity" {
  name             = "${local.name}-activity"
  retention_period = 24

  stream_mode_details {
    stream_mode = "ON_DEMAND" # no per-shard idle cost
  }

  tags = { Name = "${local.name}-kinesis-activity" }
}
