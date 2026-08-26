package stub

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
)

type StorageClient struct{}

func NewStorageClient(target string) (*StorageClient, func(), error) {
	return &StorageClient{}, func() {}, nil
}

var _ client.StorageClient = (*StorageClient)(nil)

func (s *StorageClient) Store(ctx context.Context, input client.UploadInput) (domain.File, error) {
	return domain.File{
		ID:          uuid.Must(uuid.NewV7()).String(),
		Name:        input.Name,
		Size:        input.Size,
		ContentType: input.ContentType,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (s *StorageClient) Fetch(ctx context.Context, id string) (io.ReadCloser, domain.File, error) {
	return io.NopCloser(strings.NewReader("stub file content")), domain.File{
		ID:          id,
		Name:        "stub.txt",
		Size:        17,
		ContentType: "text/plain",
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (s *StorageClient) Metadata(ctx context.Context, id string) (domain.File, error) {
	return domain.File{
		ID:          id,
		Name:        "stub.txt",
		Size:        18,
		ContentType: "text/plain",
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (s *StorageClient) List(ctx context.Context, page, pageSize int) ([]domain.File, int, error) {
	return []domain.File{
		{
			ID:          uuid.Must(uuid.NewV7()).String(),
			Name:        "stub.txt",
			Size:        18,
			ContentType: "text/plain",
			CreatedAt:   time.Now().UTC(),
		},
	}, 1, nil
}

func (s *StorageClient) DownloadArchive(ctx context.Context, ids []string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("stub zip content")), nil
}

func (c *StorageClient) GetDownloadURLs(ctx context.Context, ids []string) ([]client.DownloadURL, error) {
	return []client.DownloadURL{}, nil
}
func (c *StorageClient) GetUploadURL(ctx context.Context, filename, contentType string) (client.UploadURL, error) {
	return client.UploadURL{}, nil
}
func (c *StorageClient) ConfirmUpload(ctx context.Context, fileID string, size int64, checksum string) (domain.File, error) {
	return domain.File{}, nil
}

func (c *StorageClient) GetArchiveUploadURL(ctx context.Context, path, contentType string) (string, error) {
	return "", nil
}

func (c *StorageClient) GetArchiveDownloadURL(ctx context.Context, path string) (string, error) {
	return "", nil
}
