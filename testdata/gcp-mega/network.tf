# ---- VPCs (custom subnet mode) + peering -----------------------------------
resource "google_compute_network" "core" {
  name                    = "${var.prefix}-vpc-core"
  auto_create_subnetworks = false
  mtu                     = 1460
}

resource "google_compute_network" "data" {
  name                    = "${var.prefix}-vpc-data"
  auto_create_subnetworks = false
  mtu                     = 1460
}

resource "google_compute_network_peering" "core_to_data" {
  name         = "${var.prefix}-peer-core-to-data"
  network      = google_compute_network.core.id
  peer_network = google_compute_network.data.id
}

resource "google_compute_network_peering" "data_to_core" {
  name         = "${var.prefix}-peer-data-to-core"
  network      = google_compute_network.data.id
  peer_network = google_compute_network.core.id
}

# ---- Subnets: secondary ranges + Private Google Access ----------------------
resource "google_compute_subnetwork" "core_primary" {
  name                     = "${var.prefix}-core-primary"
  ip_cidr_range            = "10.10.0.0/20"
  region                   = var.region
  network                  = google_compute_network.core.id
  private_ip_google_access = true

  secondary_ip_range {
    range_name    = "${var.prefix}-pods"
    ip_cidr_range = "10.20.0.0/16"
  }
  secondary_ip_range {
    range_name    = "${var.prefix}-services"
    ip_cidr_range = "10.21.0.0/20"
  }
}

resource "google_compute_subnetwork" "core_lb" {
  name                     = "${var.prefix}-core-lb"
  ip_cidr_range            = "10.11.0.0/24"
  region                   = var.region
  network                  = google_compute_network.core.id
  private_ip_google_access = true
}

resource "google_compute_subnetwork" "data_primary" {
  name                     = "${var.prefix}-data-primary"
  ip_cidr_range            = "10.30.0.0/20"
  region                   = var.region
  network                  = google_compute_network.data.id
  private_ip_google_access = true
}

# ---- Firewalls: ingress + egress, split across both VPCs --------------------
resource "google_compute_firewall" "core_allow_internal" {
  name          = "${var.prefix}-core-allow-internal"
  network       = google_compute_network.core.id
  direction     = "INGRESS"
  source_ranges = ["10.10.0.0/20", "10.11.0.0/24"]

  allow {
    protocol = "tcp"
    ports    = ["0-65535"]
  }
  allow {
    protocol = "udp"
    ports    = ["0-65535"]
  }
  allow {
    protocol = "icmp"
  }
}

resource "google_compute_firewall" "core_allow_iap_ssh" {
  name          = "${var.prefix}-core-allow-iap-ssh"
  network       = google_compute_network.core.id
  direction     = "INGRESS"
  source_ranges = ["35.235.240.0/20"] # IAP TCP forwarding range only — no 0.0.0.0/0 SSH here
  target_tags   = ["${var.prefix}-ssh"]

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }
}

resource "google_compute_firewall" "core_allow_lb_health_check" {
  name          = "${var.prefix}-core-allow-lb-health-check"
  network       = google_compute_network.core.id
  direction     = "INGRESS"
  source_ranges = ["130.211.0.0/22", "35.191.0.0/16"] # GFE health-check ranges
  target_tags   = ["${var.prefix}-web"]

  allow {
    protocol = "tcp"
    ports    = ["80"]
  }
}

resource "google_compute_firewall" "data_allow_internal" {
  name          = "${var.prefix}-data-allow-internal"
  network       = google_compute_network.data.id
  direction     = "INGRESS"
  source_ranges = ["10.30.0.0/20"]

  allow {
    protocol = "tcp"
    ports    = ["0-65535"]
  }
}

resource "google_compute_firewall" "data_deny_egress_all" {
  name               = "${var.prefix}-data-deny-egress-all"
  network            = google_compute_network.data.id
  direction          = "EGRESS"
  priority           = 65534
  destination_ranges = ["0.0.0.0/0"]

  deny {
    protocol = "all"
  }
}

resource "google_compute_firewall" "data_allow_egress_google_apis" {
  name               = "${var.prefix}-data-allow-egress-google-apis"
  network            = google_compute_network.data.id
  direction          = "EGRESS"
  priority           = 1000
  destination_ranges = ["199.36.153.8/30"] # private.googleapis.com VIP

  allow {
    protocol = "tcp"
    ports    = ["443"]
  }
}

# ---- Custom route + Cloud Router (no NAT attached — free) -------------------
resource "google_compute_route" "data_custom" {
  name             = "${var.prefix}-data-custom-route"
  network          = google_compute_network.data.id
  dest_range       = "192.168.100.0/24"
  next_hop_gateway = "default-internet-gateway"
  priority         = 1000
  tags             = ["${var.prefix}-nva"]
}

resource "google_compute_router" "core" {
  name    = "${var.prefix}-router-core"
  region  = var.region
  network = google_compute_network.core.id

  bgp {
    asn = 64514
  }
}

# ---- Reserved IPs -------------------------------------------------------------
resource "google_compute_address" "vm_static" {
  name   = "${var.prefix}-vm-static-ip"
  region = var.region
}

resource "google_compute_global_address" "lb_vip" {
  name = "${var.prefix}-lb-vip"
}

# ---- Cloud DNS: public + private managed zones -------------------------------
resource "google_dns_managed_zone" "public" {
  name        = "${var.prefix}-public-zone"
  dns_name    = "tlmega-lab.example.com."
  description = "TerraLift mega-seed public zone"
  visibility  = "public"
}

resource "google_dns_managed_zone" "private" {
  name        = "${var.prefix}-private-zone"
  dns_name    = "tlmega.internal."
  description = "TerraLift mega-seed private zone"
  visibility  = "private"

  private_visibility_config {
    networks {
      network_url = google_compute_network.core.id
    }
  }
}

resource "google_dns_record_set" "public_app" {
  name         = "app.${google_dns_managed_zone.public.dns_name}"
  managed_zone = google_dns_managed_zone.public.name
  type         = "A"
  ttl          = 300
  rrdatas      = [google_compute_global_address.lb_vip.address]
}

resource "google_dns_record_set" "private_vm" {
  name         = "vm.${google_dns_managed_zone.private.dns_name}"
  managed_zone = google_dns_managed_zone.private.name
  type         = "A"
  ttl          = 300
  rrdatas      = [google_compute_instance.vm.network_interface[0].network_ip]
}

# ---- Global external HTTP load balancer (cheapest LB — no managed cert) -----
resource "google_compute_health_check" "http" {
  name = "${var.prefix}-http-hc"

  check_interval_sec = 10
  timeout_sec        = 5

  http_health_check {
    port = 80
  }
}

resource "google_compute_backend_service" "web" {
  name                  = "${var.prefix}-web-backend"
  protocol              = "HTTP"
  port_name             = "http"
  load_balancing_scheme = "EXTERNAL"
  health_checks         = [google_compute_health_check.http.id]

  backend {
    group = google_compute_instance_group_manager.web.instance_group
  }
}

resource "google_compute_url_map" "web" {
  name            = "${var.prefix}-web-urlmap"
  default_service = google_compute_backend_service.web.id
}

resource "google_compute_target_http_proxy" "web" {
  name    = "${var.prefix}-web-proxy"
  url_map = google_compute_url_map.web.id
}

resource "google_compute_global_forwarding_rule" "web" {
  name                  = "${var.prefix}-web-fr"
  target                = google_compute_target_http_proxy.web.id
  ip_address            = google_compute_global_address.lb_vip.address
  port_range            = "80"
  load_balancing_scheme = "EXTERNAL"
}
