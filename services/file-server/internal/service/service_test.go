package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/mocks"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/service"
	temporalClient "go.temporal.io/sdk/client"
)

// ---------------------------------------------------------------------
// Helper: inject user ID into context.
// If your client package does not export WithUserID, replace this with
// whatever exported helper your project uses (e.g. client.NewContextWithUserID).
// ---------------------------------------------------------------------
func withUserID(ctx context.Context, userID string) context.Context {
	return client.WithUserID(ctx, userID)
}

func sha256Sum(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

type mockTemporalClient struct {
	temporalClient.Client
	executeWorkflowFunc func(context.Context, temporalClient.StartWorkflowOptions, interface{}, ...interface{}) (temporalClient.WorkflowRun, error)
}

func (m *mockTemporalClient) ExecuteWorkflow(ctx context.Context, options temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
	if m.executeWorkflowFunc != nil {
		return m.executeWorkflowFunc(ctx, options, workflow, args...)
	}
	return nil, nil
}

// ====================================================================
// Upload
// ====================================================================

func TestFileService_Upload(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				assert.Equal(t, "test-queue", opts.TaskQueue)
				assert.True(t, len(opts.ID) > 0)
				return nil, nil
			},
		}

		content := []byte("hello world")
		checksum := sha256Sum(content)

		// Presigned-URL stand-in (S3, MinIO, etc.)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))

			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.Equal(t, content, body)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		fileID := "file-123"
		mockStorage.EXPECT().
			GetUploadURL(gomock.Any(), "test.txt", "text/plain").
			Return(client.UploadURL{UploadURL: server.URL, FileID: fileID}, nil)

		mockStorage.EXPECT().
			ConfirmUpload(gomock.Any(), fileID, int64(len(content)), checksum).
			Return(domain.File{ID: fileID, Name: "test.txt", Size: int64(len(content))}, nil)

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)

		got, err := svc.Upload(context.Background(), client.UploadInput{
			Name:        "test.txt",
			ContentType: "text/plain",
			Size:        int64(len(content)),
			Content:     bytes.NewReader(content),
		})

		require.NoError(t, err)
		assert.Equal(t, fileID, got.ID)
		assert.Equal(t, "test.txt", got.Name)
		assert.Equal(t, int64(len(content)), got.Size)
	})

	t.Run("file too large", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 100)

		_, err := svc.Upload(context.Background(), client.UploadInput{
			Name:        "big.txt",
			ContentType: "text/plain",
			Size:        101,
			Content:     bytes.NewReader([]byte("x")),
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "maximum limit")
	})

	t.Run("GetUploadURL fails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		mockStorage.EXPECT().
			GetUploadURL(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(client.UploadURL{}, errors.New("storage unreachable"))

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)

		_, err := svc.Upload(context.Background(), client.UploadInput{
			Name:        "test.txt",
			ContentType: "text/plain",
			Size:        5,
			Content:     bytes.NewReader([]byte("hello")),
		})

		require.Error(t, err)
	})

	t.Run("presigned upload returns non-200", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("access denied"))
		}))
		defer server.Close()

		mockStorage.EXPECT().
			GetUploadURL(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(client.UploadURL{UploadURL: server.URL, FileID: "f1"}, nil)

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)

		_, err := svc.Upload(context.Background(), client.UploadInput{
			Name:        "test.txt",
			ContentType: "text/plain",
			Size:        5,
			Content:     bytes.NewReader([]byte("hello")),
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "upload failed")
	})

	t.Run("ConfirmUpload fails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		mockStorage.EXPECT().
			GetUploadURL(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(client.UploadURL{UploadURL: server.URL, FileID: "f1"}, nil)

		mockStorage.EXPECT().
			ConfirmUpload(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(domain.File{}, errors.New("confirm failed"))

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)

		_, err := svc.Upload(context.Background(), client.UploadInput{
			Name:        "test.txt",
			ContentType: "text/plain",
			Size:        5,
			Content:     bytes.NewReader([]byte("hello")),
		})

		require.Error(t, err)
	})
}

// ====================================================================
// Download
// ====================================================================

func TestFileService_Download(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		wantFile := domain.File{ID: "abc", Name: "doc.txt", Size: 12}
		wantBody := io.NopCloser(strings.NewReader("file content"))

		mockStorage.EXPECT().
			Fetch(gomock.Any(), "abc").
			Return(wantBody, wantFile, nil)

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		reader, file, err := svc.Download(context.Background(), "abc")

		require.NoError(t, err)
		assert.Equal(t, wantFile, file)

		got, _ := io.ReadAll(reader)
		assert.Equal(t, "file content", string(got))
		_ = reader.Close()
	})

	t.Run("storage error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		mockStorage.EXPECT().
			Fetch(gomock.Any(), "missing").
			Return(nil, domain.File{}, errors.New("not found"))

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		_, _, err := svc.Download(context.Background(), "missing")

		require.Error(t, err)
	})
}

