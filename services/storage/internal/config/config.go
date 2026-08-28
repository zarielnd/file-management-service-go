package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	GRPCPort string

	// Storage backend: "local" or "s3"
	StorageBackend string

	// Local storage (only used when StorageBackend == "local")
	StoragePath string
	TempPath    string

	// S3 / MinIO (only used when StorageBackend == "s3")
	S3Endpoint       string
	S3PublicEndpoint string
	S3Region         string
	S3Bucket         string
	S3ArchiveBucket  string
	S3AccessKey      string
	S3SecretKey      string
	S3UsePathStyle   bool // true for MinIO/LocalStack

	DBConnectionString string
	Environment        string

	GoogleCloudProject string
}

func Load() (*Config, error) {
	_ = godotenv.Load("../../.env")

	cfg := &Config{
		GRPCPort:       getEnv("GRPC_PORT", "50051"),
		StorageBackend: getEnv("STORAGE_BACKEND", "local"),

		// Local
		StoragePath: getEnv("STORAGE_PATH", "./data"),
		TempPath:    getEnv("TEMP_PATH", "./data/temp"),

		// S3
		S3Endpoint:       getEnv("S3_ENDPOINT", ""),
		S3PublicEndpoint: getEnv("S3_PUBLIC_ENDPOINT", ""),
		S3Region:         getEnv("S3_REGION", "us-east-1"),
		S3Bucket:         getEnv("S3_BUCKET", ""),
		S3ArchiveBucket:  getEnv("S3_ARCHIVE_BUCKET", ""),
		S3AccessKey:      getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:      getEnv("S3_SECRET_KEY", ""),
		S3UsePathStyle:   getEnv("S3_USE_PATH_STYLE", "true") == "true",

		DBConnectionString: getEnv("DATABASE_URL", ""),
		Environment:        getEnv("DEPLOYMENT_ENVIRONMENT", "production"),
		GoogleCloudProject: getEnv("GOOGLE_CLOUD_PROJECT", ""),
	}

	// Only create local directories when using local backend
	if cfg.StorageBackend == "local" {
		if err := os.MkdirAll(cfg.StoragePath, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create storage directory: %w", err)
		}
		if err := os.MkdirAll(cfg.TempPath, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create temp directory: %w", err)
		}
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
