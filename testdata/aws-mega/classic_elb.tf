# Classic ELB (aws_elb) — deliberately included so the LIVE test exercises
# TerraLift's Classic-vs-v2 load-balancer classification: a Classic ELB's ARN is
# elasticloadbalancing:...:loadbalancer/<name> (no app/net/gwy segment) and must
# resolve to aws_elb, whereas the ALB/NLB in network.tf carry loadbalancer/app|net
# and resolve to aws_lb. This is the only resource that covers the aws_elb branch.
resource "aws_elb" "classic" {
  name            = "${local.name}-clb"
  subnets         = [aws_subnet.public_a.id, aws_subnet.public_b.id]
  security_groups = [aws_security_group.alb.id]

  listener {
    instance_port     = 80
    instance_protocol = "http"
    lb_port           = 80
    lb_protocol       = "http"
  }

  health_check {
    target              = "HTTP:80/"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }

  tags = {
    Name    = "${local.name}-classic-elb"
    Project = "tlmega"
  }
}
