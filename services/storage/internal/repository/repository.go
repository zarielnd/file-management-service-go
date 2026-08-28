package repository

import (
	"context"
	"time"
)

type File struct {
	ID          string
	Name        string
	StoragePath string
	ContentType string
	SizeBytes   int64
	Checksum    string
	CreatedAt   time.Time
	OwnerID     string
}

// services/storage/internal/repository/repository.go
type Repository interface {
	Create(ctx context.Context, file *File) error
	GetByID(ctx context.Context, id string, userID string) (*File, error)
	GetByIDs(ctx context.Context, ids []string, userID string) ([]*File, error)
	List(ctx context.Context, userID string, limit, offset int) ([]*File, int, error)
	Update(ctx context.Context, file *File) error
	ConfirmUpload(ctx context.Context, id string, size int64, checksum string) error
}
