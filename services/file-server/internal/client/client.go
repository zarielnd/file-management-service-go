//go:generate mockgen -source=client.go -destination=../mocks/client_mock.go -package=mocks
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

type StorageClient interface {
	Store(ctx context.Context, input UploadInput) (domain.File, error)
	Fetch(ctx context.Context, id string) (io.ReadCloser, domain.File, error)
	Metadata(ctx context.Context, id string) (domain.File, error)
	List(ctx context.Context, page, pageSize int) ([]domain.File, int, error)
	DownloadArchive(ctx context.Context, ids []string) (io.ReadCloser, error)
}
