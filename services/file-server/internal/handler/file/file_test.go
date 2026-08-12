package file

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/apperror"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
)

type uploadCall struct {
	Name        string
	ContentType string
	Size        int64
	Content     []byte
}

type fakeFileService struct {
	// Upload
	uploadCalls []uploadCall
	uploadFunc  func(call uploadCall) (domain.File, error)

	// Download
	downloadCalls []string
	downloadFunc  func(id string) (io.ReadCloser, domain.File, error)

	// List
	listCalls []struct{ Page, PageSize int }
	listFunc  func(page, pageSize int) ([]domain.File, int, error)

	// DownloadMultiple
	downloadMultipleCalls [][]string
	downloadMultipleFunc  func(ids []string) (io.ReadCloser, error)

	// Metadata
	metadataCalls []string
	metadataFunc  func(id string) (domain.File, error)
}

func (f *fakeFileService) Upload(ctx context.Context, input client.UploadInput) (domain.File, error) {
	content, err := io.ReadAll(input.Content)
	if err != nil {
		return domain.File{}, err
	}
	call := uploadCall{
		Name:        input.Name,
		ContentType: input.ContentType,
		Size:        input.Size,
		Content:     content,
	}
	f.uploadCalls = append(f.uploadCalls, call)

	if f.uploadFunc != nil {
		return f.uploadFunc(call)
	}
	return domain.File{
		ID:          fmt.Sprintf("file-%d", len(f.uploadCalls)),
		Name:        call.Name,
		Size:        call.Size,
		ContentType: call.ContentType,
		CreatedAt:   fixedTime,
	}, nil
}

func (f *fakeFileService) Download(ctx context.Context, id string) (io.ReadCloser, domain.File, error) {
	f.downloadCalls = append(f.downloadCalls, id)
	if f.downloadFunc != nil {
		return f.downloadFunc(id)
	}
	return io.NopCloser(strings.NewReader("")), domain.File{}, nil
}

func (f *fakeFileService) List(ctx context.Context, page, pageSize int) ([]domain.File, int, error) {
	f.listCalls = append(f.listCalls, struct{ Page, PageSize int }{page, pageSize})
	if f.listFunc != nil {
		return f.listFunc(page, pageSize)
	}
	return nil, 0, nil
}

