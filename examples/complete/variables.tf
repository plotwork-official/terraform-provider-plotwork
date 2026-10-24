variable "environment" {
  description = "The environment's name, used in names, folders and the stable link."
  type        = string
  default     = "prod"
}

variable "region" {
  description = "The AWS region."
  type        = string
  default     = "eu-west-1"
}

variable "docs_bucket" {
  description = "The S3 bucket the architecture SVGs are written to."
  type        = string
}
