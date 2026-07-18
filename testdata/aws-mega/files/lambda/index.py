"""tlmega-mega placeholder Lambda handler.

Minimal proxy-integration responder — just enough for `terraform apply` to
have a real deployment package. Not application logic under test.
"""
import json
import os


def handler(event, context):
    return {
        "statusCode": 200,
        "headers": {"Content-Type": "application/json"},
        "body": json.dumps({
            "service": os.environ.get("SERVICE_NAME", "tlmega-api"),
            "message": "ok",
        }),
    }