func (f *fakeFileService) DownloadMultiple(ctx context.Context, ids []string) (io.ReadCloser, error) {
	f.downloadMultipleCalls = append(f.downloadMultipleCalls, ids)
	if f.downloadMultipleFunc != nil {
		return f.downloadMultipleFunc(ids)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeFileService) Metadata(ctx context.Context, id string) (domain.File, error) {
	f.metadataCalls = append(f.metadataCalls, id)
	if f.metadataFunc != nil {
		return f.metadataFunc(id)
	}
	return domain.File{}, nil
}

var fixedTime = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// Shared test helpers
// ---------------------------------------------------------------------------

const testMaxMultipartMemory = 32 << 20 // matches production default (config.go)

type uploadFileSpec struct {
	fieldFilename string
	contentType   string
	content       []byte
}

// buildUploadRequest builds a multipart/form-data POST request with one
// "files" part per spec (in order), plus any extra non-file form fields.
func buildUploadRequest(t *testing.T, specs []uploadFileSpec, extraFields map[string]string) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	for _, spec := range specs {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename="%s"`, spec.fieldFilename))
		if spec.contentType != "" {
			h.Set("Content-Type", spec.contentType)
		}
		part, err := w.CreatePart(h)
		if err != nil {
			t.Fatalf("create form part for %q: %v", spec.fieldFilename, err)
		}
		if _, err := part.Write(spec.content); err != nil {
			t.Fatalf("write form part content for %q: %v", spec.fieldFilename, err)
		}
	}

	for name, value := range extraFields {
		if err := w.WriteField(name, value); err != nil {
			t.Fatalf("write field %q: %v", name, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/files", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeJSON(t *testing.T, r io.Reader, v any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(v); err != nil {
		t.Fatalf("decode response JSON: %v", err)
	}
}

func decodeErrorBody(t *testing.T, r io.Reader) errorEnvelope {
	t.Helper()
	var env errorEnvelope
	decodeJSON(t, r, &env)
	return env
}

// ---------------------------------------------------------------------------
// Upload
// ---------------------------------------------------------------------------

func TestFileHandler_Upload_SingleFileSuccess(t *testing.T) {
	svc := &fakeFileService{}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	content := []byte("hello world")
	specs := []uploadFileSpec{
		{fieldFilename: "hello.txt", contentType: "text/plain", content: content},
	}
	req := buildUploadRequest(t, specs, nil)
	rec := httptest.NewRecorder()

	h.Upload(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	if len(svc.uploadCalls) != 1 {
		t.Fatalf("service Upload called %d times, want 1", len(svc.uploadCalls))
	}
	call := svc.uploadCalls[0]
	if call.Name != "hello.txt" {
		t.Errorf("Name = %q, want %q", call.Name, "hello.txt")
	}
	if call.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want %q", call.ContentType, "text/plain")
	}
	if call.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", call.Size, len(content))
	}
	if !bytes.Equal(call.Content, content) {
		t.Errorf("Content = %q, want %q", call.Content, content)
	}

	var resp struct {
		Files []domain.File `json:"files"`
	}
	decodeJSON(t, rec.Body, &resp)
	if len(resp.Files) != 1 {
		t.Fatalf("response files = %d, want 1", len(resp.Files))
	}
	want := domain.File{ID: "file-1", Name: "hello.txt", Size: int64(len(content)), ContentType: "text/plain", CreatedAt: fixedTime}
	got := resp.Files[0]
	if got.ID != want.ID || got.Name != want.Name || got.Size != want.Size || got.ContentType != want.ContentType || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("response file = %+v, want %+v", got, want)
	}
}

func TestFileHandler_Upload_MultipleFilesSuccess(t *testing.T) {
	svc := &fakeFileService{}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	specs := []uploadFileSpec{
		{fieldFilename: "a.txt", contentType: "text/plain", content: []byte("content-a")},
		{fieldFilename: "b.png", contentType: "image/png", content: []byte("content-b-bytes")},
		{fieldFilename: "c.json", contentType: "application/json", content: []byte(`{"k":"v"}`)},
	}
	req := buildUploadRequest(t, specs, nil)
	rec := httptest.NewRecorder()

	h.Upload(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	if len(svc.uploadCalls) != len(specs) {
		t.Fatalf("service Upload called %d times, want %d", len(svc.uploadCalls), len(specs))
	}

	// Build a lookup by filename so assertions don't depend on call/response order.
	callsByName := make(map[string]uploadCall, len(svc.uploadCalls))
	for _, c := range svc.uploadCalls {
		callsByName[c.Name] = c
	}
	for _, spec := range specs {
		c, ok := callsByName[spec.fieldFilename]
		if !ok {
			t.Fatalf("no service call recorded for file %q", spec.fieldFilename)
		}
		if c.ContentType != spec.contentType {
			t.Errorf("%s: ContentType = %q, want %q", spec.fieldFilename, c.ContentType, spec.contentType)
		}
		if c.Size != int64(len(spec.content)) {
			t.Errorf("%s: Size = %d, want %d", spec.fieldFilename, c.Size, len(spec.content))
		}
		if !bytes.Equal(c.Content, spec.content) {
			t.Errorf("%s: Content = %q, want %q", spec.fieldFilename, c.Content, spec.content)
		}
	}

	var resp struct {
		Files []domain.File `json:"files"`
	}
	decodeJSON(t, rec.Body, &resp)
	if len(resp.Files) != len(specs) {
		t.Fatalf("response files = %d, want %d", len(resp.Files), len(specs))
	}
	resultNames := make(map[string]bool, len(resp.Files))
	for _, f := range resp.Files {
		resultNames[f.Name] = true
	}
	for _, spec := range specs {
		if !resultNames[spec.fieldFilename] {
			t.Errorf("response missing result for file %q", spec.fieldFilename)
		}
	}
}

func TestFileHandler_Upload_InvalidMultipartRequest(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "no boundary in content-type",
			contentType: "multipart/form-data",
			body:        "irrelevant",
		},
		{
			name:        "not multipart at all",
			contentType: "application/json",
			body:        `{"files":"nope"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeFileService{}
			h := NewFileHandler(svc, testMaxMultipartMemory)

			req := httptest.NewRequest(http.MethodPost, "/files", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()

			h.Upload(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			env := decodeErrorBody(t, rec.Body)
			if env.Error.Code != string(apperror.CodeInvalidRequest) {
				t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeInvalidRequest)
			}
			if len(svc.uploadCalls) != 0 {
				t.Errorf("service Upload called %d times, want 0", len(svc.uploadCalls))
			}
		})
	}
}