// ====================================================================
// List
// ====================================================================

func TestFileService_List(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		wantFiles := []domain.File{
			{ID: "1", Name: "a.txt"},
			{ID: "2", Name: "b.txt"},
		}

		mockStorage.EXPECT().
			List(gomock.Any(), 2, 50).
			Return(wantFiles, 999, nil)

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		files, total, err := svc.List(context.Background(), 2, 50)

		require.NoError(t, err)
		assert.Equal(t, wantFiles, files)
		assert.Equal(t, 999, total)
	})

	t.Run("storage error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		mockStorage.EXPECT().
			List(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, 0, errors.New("db down"))

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		_, _, err := svc.List(context.Background(), 1, 20)

		require.Error(t, err)
	})
}

// ====================================================================
// Metadata
// ====================================================================

func TestFileService_Metadata(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		want := domain.File{ID: "meta-1", Name: "x.txt", Size: 42}

		mockStorage.EXPECT().
			Metadata(gomock.Any(), "meta-1").
			Return(want, nil)

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		got, err := svc.Metadata(context.Background(), "meta-1")

		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("storage error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, nil
			},
		}

		mockStorage.EXPECT().
			Metadata(gomock.Any(), "missing").
			Return(domain.File{}, errors.New("not found"))

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		_, err := svc.Metadata(context.Background(), "missing")

		require.Error(t, err)
	})
}

// ====================================================================
// StartArchiveWorkflow
// ====================================================================

func TestFileService_StartArchiveWorkflow(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(ctx context.Context, opts temporalClient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalClient.WorkflowRun, error) {
				assert.Equal(t, "test-queue", opts.TaskQueue)
				assert.True(t, len(opts.ID) > 0)
				return nil, nil
			},
		}

		mockStorage.EXPECT().
			GetDownloadURLs(gomock.Any(), []string{"f1", "f2"}).
			Return([]client.DownloadURL{ // <-- was []string
				{FileID: "f1", URL: "http://s3/f1"},
				{FileID: "f2", URL: "http://s3/f2"},
			}, nil)

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		ctx := withUserID(context.Background(), "user-42")

		archiveID, wsEndpoint, err := svc.StartArchiveWorkflow(ctx, []string{"f1", "f2"})

		require.NoError(t, err)
		assert.NotEmpty(t, archiveID)
		assert.Equal(t, "/api/archives/"+archiveID+"/ws", wsEndpoint)
	})

	t.Run("missing user id", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{}

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		_, _, err := svc.StartArchiveWorkflow(context.Background(), []string{"f1"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing user id")
	})

	t.Run("GetDownloadURLs fails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{}

		mockStorage.EXPECT().
			GetDownloadURLs(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("storage timeout"))

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		ctx := withUserID(context.Background(), "user-1")

		_, _, err := svc.StartArchiveWorkflow(ctx, []string{"f1"})
		require.Error(t, err)
	})

	t.Run("ownership check fails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{}

		mockStorage.EXPECT().
			GetDownloadURLs(gomock.Any(), []string{"f1", "f2"}).
			Return([]client.DownloadURL{ // <-- was []string
				{FileID: "f1", URL: "http://s3/f1"},
			}, nil)

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		ctx := withUserID(context.Background(), "user-1")

		_, _, err := svc.StartArchiveWorkflow(ctx, []string{"f1", "f2"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "access denied")
	})

	t.Run("ExecuteWorkflow fails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockStorage := mocks.NewMockStorageClient(ctrl)
		mockTemporal := &mockTemporalClient{
			executeWorkflowFunc: func(context.Context, temporalClient.StartWorkflowOptions, interface{}, ...interface{}) (temporalClient.WorkflowRun, error) {
				return nil, errors.New("temporal unreachable")
			},
		}

		mockStorage.EXPECT().
			GetDownloadURLs(gomock.Any(), gomock.Any()).
			Return([]client.DownloadURL{ // <-- was []string
				{FileID: "f1", URL: "http://s3/f1"},
			}, nil)

		svc := service.NewFileService(mockStorage, mockTemporal, "test-queue", 10<<20)
		ctx := withUserID(context.Background(), "user-1")

		_, _, err := svc.StartArchiveWorkflow(ctx, []string{"f1"})
		require.Error(t, err)
	})
}
