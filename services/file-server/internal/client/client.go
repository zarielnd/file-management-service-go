package client

import (
	"context"
	"io"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
)

type UploadInput struct {
	Name        string
	ContentType string
	Size        int64
	Content     io.Reader
}

type DownloadURL struct {
	FileID    string
	Name      string
	URL       string
	SizeBytes int64
}

type UploadURL struct {
	UploadURL string
	FileID    string
}

type StorageClient interface {
	Store(ctx context.Context, input UploadInput) (domain.File, error)
	Fetch(ctx context.Context, id string) (io.ReadCloser, domain.File, error)
	Metadata(ctx context.Context, id string) (domain.File, error)
	List(ctx context.Context, page, pageSize int) ([]domain.File, int, error)
	DownloadArchive(ctx context.Context, ids []string) (io.ReadCloser, error)
	GetDownloadURLs(ctx context.Context, ids []string) ([]DownloadURL, error)
	GetUploadURL(ctx context.Context, filename, contentType string) (UploadURL, error)
	ConfirmUpload(ctx context.Context, fileID string, size int64, checksum string) (domain.File, error)
	GetArchiveUploadURL(ctx context.Context, path, contentType string) (string, error)
	GetArchiveDownloadURL(ctx context.Context, path string) (string, error)
}
