package storage

import (
	"context"
	"fmt"

	"github.com/zarielnd/file-management-service-go/services/storage/internal/config"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/storage/local"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/storage/s3store"
)

func NewProviderFromConfig(ctx context.Context, cfg *config.Config) (Provider, error) {
	switch cfg.StorageBackend {
	case "local":
		return local.NewStore(cfg.StoragePath), nil
	case "s3":
		return s3store.NewStore(ctx, s3store.Config{
			Endpoint:     cfg.S3Endpoint,
			Region:       cfg.S3Region,
			Bucket:       cfg.S3Bucket,
			AccessKey:    cfg.S3AccessKey,
			SecretKey:    cfg.S3SecretKey,
			UsePathStyle: cfg.S3UsePathStyle,
		})
	default:
		return nil, fmt.Errorf("unknown storage backend: %s", cfg.StorageBackend)
	}
}
