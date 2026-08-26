package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort         string
	StorageGRPCTarget  string
	MaxUploadSize      int64
	MaxMultipartMemory int64
	UseTemporalArchive bool   // USE_TEMPORAL_ARCHIVE
	TemporalHost       string // TEMPORAL_HOST
	TemporalQueue      string // TEMPORAL_QUEUE
}

func Load() (*Config, error) {
	godotenv.Load("../../.env")

	maxSize, err := parseBytes(getEnv("MAX_UPLOAD_SIZE", fmt.Sprintf("%d", 10*1024*1024))) // Default to 10 MB
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_UPLOAD_SIZE: %v", err)
	}
	maxMem, err := parseBytes(getEnv("MAX_MULTIPART_MEMORY", fmt.Sprintf("%d", 32*1024*1024))) // Default to 32 MB
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_MULTIPART_MEMORY: %w", err)
	}
	return &Config{
		ServerPort:         getEnv("SERVER_PORT", "8080"),
		StorageGRPCTarget:  getEnv("STORAGE_GRPC_TARGET", "localhost:50051"),
		MaxUploadSize:      maxSize,
		MaxMultipartMemory: maxMem,
		UseTemporalArchive: getEnv("USE_TEMPORAL_ARCHIVE", "false") == "true",
		TemporalHost:       getEnv("TEMPORAL_HOST", "temporal:7233"),
		TemporalQueue:      getEnv("TEMPORAL_QUEUE", "archive-queue"),
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
