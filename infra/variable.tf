variable "project_id" {
  description = "GCP Project ID"
  type        = string
}

variable "region" {
  description = "GCP Region"
  type        = string
  default     = "asia-southeast1"
}

variable "temporal_host" {
  description = "Temporal server host:port. Use Temporal Cloud or self-hosted IP"
  type        = string
  default     = "temporal:7233"
}

variable "jwt_secret" {
  description = "JWT secret"
  type        = string
  sensitive   = true
}
variable "service_key" {
  description = "Internal service key"
  type        = string
  sensitive   = true
}

variable "temporal_api_key" {
  description = "Temporal API key"
  type        = string
  sensitive   = true
}

variable "temporal_namespace" {
  description = "Temporal namespace"
  type        = string
  default     = "default"
}

# variables.tf
variable "access_token_ttl" {
  type      = string
  sensitive = true
}

variable "refresh_token_ttl" {
  type      = string
  sensitive = true
}
