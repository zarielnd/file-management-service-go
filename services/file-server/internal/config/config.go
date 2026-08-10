package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	ServerPort string
	StorageGRPCTarget string
	MaxUploadSize int64
	MaxMultipartMemory int64
}

func Load() (*Config, error) {

	maxSize, err := parseBytes(getEnv("MAX_UPLOAD_SIZE", fmt.Sprintf("%d", 10*1024*1024))) // Default to 10 MB
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_UPLOAD_SIZE: %v", err)
	}
	return &Config{
		ServerPort: getEnv("SERVER_PORT", "8080"),
		StorageGRPCTarget: getEnv("STORAGE_GRPC_TARGET", "localhost:50051"),
		MaxUploadSize: maxSize,
		MaxMultipartMemory: 32 << 20, // 32 MB
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseBytes(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}