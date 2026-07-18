# Two VPCs (peered), public/private/isolated subnets across 2 AZs, an
# internal ALB + NLB (LB-type classification exercise), Route53 private +
# public zones, and gateway + interface VPC endpoints. Deliberately NO NAT
# gateway / VPN / Transit Gateway (cost + provisioning time) — the ECS
# service gets internet access via a public subnet + public IP instead.

# --- VPCs -------------------------------------------------------------

resource "aws_vpc" "main" {
  cidr_block           = "10.60.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "${local.name}-vpc-main" }
}

resource "aws_vpc" "peer" {
  cidr_block           = "10.61.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "${local.name}-vpc-peer" }
}

resource "aws_vpc_peering_connection" "main_to_peer" {
  vpc_id      = aws_vpc.main.id
  peer_vpc_id = aws_vpc.peer.id
  auto_accept = true # same account/region — no separate accepter needed
  tags        = { Name = "${local.name}-peering" }
}

# --- Subnets ------------------------------------------------------------

resource "aws_subnet" "public_a" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.60.0.0/24"
  availability_zone       = local.az_a
  map_public_ip_on_launch = true
  tags                    = { Name = "${local.name}-public-a", Tier = "public" }
}

resource "aws_subnet" "public_b" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.60.1.0/24"
  availability_zone       = local.az_b
  map_public_ip_on_launch = true
  tags                    = { Name = "${local.name}-public-b", Tier = "public" }
}

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.60.10.0/24"
  availability_zone = local.az_a
  tags              = { Name = "${local.name}-private-a", Tier = "private" }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.60.11.0/24"
  availability_zone = local.az_b
  tags              = { Name = "${local.name}-private-b", Tier = "private" }
}

resource "aws_subnet" "isolated_a" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.60.20.0/24"
  availability_zone = local.az_a
  tags              = { Name = "${local.name}-isolated-a", Tier = "isolated" }
}

resource "aws_subnet" "isolated_b" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.60.21.0/24"
  availability_zone = local.az_b
  tags              = { Name = "${local.name}-isolated-b", Tier = "isolated" }
}

resource "aws_subnet" "peer_a" {
  vpc_id            = aws_vpc.peer.id
  cidr_block        = "10.61.0.0/24"
  availability_zone = local.az_a
  tags              = { Name = "${local.name}-peer-a" }
}

# --- Internet edge + routing ---------------------------------------------

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${local.name}-igw" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${local.name}-rt-public" }
}

resource "aws_route" "public_internet" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.main.id
}

resource "aws_route_table_association" "public_a" {
  subnet_id      = aws_subnet.public_a.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table_association" "public_b" {
  subnet_id      = aws_subnet.public_b.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${local.name}-rt-private" }
}

# No NAT — private tier only routes to the peered VPC, plus the S3 gateway
# endpoint below. It is genuinely egress-isolated, which is realistic for a
# brownfield "private" tier.
resource "aws_route" "private_to_peer" {
  route_table_id            = aws_route_table.private.id
  destination_cidr_block    = aws_vpc.peer.cidr_block
  vpc_peering_connection_id = aws_vpc_peering_connection.main_to_peer.id
}

resource "aws_route_table_association" "private_a" {
  subnet_id      = aws_subnet.private_a.id
  route_table_id = aws_route_table.private.id
}

resource "aws_route_table_association" "private_b" {
  subnet_id      = aws_subnet.private_b.id
  route_table_id = aws_route_table.private.id
}

resource "aws_route_table" "isolated" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${local.name}-rt-isolated" }
}

resource "aws_route_table_association" "isolated_a" {
  subnet_id      = aws_subnet.isolated_a.id
  route_table_id = aws_route_table.isolated.id
}

resource "aws_route_table_association" "isolated_b" {
  subnet_id      = aws_subnet.isolated_b.id
  route_table_id = aws_route_table.isolated.id
}

# --- VPC endpoints (no NAT needed for S3 / Secrets Manager reachability) --

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id, aws_route_table.isolated.id]
  tags              = { Name = "${local.name}-vpce-s3" }
}

resource "aws_vpc_endpoint" "secretsmanager" {
  vpc_id              = aws_vpc.main.id
  service_name        = "com.amazonaws.${var.region}.secretsmanager"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private_a.id, aws_subnet.private_b.id]
  security_group_ids  = [aws_security_group.vpce.id]
  private_dns_enabled = true
  tags                = { Name = "${local.name}-vpce-secretsmanager" }
}

# --- Security groups -------------------------------------------------------

resource "aws_security_group" "web" {
  name        = "${local.name}-web"
  description = "EC2 web tier - HTTP/SSH from within the VPC only"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "http from vpc"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.main.cidr_block]
  }
  ingress {
    description = "ssh from vpc"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.main.cidr_block]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = { Name = "${local.name}-sg-web" }
}

resource "aws_security_group" "alb" {
  name        = "${local.name}-alb"
  description = "Internal ALB security group"
  vpc_id      = aws_vpc.main.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = { Name = "${local.name}-sg-alb" }
}

