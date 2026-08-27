package file

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/apperror"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/config"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/httpx"
)

type FileService interface {
	Upload(ctx context.Context, input client.UploadInput) (domain.File, error)
	Download(ctx context.Context, id string) (io.ReadCloser, domain.File, error)
	List(ctx context.Context, page, pageSize int) ([]domain.File, int, error)
	Metadata(ctx context.Context, id string) (domain.File, error)
	StartArchiveWorkflow(ctx context.Context, ids []string) (archiveID string, wsEndpoint string, err error)
}

type FileHandler struct {
	service            FileService
	cfg                *config.Config
	maxMultipartMemory int64
}

func NewFileHandler(fileService FileService, cfg *config.Config, maxMultipartMemory int64) *FileHandler {
	return &FileHandler{
		service:            fileService,
		cfg:                cfg,
		maxMultipartMemory: maxMultipartMemory,
	}
}

type downloadMultipleRequest struct {
	IDs []string `json:"file_ids"`
}

func (h *FileHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(h.maxMultipartMemory); err != nil {
		httpx.WriteError(w, apperror.Invalid("invalid multipart form"))
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		httpx.WriteError(w, apperror.Invalid("no files provided"))
		return
	}

	var results []domain.File
	for _, fh := range files {
		file, err := fh.Open()
		if err != nil {
			httpx.WriteError(w, apperror.Internal("failed to open upload"))
			return
		}

		result, err := h.service.Upload(r.Context(), client.UploadInput{
			Name:        fh.Filename,
			ContentType: fh.Header.Get("Content-Type"),
			Size:        fh.Size,
			Content:     file,
		})
		file.Close()

		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		results = append(results, result)
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"files": results})
}

func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httpx.WriteError(w, apperror.Invalid("missing file ID"))
		return
	}

	reader, file, err := h.service.Download(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+file.Name+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))

	if _, err := io.Copy(w, reader); err != nil {
		log.Printf("download copy error: %v", err)
		return
	}
}

func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	files, total, err := h.service.List(r.Context(), page, pageSize)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"files": files,
		"pagination": map[string]any{
			"page":     page,
			"pageSize": pageSize,
			"total":    total,
		},
	})
}

func (h *FileHandler) DownloadMultiple(w http.ResponseWriter, r *http.Request) {
	var req downloadMultipleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperror.Invalid("invalid JSON body"))
		return
	}
	if len(req.IDs) == 0 {
		httpx.WriteError(w, apperror.Invalid("file_ids cannot be empty"))
		return
	}

	archiveID, wsEndpoint, err := h.service.StartArchiveWorkflow(r.Context(), req.IDs)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"archive_id":  archiveID,
		"ws_endpoint": wsEndpoint,
	})
}

func (h *FileHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httpx.WriteError(w, apperror.Invalid("missing file ID"))
		return
	}

	file, err := h.service.Metadata(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"file": file})
}
