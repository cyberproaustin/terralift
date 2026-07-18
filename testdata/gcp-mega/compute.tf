# ---- Single e2-micro VM + attached data disk --------------------------------
resource "google_compute_disk" "vm_data" {
  name = "${var.prefix}-vm-data-disk"
  zone = var.zone
  type = "pd-standard"
  size = 10
}

resource "google_compute_instance" "vm" {
  name         = "${var.prefix}-vm"
  machine_type = "e2-micro"
  zone         = var.zone
  tags         = ["${var.prefix}-ssh"]

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-12"
      size  = 10
      type  = "pd-standard"
    }
  }

  attached_disk {
    source = google_compute_disk.vm_data.id
  }

  network_interface {
    subnetwork = google_compute_subnetwork.core_primary.id
    access_config {
      nat_ip = google_compute_address.vm_static.address
    }
  }

  service_account {
    email  = google_service_account.compute.email
    scopes = ["cloud-platform"]
  }

  metadata_startup_script = "#! /bin/bash\necho tlmega-vm-ok > /var/tmp/tlmega-ready\n"

  labels = {
    app = var.prefix
  }
}

# ---- Instance template + small managed instance group (LB backend) ----------
resource "google_compute_instance_template" "web" {
  name_prefix  = "${var.prefix}-web-tmpl-"
  machine_type = "e2-micro"

  disk {
    source_image = "debian-cloud/debian-12"
    auto_delete  = true
    boot         = true
    disk_size_gb = 10
    disk_type    = "pd-standard"
  }

  network_interface {
    subnetwork = google_compute_subnetwork.core_lb.id
  }

  tags = ["${var.prefix}-web"]

  service_account {
    email  = google_service_account.compute.email
    scopes = ["cloud-platform"]
  }

  # Debian 12 ships python3 out of the box, so this serves HTTP without any
  # internet egress — no Cloud NAT required for the LB backend to come healthy.
  metadata_startup_script = <<-EOT
    #! /bin/bash
    mkdir -p /var/www
    echo "tlmega-web-ok" > /var/www/index.html
    cd /var/www && nohup python3 -m http.server 80 >/tmp/tlmega-http.log 2>&1 &
  EOT

  lifecycle {
    create_before_destroy = true
  }
}

resource "google_compute_instance_group_manager" "web" {
  name               = "${var.prefix}-web-mig"
  zone               = var.zone
  base_instance_name = "${var.prefix}-web"
  target_size        = 1

  version {
    instance_template = google_compute_instance_template.web.id
  }

  named_port {
    name = "http"
    port = 80
  }
}

# ---- Artifact Registry --------------------------------------------------------
resource "google_artifact_registry_repository" "images" {
  location      = var.region
  repository_id = "${var.prefix}-images"
  format        = "DOCKER"
  description   = "TerraLift mega-seed container images"
}