func TestFileHandler_Upload_NoFilesField(t *testing.T) {
	svc := &fakeFileService{}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	// Valid multipart form, but no "files" part - only an unrelated field.
	req := buildUploadRequest(t, nil, map[string]string{"note": "no files here"})
	rec := httptest.NewRecorder()

	h.Upload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	env := decodeErrorBody(t, rec.Body)
	if env.Error.Code != string(apperror.CodeInvalidRequest) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeInvalidRequest)
	}
	if len(svc.uploadCalls) != 0 {
		t.Errorf("service Upload called %d times, want 0", len(svc.uploadCalls))
	}
}

func TestFileHandler_Upload_ServiceError(t *testing.T) {
	tests := []struct {
		name       string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "app error propagates its own status/code",
			svcErr:     apperror.TooLarge("file size exceeds the maximum limit"),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   string(apperror.CodeFileTooLarge),
		},
		{
			name:       "generic error maps to internal error via httpx.WriteError",
			svcErr:     errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   string(apperror.CodeInternalError),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeFileService{
				uploadFunc: func(call uploadCall) (domain.File, error) {
					return domain.File{}, tt.svcErr
				},
			}
			h := NewFileHandler(svc, testMaxMultipartMemory)

			specs := []uploadFileSpec{{fieldFilename: "f.txt", contentType: "text/plain", content: []byte("x")}}
			req := buildUploadRequest(t, specs, nil)
			rec := httptest.NewRecorder()

			h.Upload(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			env := decodeErrorBody(t, rec.Body)
			if env.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", env.Error.Code, tt.wantCode)
			}
			if len(svc.uploadCalls) != 1 {
				t.Errorf("service Upload called %d times, want 1", len(svc.uploadCalls))
			}
		})
	}
}

// File-open failure is intentionally NOT tested.
//
// The handler calls fh.Open() on a *multipart.FileHeader produced by
// r.ParseMultipartForm. That FileHeader either wraps an in-memory buffer or
// a *os.File spilled to a temp directory once the multipart body exceeds
// maxMultipartMemory - in both cases Open() reads back data ParseMultipartForm
// already wrote moments earlier, on the same request. There's no supported
// way to make it fail from a black-box test: we don't own file creation
// (net/http/internal does), and forcing a failure would mean either faking
// out mime/multipart internals or making the filesystem/temp-dir unwritable
// for the whole process, which is heavy, flaky, and not worth the coverage
// gained for a single defensive `if err != nil` branch. Skipping per the
// instructions rather than contorting the production code or the test.

// ---------------------------------------------------------------------------
// Download
// ---------------------------------------------------------------------------

