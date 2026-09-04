terraform {
  required_version = ">= 1.5"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# ------------------------------------------------------------------------------
# 1. APIs
# ------------------------------------------------------------------------------
resource "google_project_service" "apis" {
  for_each = toset([
    "run.googleapis.com",
    "sqladmin.googleapis.com",
    "secretmanager.googleapis.com",
    "artifactregistry.googleapis.com",
    "vpcaccess.googleapis.com",
    "redis.googleapis.com",
    "cloudtrace.googleapis.com",
    "monitoring.googleapis.com",
  ])
  service            = each.value
  disable_on_destroy = false
}

# ------------------------------------------------------------------------------
# 2. Service Account
# ------------------------------------------------------------------------------
resource "google_service_account" "app" {
  account_id   = "file-management-app"
  display_name = "File Management App"
}

resource "google_project_iam_member" "app_roles" {
  for_each = toset([
    "roles/cloudtrace.agent",
    "roles/monitoring.metricWriter",
    "roles/cloudsql.client",
    "roles/secretmanager.secretAccessor",
    "roles/storage.objectAdmin",
    "roles/storage.admin",
    "roles/artifactregistry.reader",
    "roles/vpcaccess.user"
  ])
  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.app.email}"
}


resource "google_storage_bucket_iam_member" "files_bucket_reader" {
  bucket = google_storage_bucket.files.name
  role   = "roles/storage.legacyBucketOwner"
  member = "serviceAccount:${google_service_account.app.email}"
}

resource "google_storage_bucket_iam_member" "archives_bucket_reader" {
  bucket = google_storage_bucket.archives.name
  role   = "roles/storage.legacyBucketOwner"
  member = "serviceAccount:${google_service_account.app.email}"
}

# ------------------------------------------------------------------------------
# 3. Artifact Registry
# ------------------------------------------------------------------------------
resource "google_artifact_registry_repository" "repo" {
  location      = var.region
  repository_id = "file-management"
  format        = "DOCKER"
  depends_on    = [google_project_service.apis]
}

# ------------------------------------------------------------------------------
# 4. Cloud SQL (Postgres)
# ------------------------------------------------------------------------------
resource "random_password" "db_root" {
  length  = 32
  special = false
}

resource "google_sql_database_instance" "main" {
  name             = "file-management-db"
  database_version = "POSTGRES_17"
  region           = var.region

  settings {
    tier = "db-f1-micro"
    ip_configuration {
      ipv4_enabled = true
      authorized_networks {
        name  = "allow-all-temporarily"
        value = "0.0.0.0/0"
      }
    }
    backup_configuration {
      enabled = true
    }
  }
  deletion_protection = false
  depends_on          = [google_project_service.apis]
}

resource "google_sql_database" "app" {
  name     = "file_management"
  instance = google_sql_database_instance.main.name
}

resource "google_sql_user" "app" {
  name     = "file_user"
  instance = google_sql_database_instance.main.name
  password = random_password.db_root.result
}

# ------------------------------------------------------------------------------
# 5. Memorystore Redis + VPC Connector
# ------------------------------------------------------------------------------
resource "google_redis_instance" "cache" {
  name           = "file-cache"
  tier           = "BASIC"
  memory_size_gb = 1
  region         = var.region
  redis_version  = "REDIS_7_0"
  depends_on     = [google_project_service.apis]
}

resource "google_vpc_access_connector" "connector" {
  name          = "redis-connector"
  region        = var.region
  ip_cidr_range = "10.8.0.0/28"
  network       = "default"
  min_instances = 2
  max_instances = 3
  depends_on    = [google_project_service.apis]
}

# ------------------------------------------------------------------------------
# 6. Cloud Storage (GCS)
# ------------------------------------------------------------------------------
resource "google_storage_bucket" "files" {
  name                        = "${var.project_id}-files"
  location                    = var.region
  force_destroy               = true
  uniform_bucket_level_access = true
}

resource "google_storage_bucket" "archives" {
  name                        = "${var.project_id}-archives"
  location                    = var.region
  force_destroy               = true
  uniform_bucket_level_access = true

  lifecycle_rule {
    action { type = "Delete" }
    condition { age = 1 }
  }
}

# ------------------------------------------------------------------------------
# 7. Secrets
# ------------------------------------------------------------------------------
resource "random_password" "jwt" {
  length  = 64
  special = false
}

resource "random_password" "service_key" {
  length  = 32
  special = false
}


resource "google_storage_hmac_key" "app" {
  service_account_email = google_service_account.app.email
}

resource "google_secret_manager_secret_version" "s3_access_key" {
  secret      = google_secret_manager_secret.secrets["s3-access-key"].id
  secret_data = google_storage_hmac_key.app.access_id
}

resource "google_secret_manager_secret_version" "s3_secret_key" {
  secret      = google_secret_manager_secret.secrets["s3-secret-key"].id
  secret_data = google_storage_hmac_key.app.secret
}

