package config

import (
	"fmt"
	"os"
)

type Config struct {
	GRPCPort string
	DBConnectionString string
	StoragePath string
	TempPath string
}

func Load() (*Config, error) {

	storagePath := getEnv("STORAGE_PATH", "./data")
	if err:= os.MkdirAll(storagePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	tempPath:= getEnv("TEMP_PATH", "./data/temp")
	if err:= os.MkdirAll(storagePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Config{
		GRPCPort: getEnv("GRPC_PORT", "50051"),
		DBConnectionString: getEnv("DB_CONNECTION_STRING", ""),
		StoragePath: storagePath,
		TempPath: tempPath,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