func TestFileHandler_Download_Success(t *testing.T) {
	body := "file body bytes"
	file := domain.File{ID: "abc-123", Name: "report.pdf", Size: int64(len(body)), ContentType: "application/pdf", CreatedAt: fixedTime}

	svc := &fakeFileService{
		downloadFunc: func(id string) (io.ReadCloser, domain.File, error) {
			return io.NopCloser(strings.NewReader(body)), file, nil
		},
	}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodGet, "/files/abc-123/download", nil)
	req.SetPathValue("id", "abc-123")
	rec := httptest.NewRecorder()

	h.Download(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type = %q, want %q", got, "application/pdf")
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="report.pdf"` {
		t.Errorf("Content-Disposition = %q, want %q", got, `attachment; filename="report.pdf"`)
	}
	if got := rec.Header().Get("Content-Length"); got != fmt.Sprintf("%d", len(body)) {
		t.Errorf("Content-Length = %q, want %q", got, fmt.Sprintf("%d", len(body)))
	}
	if got := rec.Body.String(); got != body {
		t.Errorf("body = %q, want %q", got, body)
	}

	if len(svc.downloadCalls) != 1 || svc.downloadCalls[0] != "abc-123" {
		t.Errorf("downloadCalls = %v, want [\"abc-123\"]", svc.downloadCalls)
	}
}

func TestFileHandler_Download_MissingID(t *testing.T) {
	svc := &fakeFileService{}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	// No SetPathValue call: r.PathValue("id") naturally returns "" when the
	// request wasn't routed through a mux pattern, simulating a missing ID.
	req := httptest.NewRequest(http.MethodGet, "/files//download", nil)
	rec := httptest.NewRecorder()

	h.Download(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	env := decodeErrorBody(t, rec.Body)
	if env.Error.Code != string(apperror.CodeInvalidRequest) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeInvalidRequest)
	}
	if len(svc.downloadCalls) != 0 {
		t.Errorf("service Download called %d times, want 0", len(svc.downloadCalls))
	}
}

func TestFileHandler_Download_ServiceError(t *testing.T) {
	svc := &fakeFileService{
		downloadFunc: func(id string) (io.ReadCloser, domain.File, error) {
			return nil, domain.File{}, apperror.NotFound("file not found")
		},
	}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodGet, "/files/missing/download", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()

	h.Download(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	env := decodeErrorBody(t, rec.Body)
	if env.Error.Code != string(apperror.CodeFileNotFound) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeFileNotFound)
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestFileHandler_List_PageAndPageSize(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		wantPage     int
		wantPageSize int
	}{
		{name: "missing params default to page=1 pageSize=20", query: "", wantPage: 1, wantPageSize: 20},
		{name: "non-numeric params default to page=1 pageSize=20", query: "?page=abc&pageSize=xyz", wantPage: 1, wantPageSize: 20},
		{name: "negative params default to page=1 pageSize=20", query: "?page=-5&pageSize=-10", wantPage: 1, wantPageSize: 20},
		{name: "custom valid params are passed through", query: "?page=3&pageSize=50", wantPage: 3, wantPageSize: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeFileService{}
			h := NewFileHandler(svc, testMaxMultipartMemory)

			req := httptest.NewRequest(http.MethodGet, "/files"+tt.query, nil)
			rec := httptest.NewRecorder()

			h.List(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if len(svc.listCalls) != 1 {
				t.Fatalf("service List called %d times, want 1", len(svc.listCalls))
			}
			got := svc.listCalls[0]
			if got.Page != tt.wantPage || got.PageSize != tt.wantPageSize {
				t.Errorf("List called with (page=%d, pageSize=%d), want (page=%d, pageSize=%d)", got.Page, got.PageSize, tt.wantPage, tt.wantPageSize)
			}
		})
	}
}

func TestFileHandler_List_Success(t *testing.T) {
	files := []domain.File{
		{ID: "1", Name: "a.txt", Size: 10, ContentType: "text/plain", CreatedAt: fixedTime},
		{ID: "2", Name: "b.txt", Size: 20, ContentType: "text/plain", CreatedAt: fixedTime},
	}
	svc := &fakeFileService{
		listFunc: func(page, pageSize int) ([]domain.File, int, error) {
			return files, 2, nil
		},
	}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodGet, "/files?page=1&pageSize=20", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Files      []domain.File `json:"files"`
		Pagination struct {
			Page     int `json:"page"`
			PageSize int `json:"pageSize"`
			Total    int `json:"total"`
		} `json:"pagination"`
	}
	decodeJSON(t, rec.Body, &resp)

	if len(resp.Files) != len(files) {
		t.Fatalf("response files = %d, want %d", len(resp.Files), len(files))
	}
	if resp.Pagination.Page != 1 || resp.Pagination.PageSize != 20 || resp.Pagination.Total != 2 {
		t.Errorf("pagination = %+v, want page=1 pageSize=20 total=2", resp.Pagination)
	}
}

func TestFileHandler_List_ServiceError(t *testing.T) {
	svc := &fakeFileService{
		listFunc: func(page, pageSize int) ([]domain.File, int, error) {
			return nil, 0, apperror.Internal("db unavailable")
		},
	}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodGet, "/files", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	env := decodeErrorBody(t, rec.Body)
	if env.Error.Code != string(apperror.CodeInternalError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeInternalError)
	}
}

// ---------------------------------------------------------------------------
// DownloadMultiple
// ---------------------------------------------------------------------------

func TestFileHandler_DownloadMultiple_Success(t *testing.T) {
	archiveBody := "zip archive bytes"
	svc := &fakeFileService{
		downloadMultipleFunc: func(ids []string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(archiveBody)), nil
		},
	}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	reqBody := `{"file_ids":["id-1","id-2","id-3"]}`
	req := httptest.NewRequest(http.MethodPost, "/files/download-many", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.DownloadMultiple(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q, want %q", got, "application/zip")
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="files.zip"` {
		t.Errorf("Content-Disposition = %q, want %q", got, `attachment; filename="files.zip"`)
	}
	if got := rec.Body.String(); got != archiveBody {
		t.Errorf("body = %q, want %q", got, archiveBody)
	}

	if len(svc.downloadMultipleCalls) != 1 {
		t.Fatalf("service DownloadMultiple called %d times, want 1", len(svc.downloadMultipleCalls))
	}
	wantIDs := []string{"id-1", "id-2", "id-3"}
	gotIDs := svc.downloadMultipleCalls[0]
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("ids = %v, want %v", gotIDs, wantIDs)
	}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Errorf("ids[%d] = %q, want %q", i, gotIDs[i], wantIDs[i])
		}
	}
}