locals {
  db_url = "postgres://${google_sql_user.app.name}:${random_password.db_root.result}@/file_management?host=/cloudsql/${google_sql_database_instance.main.connection_name}&sslmode=disable"
}

resource "google_secret_manager_secret" "secrets" {
  for_each = toset([
    "database-url",
    "jwt-secret",
    "service-key",
    "redis-addr",
    "storage-grpc-target",
    "s3-access-key",
    "s3-secret-key",
    "access-token-ttl",
    "refresh-token-ttl",
    "temporal-host",
    "temporal-api-key",
    "temporal-namespace",
  ])
  secret_id = each.value
  replication {
    auto {}
  }
  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "database_url" {
  secret      = google_secret_manager_secret.secrets["database-url"].id
  secret_data = local.db_url
}

resource "google_secret_manager_secret_version" "jwt_secret" {
  secret      = google_secret_manager_secret.secrets["jwt-secret"].id
  secret_data = random_password.jwt.result
}

resource "google_secret_manager_secret_version" "service_key" {
  secret      = google_secret_manager_secret.secrets["service-key"].id
  secret_data = random_password.service_key.result
}

resource "google_secret_manager_secret_version" "redis_addr" {
  secret      = google_secret_manager_secret.secrets["redis-addr"].id
  secret_data = "${google_redis_instance.cache.host}:${google_redis_instance.cache.port}"
}

resource "google_secret_manager_secret_version" "storage_grpc_target" {
  secret      = google_secret_manager_secret.secrets["storage-grpc-target"].id
  secret_data = google_cloud_run_service.storage.status[0].url
  depends_on  = [google_cloud_run_service.storage]
}

resource "google_secret_manager_secret_version" "access_token_ttl" {
  secret      = google_secret_manager_secret.secrets["access-token-ttl"].id
  secret_data = var.access_token_ttl
}

resource "google_secret_manager_secret_version" "refresh_token_ttl" {
  secret      = google_secret_manager_secret.secrets["refresh-token-ttl"].id
  secret_data = var.refresh_token_ttl
}
resource "google_secret_manager_secret_version" "temporal_host" {
  secret      = google_secret_manager_secret.secrets["temporal-host"].id
  secret_data = var.temporal_host # replace with yours
}

resource "google_secret_manager_secret_version" "temporal_api_key" {
  secret      = google_secret_manager_secret.secrets["temporal-api-key"].id
  secret_data = var.temporal_api_key # replace with yours
}

resource "google_secret_manager_secret_version" "temporal_namespace" {
  secret      = google_secret_manager_secret.secrets["temporal-namespace"].id
  secret_data = var.temporal_namespace # replace with yours
}

# ------------------------------------------------------------------------------
# 8. Cloud Run - Storage Service (gRPC)
# ------------------------------------------------------------------------------
resource "google_cloud_run_service" "storage" {
  name     = "storage-service"
  location = var.region

  template {
    spec {
      service_account_name = google_service_account.app.email
      containers {
        image = "${var.region}-docker.pkg.dev/${var.project_id}/file-management/storage-service:latest"
        ports {
          name           = "h2c"
          container_port = 50051
        }
        env {
          name  = "GRPC_PORT"
          value = "50051"
        }
        env {
          name  = "DEPLOYMENT_ENVIRONMENT"
          value = "production"
        }
        env {
          name  = "GOOGLE_CLOUD_PROJECT"
          value = var.project_id
        }
        env {
          name  = "STORAGE_BACKEND"
          value = "s3"
        }
        env {
          name  = "S3_REGION"
          value = var.region
        }
        env {
          name  = "S3_ENDPOINT"
          value = "https://storage.googleapis.com"
        }
        env {
          name  = "S3_PUBLIC_ENDPOINT"
          value = "https://storage.googleapis.com"
        }
        env {
          name  = "S3_BUCKET"
          value = google_storage_bucket.files.name
        }
        env {
          name  = "S3_ARCHIVE_BUCKET"
          value = google_storage_bucket.archives.name
        }
        env {
          name  = "S3_USE_PATH_STYLE"
          value = "true"
        }

        # Secrets
        dynamic "env" {
          for_each = {
            "DATABASE_URL"  = "database-url"
            "S3_ACCESS_KEY" = "s3-access-key"
            "S3_SECRET_KEY" = "s3-secret-key"
            "SERVICE_KEY"   = "service-key"
          }
          content {
            name = env.key
            value_from {
              secret_key_ref {
                name = env.value
                key  = "latest"
              }
            }
          }
        }
      }
    }

    metadata {
      annotations = {
        "run.googleapis.com/cloudsql-instances"   = google_sql_database_instance.main.connection_name
        "run.googleapis.com/vpc-access-connector" = google_vpc_access_connector.connector.name
        "run.googleapis.com/vpc-access-egress"    = "private-ranges-only"
        "run.googleapis.com/ingress"              = "all"
      }
    }
  }

  traffic {
    percent         = 100
    latest_revision = true
  }

  depends_on = [google_project_service.apis, ]
}

resource "google_cloud_run_service_iam_member" "storage_invoker" {
  service  = google_cloud_run_service.storage.name
  location = google_cloud_run_service.storage.location
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.app.email}" # Internal services still need auth via SERVICE_KEY
}

