# Low-tier compute + containers: one t3.micro EC2 instance (+ EBS + explicit
# ENI from network.tf), an (unattached — realistic brownfield orphan) launch
# template, an ECR repo, and the ECS Fargate cluster/task/service.

data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }
  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

resource "aws_key_pair" "web" {
  key_name   = "${local.name}-web"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJYy5UhP4eyLjRvmRdUULoGbMx6jfU4ax186c/R4KA/6 tlmega-throwaway"
  tags       = { Name = "${local.name}-web-key" }
}

resource "aws_instance" "web" {
  ami           = data.aws_ami.al2023.id
  instance_type = "t3.micro"
  key_name      = aws_key_pair.web.key_name

  network_interface {
    network_interface_id = aws_network_interface.web.id
    device_index         = 0
  }

  iam_instance_profile = aws_iam_instance_profile.ec2.name

  root_block_device {
    volume_size = 30 # AL2023 AMI snapshot requires >= 30GB
    volume_type = "gp3"
    encrypted   = true
  }

  tags = { Name = "${local.name}-web" }
}

resource "aws_ebs_volume" "data" {
  availability_zone = local.az_a
  size              = 5
  type              = "gp3"
  encrypted         = true
  tags              = { Name = "${local.name}-data" }
}

resource "aws_volume_attachment" "data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.web.id
}

# Orphaned-but-plausible launch template — no ASG references it, which is a
# very common brownfield finding (leftover from a decommissioned ASG).
resource "aws_launch_template" "web" {
  name_prefix   = "${local.name}-lt-"
  image_id      = data.aws_ami.al2023.id
  instance_type = "t3.micro"
  key_name      = aws_key_pair.web.key_name

  vpc_security_group_ids = [aws_security_group.web.id]

  tag_specifications {
    resource_type = "instance"
    tags          = { Name = "${local.name}-lt-instance" }
  }

  tags = { Name = "${local.name}-lt" }
}

resource "aws_ecr_repository" "app" {
  name                 = "${local.name}-app"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = { Name = "${local.name}-ecr" }
}

resource "aws_ecs_cluster" "main" {
  name = "${local.name}-cluster"

  setting {
    name  = "containerInsights"
    value = "disabled"
  }

  tags = { Name = "${local.name}-cluster" }
}

# Rich container_definitions: a benign environment[] block PLUS an insecure
# literal secret (flagged by secrets-review) PLUS a secrets[] entry that is a
# Secrets Manager ARN reference (ships clean, no literal value). See
# MANIFEST.md for the full insecure/secure map.
resource "aws_ecs_task_definition" "app" {
  family                   = "${local.name}-app"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = aws_iam_role.ecs_task_execution.arn
  task_role_arn            = aws_iam_role.ecs_task.arn

  container_definitions = jsonencode([
    {
      name      = "app"
      image     = "${aws_ecr_repository.app.repository_url}:latest"
      essential = true
      portMappings = [
        { containerPort = 80, protocol = "tcp" }
      ]
      environment = [
        { name = "APP_ENV", value = "production" },
        { name = "LOG_LEVEL", value = "info" },
        { name = "REGION", value = var.region },
        { name = "SERVICE_NAME", value = "${local.name}-app" },
        { name = "FEATURE_FLAG_NEW_CHECKOUT", value = "true" },
        { name = "CACHE_TTL_SECONDS", value = "300" },
        { name = "MAX_CONNECTIONS", value = "50" },
        { name = "DB_HOST", value = "tlmega-app-db.internal.tlmega.example" },
        { name = "DB_PORT", value = "5432" },
        { name = "DB_USER", value = "app_svc" },
        # INSECURE: literal secret in a container-definitions env entry.
        { name = "DB_PASSWORD", value = "Cx9!vQ2vB7pL-tlmega-plaintext" },
      ]
      secrets = [
        # SECURE: valueFrom is a Secrets Manager ARN, not a literal value.
        { name = "APP_DB_CONN", valueFrom = aws_secretsmanager_secret.app_db.arn }
      ]
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.ecs.name
          "awslogs-region"        = var.region
          "awslogs-stream-prefix" = "app"
        }
      }
    }
  ])

  tags = { Name = "${local.name}-ecs-taskdef" }
}

resource "aws_ecs_service" "app" {
  name            = "${local.name}-svc"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = [aws_subnet.public_a.id, aws_subnet.public_b.id]
    security_groups  = [aws_security_group.ecs.id]
    assign_public_ip = true # no NAT gateway — image pulls need a public IP
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.svc.arn
    container_name   = "app"
    container_port   = 80
  }

  depends_on = [aws_lb_listener.svc]

  tags = { Name = "${local.name}-ecs-svc" }
}
