package service

import (
	"context"
	"io"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/apperror"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
)

const maxFileSize = 100 * 1024 * 1024

type FileService struct {
	storageClient client.StorageClient
}

func NewFileService(storageClient client.StorageClient) *FileService {
	return &FileService{
		storageClient: storageClient,
	}
}

func (s * FileService) Upload(ctx context.Context, input client.UploadInput) (domain.File, error) {
	if input.Size > maxFileSize {
		return domain.File{}, apperror.Invalid("file size exceeds the maximum limit")
	}

	return s.storageClient.Store(ctx, input)
}

func (s *FileService) Download(ctx context.Context, id string) (io.ReadCloser, domain.File, error) {
	return s.storageClient.Fetch(ctx, id)
}

func (s *FileService) List(ctx context.Context, page, pageSize int) ([]domain.File, int, error) {
	return s.storageClient.List(ctx, page, pageSize)
}

func (s *FileService) DownloadMultiple(ctx context.Context, ids []string) (io.ReadCloser, error) {
	return s.storageClient.DownloadArchive(ctx, ids)
}

func (s *FileService) Metadata(ctx context.Context, id string) (domain.File, error) {
	return s.storageClient.Metadata(ctx, id)
}
