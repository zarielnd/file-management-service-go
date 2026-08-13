package file

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/apperror"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
)

// ============================================
// Mock Service
// ============================================

type mockFileService struct {
	uploadFunc          func(ctx context.Context, input client.UploadInput) (domain.File, error)
	downloadFunc        func(ctx context.Context, id string) (io.ReadCloser, domain.File, error)
	listFunc            func(ctx context.Context, page, pageSize int) ([]domain.File, int, error)
	metadataFunc        func(ctx context.Context, id string) (domain.File, error)
	downloadMultipleFunc func(ctx context.Context, ids []string) (io.ReadCloser, error)
}

func (m *mockFileService) Upload(ctx context.Context, input client.UploadInput) (domain.File, error) {
	return m.uploadFunc(ctx, input)
}

func (m *mockFileService) Download(ctx context.Context, id string) (io.ReadCloser, domain.File, error) {
	return m.downloadFunc(ctx, id)
}

func (m *mockFileService) List(ctx context.Context, page, pageSize int) ([]domain.File, int, error) {
	return m.listFunc(ctx, page, pageSize)
}

func (m *mockFileService) Metadata(ctx context.Context, id string) (domain.File, error) {
	return m.metadataFunc(ctx, id)
}

func (m *mockFileService) DownloadMultiple(ctx context.Context, ids []string) (io.ReadCloser, error) {
	return m.downloadMultipleFunc(ctx, ids)
}

// ============================================
// Helpers
// ============================================

func newTestHandler(svc FileService) *FileHandler {
	// maxUploadSize = 100MB for tests
	return NewFileHandler(svc, 100*1024*1024)
}

func assertStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Errorf("status code: got %d, want %d, body: %s", rr.Code, want, rr.Body.String())
	}
}

func assertJSONError(t *testing.T, rr *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v, body: %s", err, rr.Body.String())
	}
	if resp.Error.Code != wantCode {
		t.Errorf("error code: got %q, want %q", resp.Error.Code, wantCode)
	}
}

// ============================================
// Upload Tests
// ============================================