func TestFileHandler_DownloadMultiple_InvalidJSON(t *testing.T) {
	svc := &fakeFileService{}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodPost, "/files/download-many", strings.NewReader(`{"file_ids": not-json`))
	rec := httptest.NewRecorder()

	h.DownloadMultiple(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	env := decodeErrorBody(t, rec.Body)
	if env.Error.Code != string(apperror.CodeInvalidRequest) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeInvalidRequest)
	}
	if len(svc.downloadMultipleCalls) != 0 {
		t.Errorf("service DownloadMultiple called %d times, want 0", len(svc.downloadMultipleCalls))
	}
}

func TestFileHandler_DownloadMultiple_EmptyFileIDs(t *testing.T) {
	svc := &fakeFileService{}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodPost, "/files/download-many", strings.NewReader(`{"file_ids":[]}`))
	rec := httptest.NewRecorder()

	h.DownloadMultiple(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	env := decodeErrorBody(t, rec.Body)
	if env.Error.Code != string(apperror.CodeInvalidRequest) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeInvalidRequest)
	}
	if len(svc.downloadMultipleCalls) != 0 {
		t.Errorf("service DownloadMultiple called %d times, want 0", len(svc.downloadMultipleCalls))
	}
}

func TestFileHandler_DownloadMultiple_ServiceError(t *testing.T) {
	svc := &fakeFileService{
		downloadMultipleFunc: func(ids []string) (io.ReadCloser, error) {
			return nil, apperror.NotFound("one or more files not found")
		},
	}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodPost, "/files/download-many", strings.NewReader(`{"file_ids":["missing"]}`))
	rec := httptest.NewRecorder()

	h.DownloadMultiple(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	env := decodeErrorBody(t, rec.Body)
	if env.Error.Code != string(apperror.CodeFileNotFound) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeFileNotFound)
	}
}

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func TestFileHandler_Metadata_Success(t *testing.T) {
	file := domain.File{ID: "abc-123", Name: "report.pdf", Size: 1024, ContentType: "application/pdf", CreatedAt: fixedTime}
	svc := &fakeFileService{
		metadataFunc: func(id string) (domain.File, error) {
			return file, nil
		},
	}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodGet, "/files/abc-123/metadata", nil)
	req.SetPathValue("id", "abc-123")
	rec := httptest.NewRecorder()

	h.Metadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		File domain.File `json:"file"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.File.ID != file.ID || resp.File.Name != file.Name || resp.File.Size != file.Size || resp.File.ContentType != file.ContentType {
		t.Errorf("response file = %+v, want %+v", resp.File, file)
	}

	if len(svc.metadataCalls) != 1 || svc.metadataCalls[0] != "abc-123" {
		t.Errorf("metadataCalls = %v, want [\"abc-123\"]", svc.metadataCalls)
	}
}

func TestFileHandler_Metadata_MissingID(t *testing.T) {
	svc := &fakeFileService{}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodGet, "/files//metadata", nil)
	rec := httptest.NewRecorder()

	h.Metadata(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	env := decodeErrorBody(t, rec.Body)
	if env.Error.Code != string(apperror.CodeInvalidRequest) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeInvalidRequest)
	}
	if len(svc.metadataCalls) != 0 {
		t.Errorf("service Metadata called %d times, want 0", len(svc.metadataCalls))
	}
}

func TestFileHandler_Metadata_ServiceError(t *testing.T) {
	svc := &fakeFileService{
		metadataFunc: func(id string) (domain.File, error) {
			return domain.File{}, apperror.NotFound("file not found")
		},
	}
	h := NewFileHandler(svc, testMaxMultipartMemory)

	req := httptest.NewRequest(http.MethodGet, "/files/missing/metadata", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()

	h.Metadata(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	env := decodeErrorBody(t, rec.Body)
	if env.Error.Code != string(apperror.CodeFileNotFound) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apperror.CodeFileNotFound)
	}
}