# ------------------------------------------------------------------------------
# 9. Cloud Run - File Server (HTTP)
# ------------------------------------------------------------------------------
resource "google_cloud_run_service" "file_server" {
  name     = "file-server"
  location = var.region

  template {
    spec {
      service_account_name = google_service_account.app.email
      containers {
        image = "${var.region}-docker.pkg.dev/${var.project_id}/file-management/file-server:latest"
        ports {
          container_port = 8080
        }
        env {
          name  = "SERVER_PORT"
          value = "8080"
        }
        env {
          name  = "DEPLOYMENT_ENVIRONMENT"
          value = "production"
        }
        env {
          name  = "GOOGLE_CLOUD_PROJECT"
          value = var.project_id
        }
        env {
          name  = "MAX_UPLOAD_SIZE"
          value = "104857600"
        }
        env {
          name  = "MAX_MULTIPART_MEMORY"
          value = "33554432"
        }
        env {
          name  = "USE_TEMPORAL_ARCHIVE"
          value = "true"
        }
        env {
          name  = "TEMPORAL_QUEUE"
          value = "archive-queue"
        }

        # Secrets
        dynamic "env" {
          for_each = {
            "DATABASE_URL"             = "database-url"
            "JWT_SECRET"               = "jwt-secret"
            "SERVICE_KEY"              = "service-key"
            "REDIS_ADDR"               = "redis-addr"
            "ACCESS_TOKEN_TTL_MINUTES" = "access-token-ttl"
            "REFRESH_TOKEN_TTL_DAYS"   = "refresh-token-ttl"
            "TEMPORAL_HOST"            = "temporal-host"
            "TEMPORAL_API_KEY"         = "temporal-api-key"
            "TEMPORAL_NAMESPACE"       = "temporal-namespace"
            "STORAGE_GRPC_TARGET"      = "storage-grpc-target"
          }
          content {
            name = env.key
            value_from {
              secret_key_ref {
                name = env.value
                key  = "latest"
              }
            }
          }
        }
      }
    }

    metadata {
      annotations = {
        "run.googleapis.com/cloudsql-instances"   = google_sql_database_instance.main.connection_name
        "run.googleapis.com/vpc-access-connector" = google_vpc_access_connector.connector.name
        "run.googleapis.com/vpc-access-egress"    = "private-ranges-only"
      }
    }
  }

  traffic {
    percent         = 100
    latest_revision = true
  }

  depends_on = [google_cloud_run_service.storage,
    google_secret_manager_secret_version.access_token_ttl,
    google_secret_manager_secret_version.refresh_token_ttl,
    # Also add the other secrets this service uses so Terraform knows the order:
    google_secret_manager_secret_version.database_url,
    google_secret_manager_secret_version.jwt_secret,
    google_secret_manager_secret_version.service_key,
    google_secret_manager_secret_version.redis_addr,
  ]
}

resource "google_cloud_run_service_iam_member" "file_server_invoker" {
  service  = google_cloud_run_service.file_server.name
  location = google_cloud_run_service.file_server.location
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# ------------------------------------------------------------------------------
# 10. Cloud Run Job - Migrate
# ------------------------------------------------------------------------------
resource "google_cloud_run_v2_job" "migrate" {
  name     = "migrate"
  location = var.region

  template {
    template {
      service_account = google_service_account.app.email
      containers {
        image   = "${var.region}-docker.pkg.dev/${var.project_id}/file-management/migrate:latest"
        command = ["/migrate"]
        args    = ["-path", "/migrations", "-database", local.db_url, "up"]
      }
    }
  }

  depends_on = [google_project_service.apis]
}

# ------------------------------------------------------------------------------
# 11. Cloud Run Job - Worker
# ------------------------------------------------------------------------------
resource "google_cloud_run_v2_job" "worker" {
  name     = "worker"
  location = var.region

  template {
    template {
      service_account = google_service_account.app.email
      containers {
        image = "${var.region}-docker.pkg.dev/${var.project_id}/file-management/worker:latest"
        env {
          name  = "TEMPORAL_QUEUE"
          value = "archive-queue"
        }
        env {
          name  = "GOOGLE_CLOUD_PROJECT"
          value = var.project_id
        }
        dynamic "env" {
          for_each = {
            "SERVICE_KEY"         = "service-key"
            "TEMPORAL_HOST"       = "temporal-host"
            "TEMPORAL_API_KEY"    = "temporal-api-key"
            "TEMPORAL_NAMESPACE"  = "temporal-namespace"
            "STORAGE_GRPC_TARGET" = "storage-grpc-target"
          }
          content {
            name = env.key
            value_source {
              secret_key_ref {
                secret  = env.value
                version = "latest"
              }
            }
          }
        }
      }
    }
  }

  depends_on = [google_project_service.apis]
}