func TestUpload_Success(t *testing.T) {
	svc := &mockFileService{
		uploadFunc: func(ctx context.Context, input client.UploadInput) (domain.File, error) {
			return domain.File{
				ID:          "test-id-123",
				Name:        "test.txt",
				Size:        12,
				ContentType: "text/plain",
				CreatedAt:   time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	h := newTestHandler(svc)

	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	part, _ := writer.CreateFormFile("files", "test.txt")
	part.Write([]byte("hello world"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/files", &b)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	h.Upload(rr, req)

	assertStatus(t, rr, http.StatusCreated)

	// Handler returns {"files": [{"id":"...", ...}]}
	var resp struct {
		Files []domain.File `json:"files"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	if resp.Files[0].ID != "test-id-123" {
		t.Errorf("id: got %q, want %q", resp.Files[0].ID, "test-id-123")
	}
}

func TestUpload_NoFiles(t *testing.T) {
	svc := &mockFileService{}
	h := newTestHandler(svc)

	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/files", &b)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	h.Upload(rr, req)

	assertStatus(t, rr, http.StatusBadRequest)
	assertJSONError(t, rr, "INVALID_REQUEST")
}

func TestUpload_ServiceError(t *testing.T) {
	svc := &mockFileService{
		uploadFunc: func(ctx context.Context, input client.UploadInput) (domain.File, error) {
			return domain.File{}, apperror.Internal("storage failed")
		},
	}
	h := newTestHandler(svc)

	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	part, _ := writer.CreateFormFile("files", "test.txt")
	part.Write([]byte("hello"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/files", &b)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	h.Upload(rr, req)

	assertStatus(t, rr, http.StatusInternalServerError)
	assertJSONError(t, rr, "INTERNAL_ERROR")
}

// ============================================
// Download Tests
// ============================================

func TestDownload_Success(t *testing.T) {
	svc := &mockFileService{
		downloadFunc: func(ctx context.Context, id string) (io.ReadCloser, domain.File, error) {
			return io.NopCloser(strings.NewReader("file content")), domain.File{
				ID:          "abc123",
				Name:        "document.pdf",
				Size:        12,
				ContentType: "application/pdf",
			}, nil
		},
	}
	h := newTestHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/files/abc123/download", nil)
	req.SetPathValue("id", "abc123")
	rr := httptest.NewRecorder()

	h.Download(rr, req)

	assertStatus(t, rr, http.StatusOK)
	if got := rr.Header().Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type: got %q, want %q", got, "application/pdf")
	}
	if got := rr.Header().Get("Content-Disposition"); !strings.Contains(got, "document.pdf") {
		t.Errorf("Content-Disposition missing filename: %q", got)
	}
	if body := rr.Body.String(); body != "file content" {
		t.Errorf("body: got %q, want %q", body, "file content")
	}
}

func TestDownload_NotFound(t *testing.T) {
	svc := &mockFileService{
		downloadFunc: func(ctx context.Context, id string) (io.ReadCloser, domain.File, error) {
			return nil, domain.File{}, apperror.NotFound("file not found")
		},
	}
	h := newTestHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/files/missing/download", nil)
	req.SetPathValue("id", "missing")
	rr := httptest.NewRecorder()

	h.Download(rr, req)

	assertStatus(t, rr, http.StatusNotFound)
	assertJSONError(t, rr, "FILE_NOT_FOUND")
}

func TestDownload_MissingID(t *testing.T) {
	svc := &mockFileService{}
	h := newTestHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/files//download", nil)
	req.SetPathValue("id", "")
	rr := httptest.NewRecorder()

	h.Download(rr, req)

	assertStatus(t, rr, http.StatusBadRequest)
	assertJSONError(t, rr, "INVALID_REQUEST")
}

// ============================================
// List Tests
// ============================================

func TestList_Success(t *testing.T) {
	svc := &mockFileService{
		listFunc: func(ctx context.Context, page, pageSize int) ([]domain.File, int, error) {
			return []domain.File{
				{ID: "1", Name: "a.txt"},
				{ID: "2", Name: "b.txt"},
			}, 2, nil
		},
	}
	h := newTestHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/files?page=1&page_size=10", nil)
	rr := httptest.NewRecorder()

	h.List(rr, req)

	assertStatus(t, rr, http.StatusOK)

	var resp struct {
		Files      []domain.File `json:"files"`
		Pagination struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			Total    int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Files) != 2 {
		t.Errorf("files count: got %d, want 2", len(resp.Files))
	}
	if resp.Pagination.Total != 2 {
		t.Errorf("total: got %d, want 2", resp.Pagination.Total)
	}
}

func TestList_DefaultPagination(t *testing.T) {
	svc := &mockFileService{
		listFunc: func(ctx context.Context, page, pageSize int) ([]domain.File, int, error) {
			if page != 1 {
				t.Errorf("page: got %d, want 1", page)
			}
			if pageSize != 20 {
				t.Errorf("pageSize: got %d, want 20", pageSize)
			}
			return nil, 0, nil
		},
	}
	h := newTestHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/files", nil)
	rr := httptest.NewRecorder()

	h.List(rr, req)
	assertStatus(t, rr, http.StatusOK)
}

// ============================================
// Metadata Tests
// ============================================

func TestMetadata_Success(t *testing.T) {
	svc := &mockFileService{
		metadataFunc: func(ctx context.Context, id string) (domain.File, error) {
			return domain.File{
				ID:          "meta123",
				Name:        "photo.jpg",
				Size:        1024,
				ContentType: "image/jpeg",
				CreatedAt:   time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	h := newTestHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/files/meta123/metadata", nil)
	req.SetPathValue("id", "meta123")
	rr := httptest.NewRecorder()

	h.Metadata(rr, req)

	assertStatus(t, rr, http.StatusOK)

	// Handler returns {"file": {"id":"...", ...}}
	var resp struct {
		File domain.File `json:"file"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.File.ID != "meta123" {
		t.Errorf("id: got %q, want %q", resp.File.ID, "meta123")
	}
}

func TestMetadata_NotFound(t *testing.T) {
	svc := &mockFileService{
		metadataFunc: func(ctx context.Context, id string) (domain.File, error) {
			return domain.File{}, apperror.NotFound("not found")
		},
	}
	h := newTestHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/files/ghost/metadata", nil)
	req.SetPathValue("id", "ghost")
	rr := httptest.NewRecorder()

	h.Metadata(rr, req)

	assertStatus(t, rr, http.StatusNotFound)
	assertJSONError(t, rr, "FILE_NOT_FOUND")
}

// ============================================
// DownloadMultiple Tests
// ============================================

func TestDownloadMultiple_Success(t *testing.T) {
	svc := &mockFileService{
		downloadMultipleFunc: func(ctx context.Context, ids []string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("zip-content")), nil
		},
	}
	h := newTestHandler(svc)

	body := `{"file_ids": ["id1", "id2"]}`
	req := httptest.NewRequest(http.MethodPost, "/files/download", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.DownloadMultiple(rr, req)

	assertStatus(t, rr, http.StatusOK)
	if got := rr.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type: got %q, want %q", got, "application/zip")
	}
	if body := rr.Body.String(); body != "zip-content" {
		t.Errorf("body: got %q, want %q", body, "zip-content")
	}
}

func TestDownloadMultiple_EmptyBody(t *testing.T) {
	svc := &mockFileService{}
	h := newTestHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/files/download", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.DownloadMultiple(rr, req)

	assertStatus(t, rr, http.StatusBadRequest)
	assertJSONError(t, rr, "INVALID_REQUEST")
}

func TestDownloadMultiple_InvalidJSON(t *testing.T) {
	svc := &mockFileService{}
	h := newTestHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/files/download", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.DownloadMultiple(rr, req)

	assertStatus(t, rr, http.StatusBadRequest)
	assertJSONError(t, rr, "INVALID_REQUEST")
}