# Standalone rule resource (vs. inline ingress{}) — a common brownfield mix.
resource "aws_security_group_rule" "alb_ingress_http" {
  type              = "ingress"
  from_port         = 80
  to_port           = 80
  protocol          = "tcp"
  cidr_blocks       = [aws_vpc.main.cidr_block]
  security_group_id = aws_security_group.alb.id
  description       = "http from vpc"
}

resource "aws_security_group" "ecs" {
  name        = "${local.name}-ecs"
  description = "ECS Fargate service tier"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "http from vpc (nlb health checks + traffic)"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.main.cidr_block]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = { Name = "${local.name}-sg-ecs" }
}

resource "aws_security_group" "vpce" {
  name        = "${local.name}-vpce"
  description = "Interface VPC endpoint security group"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "https from vpc"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.main.cidr_block]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = { Name = "${local.name}-sg-vpce" }
}

# --- Network ACL (isolated tier gets an explicit, restrictive NACL) -------

resource "aws_network_acl" "isolated" {
  vpc_id     = aws_vpc.main.id
  subnet_ids = [aws_subnet.isolated_a.id, aws_subnet.isolated_b.id]
  tags       = { Name = "${local.name}-nacl-isolated" }
}

resource "aws_network_acl_rule" "isolated_ingress_vpc" {
  network_acl_id = aws_network_acl.isolated.id
  rule_number    = 100
  egress         = false
  protocol       = "-1"
  rule_action    = "allow"
  cidr_block     = aws_vpc.main.cidr_block
  from_port      = 0
  to_port        = 0
}

resource "aws_network_acl_rule" "isolated_egress_vpc" {
  network_acl_id = aws_network_acl.isolated.id
  rule_number    = 100
  egress         = true
  protocol       = "-1"
  rule_action    = "allow"
  cidr_block     = aws_vpc.main.cidr_block
  from_port      = 0
  to_port        = 0
}

# --- Route53 (private + public hosted zones) ------------------------------

resource "aws_route53_zone" "private" {
  name = "internal.tlmega.example"
  vpc {
    vpc_id = aws_vpc.main.id
  }
  tags = { Name = "${local.name}-zone-private" }
}

resource "aws_route53_zone" "public" {
  name = "${local.name}.tlmega-lab.com" # example.com is reserved by AWS
  tags = { Name = "${local.name}-zone-public" }
}

resource "aws_route53_record" "app_internal" {
  zone_id = aws_route53_zone.private.zone_id
  name    = "app.internal.tlmega.example"
  type    = "A"
  alias {
    name                   = aws_lb.app.dns_name
    zone_id                = aws_lb.app.zone_id
    evaluate_target_health = true
  }
}

resource "aws_route53_record" "www_public" {
  zone_id = aws_route53_zone.public.zone_id
  name    = "www.${local.name}.tlmega-lab.com"
  type    = "A"
  alias {
    name                   = aws_lb.app.dns_name
    zone_id                = aws_lb.app.zone_id
    evaluate_target_health = true
  }
}

# --- Load balancers: one ALB, one NLB (exercises TerraLift's LB-type ------
# --- classification: elasticloadbalancing:loadbalancer/app vs /net) -------

resource "aws_lb" "app" {
  name               = "${local.name}-alb"
  internal           = true
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = [aws_subnet.private_a.id, aws_subnet.private_b.id]
  tags               = { Name = "${local.name}-alb" }
}

resource "aws_lb_target_group" "app" {
  name        = "${local.name}-tg-app"
  port        = 80
  protocol    = "HTTP"
  vpc_id      = aws_vpc.main.id
  target_type = "instance"

  health_check {
    path                = "/"
    healthy_threshold   = 2
    unhealthy_threshold = 5
    interval            = 30
    timeout             = 5
  }

  tags = { Name = "${local.name}-tg-app" }
}

resource "aws_lb_target_group_attachment" "app" {
  target_group_arn = aws_lb_target_group.app.arn
  target_id        = aws_instance.web.id
  port             = 80
}

resource "aws_lb_listener" "app" {
  load_balancer_arn = aws_lb.app.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app.arn
  }
}

resource "aws_lb" "svc" {
  name               = "${local.name}-nlb"
  internal           = true
  load_balancer_type = "network"
  subnets            = [aws_subnet.private_a.id, aws_subnet.private_b.id]
  tags               = { Name = "${local.name}-nlb" }
}

resource "aws_lb_target_group" "svc" {
  name        = "${local.name}-tg-svc"
  port        = 80
  protocol    = "TCP"
  vpc_id      = aws_vpc.main.id
  target_type = "ip" # required for awsvpc-mode Fargate registration

  health_check {
    protocol            = "TCP"
    healthy_threshold   = 2
    unhealthy_threshold = 2
    interval            = 10
  }

  tags = { Name = "${local.name}-tg-svc" }
}

resource "aws_lb_listener" "svc" {
  load_balancer_arn = aws_lb.svc.arn
  port              = 80
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.svc.arn
  }
}

# --- Explicit ENI, attached to the EC2 instance below ---------------------

resource "aws_network_interface" "web" {
  subnet_id       = aws_subnet.public_a.id
  security_groups = [aws_security_group.web.id]
  tags            = { Name = "${local.name}-eni-web" }
}
