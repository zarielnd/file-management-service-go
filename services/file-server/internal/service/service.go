package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	temporalClient "go.temporal.io/sdk/client"

	"github.com/google/uuid"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/apperror"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/temporal/workflows"
)

type FileService struct {
	storageClient  client.StorageClient
	httpClient     *http.Client
	maxFileSize    int64
	temporalClient temporalClient.Client
	temporalQueue  string
}

func NewFileService(
	storageClient client.StorageClient,
	temporalClient temporalClient.Client,
	temporalQueue string,
	maxFileSize int64,
) *FileService {
	return &FileService{
		storageClient:  storageClient,
		temporalClient: temporalClient,
		temporalQueue:  temporalQueue,
		httpClient:     &http.Client{Timeout: 5 * time.Minute},
		maxFileSize:    maxFileSize,
	}
}

func (s *FileService) Upload(ctx context.Context, input client.UploadInput) (domain.File, error) {
	if input.Size > s.maxFileSize {
		return domain.File{}, apperror.Invalid("file size exceeds the maximum limit")
	}

	// 1. Reserve + get presigned URL
	reserved, err := s.storageClient.GetUploadURL(ctx, input.Name, input.ContentType)
	if err != nil {
		return domain.File{}, err
	}

	// 2. Compute checksum while streaming
	hash := sha256.New()
	tee := io.TeeReader(input.Content, hash)

	// 3. Upload directly to S3 via HTTP PUT
	size, err := s.uploadToPresignedURL(ctx, reserved.UploadURL, tee, input.Size)
	if err != nil {
		return domain.File{}, apperror.Internal("upload failed")
	}

	checksum := hex.EncodeToString(hash.Sum(nil))

	// 4. Confirm with Storage Service
	return s.storageClient.ConfirmUpload(ctx, reserved.FileID, size, checksum)
}

func (s *FileService) uploadToPresignedURL(ctx context.Context, url string, reader io.Reader, size int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = size

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("s3 upload failed: %d %s", resp.StatusCode, string(body))
	}

	return size, nil
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

func (s *FileService) StartArchiveWorkflow(ctx context.Context, ids []string) (archiveID string, wsEndpoint string, err error) {
	archiveID = uuid.Must(uuid.NewV7()).String()

	_, err = s.temporalClient.ExecuteWorkflow(ctx,
		temporalClient.StartWorkflowOptions{
			ID:        "archive-" + archiveID,
			TaskQueue: s.temporalQueue,
		},
		workflows.BulkDownloadWorkflow,
		workflows.ArchiveRequest{
			FileIDs:   ids,
			ArchiveID: archiveID,
		},
	)
	if err != nil {
		return "", "", err
	}

	wsEndpoint = "/api/archives/" + archiveID + "/ws"
	return archiveID, wsEndpoint, nil
}
