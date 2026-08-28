package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort         string
	StorageGRPCTarget  string
	MaxUploadSize      int64
	MaxMultipartMemory int64
	UseTemporalArchive bool
	TemporalHost       string
	TemporalQueue      string
	DatabaseURL        string
	RedisAddr          string
	//auth
	JWTSecret       string
	ServiceKey      string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

func Load() (*Config, error) {
	godotenv.Load("../../.env")

	maxSize, err := parseBytes(getEnv("MAX_UPLOAD_SIZE", fmt.Sprintf("%d", 10*1024*1024)))
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_UPLOAD_SIZE: %v", err)
	}
	maxMem, err := parseBytes(getEnv("MAX_MULTIPART_MEMORY", fmt.Sprintf("%d", 32*1024*1024)))
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_MULTIPART_MEMORY: %w", err)
	}

	accessTTL, _ := strconv.Atoi(getEnv("ACCESS_TOKEN_TTL_MINUTES", "15"))
	refreshTTL, _ := strconv.Atoi(getEnv("REFRESH_TOKEN_TTL_DAYS", "7"))

	return &Config{
		ServerPort:         getEnv("SERVER_PORT", "8080"),
		StorageGRPCTarget:  getEnv("STORAGE_GRPC_TARGET", "localhost:50051"),
		MaxUploadSize:      maxSize,
		MaxMultipartMemory: maxMem,
		UseTemporalArchive: getEnv("USE_TEMPORAL_ARCHIVE", "false") == "true",
		TemporalHost:       getEnv("TEMPORAL_HOST", "temporal:7233"),
		TemporalQueue:      getEnv("TEMPORAL_QUEUE", "archive-queue"),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		JWTSecret:          getEnv("JWT_SECRET", "change-me-in-production"),
		AccessTokenTTL:     time.Duration(accessTTL) * time.Minute,
		RefreshTokenTTL:    time.Duration(refreshTTL) * 24 * time.Hour,
		ServiceKey:         getEnv("SERVICE_KEY", ""),
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
