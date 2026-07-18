variable "region" {
  description = "AWS region to deploy the brownfield environment into."
  type        = string
  default     = "us-east-1"
}

variable "prefix" {
  description = "Name prefix for every tlmega- resource."
  type        = string
  default     = "tlmega"
}
