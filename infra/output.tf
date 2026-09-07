output "file_server_url" {
  description = "Public URL of the file-server"
  value       = try(google_cloud_run_service.file_server.status[0].url,null)
}

output "storage_service_url" {
  description = "Internal URL of the storage-service"
  value       = try(google_cloud_run_service.storage.status[0].url, null)
}

output "database_connection_name" {
  description = "Cloud SQL connection name"
  value       = try(google_sql_database_instance.main.connection_name)
}

output "redis_host" {
  description = "Redis host:port"
  value       = "${google_redis_instance.cache.host}:${google_redis_instance.cache.port}"
}

output "gcs_files_bucket" {
  value = google_storage_bucket.files.name
}

output "gcs_archives_bucket" {
  value = google_storage_bucket.archives.name
}

output "db_password" {
  description = "Auto-generated DB password"
  value       = random_password.db_root.result
  sensitive   = true
}

output "jwt_secret" {
  description = "Auto-generated JWT secret"
  value       = random_password.jwt.result
  sensitive   = true
}

output "service_key" {
  description = "Auto-generated internal service key"
  value       = random_password.service_key.result
  sensitive   = true
}