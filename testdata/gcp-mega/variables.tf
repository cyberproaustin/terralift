variable "project_id" {
  type        = string
  description = "GCP project to deploy the mega-seed into."
  default     = "terralift-mega-161207246"
}

variable "region" {
  type    = string
  default = "us-central1"
}

variable "zone" {
  type    = string
  default = "us-central1-a"
}

variable "prefix" {
  type    = string
  default = "tlmega"
}
