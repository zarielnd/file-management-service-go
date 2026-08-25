package file

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/config"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/mocks"
	"go.uber.org/mock/gomock"
)

// --- Upload: separate t.Run because multipart setup is complex ---

func TestFileHandler_Upload(t *testing.T) {
	t.Run("success single file", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockSvc := mocks.NewMockFileService(ctrl)
		h := NewFileHandler(mockSvc, &config.Config{UseTemporalArchive: false}, 32<<20)

		mockSvc.EXPECT().Upload(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, in client.UploadInput) (domain.File, error) {
				content, err := io.ReadAll(in.Content)
				if err != nil {
					t.Fatal(err)
				}
				if in.Name != "test.txt" {
					t.Errorf("filename = %s, want test.txt", in.Name)
				}
				if string(content) != "hello" {
					t.Errorf("content = %q, want %q", content, "hello")
				}

				if in.ContentType != "text/plain" {
					t.Errorf("content type = %q, want %q", in.ContentType, "text/plain")
				}

				if in.Size != 5 {
					t.Errorf("size = %d, want 5", in.Size)
				}
				return domain.File{ID: "123", Name: "test.txt"}, nil
			},
		).Times(1)

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		header := make(textproto.MIMEHeader)
		header.Set(
			"Content-Disposition",
			`form-data; name="files"; filename="test.txt"`,
		)
		header.Set("Content-Type", "text/plain")

		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}

		_, err = io.WriteString(part, "hello")
		if err != nil {
			t.Fatal(err)
		}
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/upload", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rr := httptest.NewRecorder()

		h.Upload(rr, req)

		if rr.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Files []domain.File `json:"files"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Files) != 1 {
			t.Fatalf("files count = %d, want 1", len(resp.Files))
		}
		if resp.Files[0].ID != "123" {
			t.Errorf("id = %s, want 123", resp.Files[0].ID)
		}
	})

	t.Run("no files", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockSvc := mocks.NewMockFileService(ctrl)
		h := NewFileHandler(mockSvc, &config.Config{UseTemporalArchive: false}, 32<<20)

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/upload", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rr := httptest.NewRecorder()

		h.Upload(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})

	t.Run("service error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockSvc := mocks.NewMockFileService(ctrl)
		h := NewFileHandler(mockSvc, &config.Config{UseTemporalArchive: false}, 32<<20)

		mockSvc.EXPECT().Upload(gomock.Any(), gomock.Any()).Return(
			domain.File{}, errors.New("storage fail"),
		).Times(1)

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("files", "fail.txt")
		io.WriteString(part, "x")
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/upload", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rr := httptest.NewRecorder()

		h.Upload(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
		}
	})
}

// --- Download: separate t.Run because io.ReadCloser setup is complex ---

func TestFileHandler_Download(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockSvc := mocks.NewMockFileService(ctrl)
		h := NewFileHandler(mockSvc, &config.Config{UseTemporalArchive: false}, 32<<20)

		mockSvc.EXPECT().Download(gomock.Any(), "abc").Return(
			io.NopCloser(strings.NewReader("file content")),
			domain.File{
				ID: "abc", Name: "doc.txt",
				ContentType: "text/plain", Size: 12,
			},
			nil,
		)

		req := httptest.NewRequest(http.MethodGet, "/files/abc", nil)
		req.SetPathValue("id", "abc")
		rr := httptest.NewRecorder()

		h.Download(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
		}
		if ct := rr.Header().Get("Content-Type"); ct != "text/plain" {
			t.Errorf("Content-Type = %s, want text/plain", ct)
		}
		if got := rr.Header().Get("Content-Length"); got != "12" {
			t.Errorf("Content-Length = %s, want 12", got)
		}
		if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, "doc.txt") {
			t.Errorf("Content-Disposition = %s, want contain doc.txt", cd)
		}
		if got := rr.Body.String(); got != "file content" {
			t.Errorf("body = %q, want %q", got, "file content")
		}
	})

	t.Run("missing id", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockSvc := mocks.NewMockFileService(ctrl)
		h := NewFileHandler(mockSvc, &config.Config{UseTemporalArchive: false}, 32<<20)

		req := httptest.NewRequest(http.MethodGet, "/files/", nil)
		req.SetPathValue("id", "")
		rr := httptest.NewRecorder()

		h.Download(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})

	t.Run("service error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockSvc := mocks.NewMockFileService(ctrl)
		h := NewFileHandler(mockSvc, &config.Config{UseTemporalArchive: false}, 32<<20)

		mockSvc.EXPECT().Download(gomock.Any(), "missing").Return(
			nil, domain.File{}, errors.New("not found"),
		)

		req := httptest.NewRequest(http.MethodGet, "/files/missing", nil)
		req.SetPathValue("id", "missing")
		rr := httptest.NewRecorder()

		h.Download(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
		}
	})
}

// --- List: table-driven ---

func TestFileHandler_List(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		mockSetup func(*mocks.MockFileService)
		wantCode  int
		wantPage  int
		wantSize  int
		wantTotal int
	}{
		{
			name:  "default pagination",
			query: "",
			mockSetup: func(m *mocks.MockFileService) {
				m.EXPECT().List(gomock.Any(), 1, 20).Return(
					[]domain.File{{ID: "1", Name: "a.txt"}}, 1, nil,
				)
			},
			wantCode:  http.StatusOK,
			wantPage:  1,
			wantSize:  20,
			wantTotal: 1,
		},
		{
			name:  "custom pagination",
			query: "?page=3&pageSize=50",
			mockSetup: func(m *mocks.MockFileService) {
				m.EXPECT().List(gomock.Any(), 3, 50).Return(
					[]domain.File{}, 100, nil,
				)
			},
			wantCode:  http.StatusOK,
			wantPage:  3,
			wantSize:  50,
			wantTotal: 100,
		},
		{
			name:  "service error",
			query: "",
			mockSetup: func(m *mocks.MockFileService) {
				m.EXPECT().List(gomock.Any(), 1, 20).Return(
					nil, 0, errors.New("db down"),
				)
			},
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockSvc := mocks.NewMockFileService(ctrl)
			h := NewFileHandler(mockSvc, &config.Config{UseTemporalArchive: false}, 32<<20)

			tt.mockSetup(mockSvc)

			req := httptest.NewRequest(http.MethodGet, "/files"+tt.query, nil)
			rr := httptest.NewRecorder()

			h.List(rr, req)

			if rr.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rr.Code, tt.wantCode)
			}

			if tt.wantCode != http.StatusOK {
				return
			}

			var resp struct {
				Files      []domain.File `json:"files"`
				Pagination struct {
					Page     int `json:"page"`
					PageSize int `json:"pageSize"`
					Total    int `json:"total"`
				} `json:"pagination"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Pagination.Page != tt.wantPage {
				t.Errorf("page = %d, want %d", resp.Pagination.Page, tt.wantPage)
			}
			if resp.Pagination.PageSize != tt.wantSize {
				t.Errorf("pageSize = %d, want %d", resp.Pagination.PageSize, tt.wantSize)
			}
			if resp.Pagination.Total != tt.wantTotal {
				t.Errorf("total = %d, want %d", resp.Pagination.Total, tt.wantTotal)
			}
		})
	}
}

