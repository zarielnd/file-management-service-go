package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	GRPCPort string
	DBConnectionString string
	StoragePath string
	TempPath string
}

func Load() (*Config, error) {
	godotenv.Load("../../.env");

	storagePath := getEnv("STORAGE_PATH", "./data")
	if err:= os.MkdirAll(storagePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	tempPath:= getEnv("TEMP_PATH", "./data/temp")
	if err:= os.MkdirAll(tempPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Config{
		GRPCPort: getEnv("GRPC_PORT", "50051"),
		DBConnectionString: getEnv("DATABASE_URL", ""),
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