// --- DownloadMultiple: table-driven ---

func TestFileHandler_DownloadMultiple(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		mockSetup func(*mocks.MockFileService)
		wantCode  int
		wantCT    string
		wantCD    string
		wantBody  string
	}{
		{
			name: "success",
			body: `{"file_ids":["1","2"]}`,
			mockSetup: func(m *mocks.MockFileService) {
				m.EXPECT().
					DownloadMultiple(gomock.Any(), []string{"1", "2"}).
					Return(
						io.NopCloser(strings.NewReader("zipdata")),
						nil,
					)
			},
			wantCode: http.StatusOK,
			wantCT:   "application/zip",
			wantCD:   `attachment; filename="files.zip"`,
			wantBody: "zipdata",
		},
		{
			name:     "empty ids",
			body:     `{"file_ids":[]}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid json",
			body:     `not-json`,
			wantCode: http.StatusBadRequest,
		},
		{
			name: "service error",
			body: `{"file_ids":["1"]}`,
			mockSetup: func(m *mocks.MockFileService) {
				m.EXPECT().
					DownloadMultiple(gomock.Any(), []string{"1"}).
					Return(
						nil,
						errors.New("archive fail"),
					)
			},
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockSvc := mocks.NewMockFileService(ctrl)

			h := NewFileHandler(mockSvc, &config.Config{UseTemporalArchive: false}, 32<<20)

			if tt.mockSetup != nil {
				tt.mockSetup(mockSvc)
			}

			req := httptest.NewRequest(
				http.MethodPost,
				"/download",
				strings.NewReader(tt.body),
			)
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()

			// Act
			h.DownloadMultiple(rr, req)

			// Assert status
			if rr.Code != tt.wantCode {
				t.Fatalf(
					"status = %d, want %d",
					rr.Code,
					tt.wantCode,
				)
			}

			// Error cases don't need to check successful response headers/body.
			if tt.wantCode != http.StatusOK {
				return
			}

			// Assert Content-Type
			if got := rr.Header().Get("Content-Type"); got != tt.wantCT {
				t.Errorf(
					"Content-Type = %q, want %q",
					got,
					tt.wantCT,
				)
			}

			// Assert Content-Disposition
			if got := rr.Header().Get("Content-Disposition"); got != tt.wantCD {
				t.Errorf(
					"Content-Disposition = %q, want %q",
					got,
					tt.wantCD,
				)
			}

			// Assert response body
			if got := rr.Body.String(); got != tt.wantBody {
				t.Errorf(
					"body = %q, want %q",
					got,
					tt.wantBody,
				)
			}
		})
	}
}

// --- Metadata: table-driven ---

func TestFileHandler_Metadata(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		mockSetup func(*mocks.MockFileService)
		wantCode  int
		wantID    string
	}{
		{
			name: "success",
			id:   "abc",
			mockSetup: func(m *mocks.MockFileService) {
				m.EXPECT().Metadata(gomock.Any(), "abc").Return(
					domain.File{ID: "abc", Name: "x.txt", Size: 100}, nil,
				)
			},
			wantCode: http.StatusOK,
			wantID:   "abc",
		},
		{
			name:     "missing id",
			id:       "",
			wantCode: http.StatusBadRequest,
		},
		{
			name: "service error",
			id:   "missing",
			mockSetup: func(m *mocks.MockFileService) {
				m.EXPECT().Metadata(gomock.Any(), "missing").Return(
					domain.File{}, errors.New("not found"),
				)
			},
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockSvc := mocks.NewMockFileService(ctrl)
			h := NewFileHandler(mockSvc, &config.Config{UseTemporalArchive: false}, 32<<20)

			if tt.mockSetup != nil {
				tt.mockSetup(mockSvc)
			}

			req := httptest.NewRequest(http.MethodGet, "/files/"+tt.id, nil)
			req.SetPathValue("id", tt.id)
			rr := httptest.NewRecorder()

			h.Metadata(rr, req)

			if rr.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rr.Code, tt.wantCode)
			}

			if tt.wantID == "" {
				return
			}

			var resp struct {
				File domain.File `json:"file"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp.File.ID != tt.wantID {
				t.Errorf("id = %s, want %s", resp.File.ID, tt.wantID)
			}
		})
	}
}
