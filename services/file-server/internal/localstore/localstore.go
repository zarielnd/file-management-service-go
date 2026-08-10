package localstore

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/apperror"
	service "github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
)

type entry struct {
	file domain.File
	content []byte
}

type Store struct {
	mu sync.RWMutex
	entries map[string]entry
}

func NewStore() *Store {
	return &Store{
		entries: make(map[string]entry),
	}
}

var _ service.StorageClient = (*Store)(nil)

func (s *Store) Store(ctx context.Context, input service.UploadInput) (domain.File, error) {
	content, err := io.ReadAll(input.Content)
	if err != nil {
		return domain.File{}, apperror.Internal("failed to read upload stream")
	}

	file := domain.File{
		ID: newID(),
		Name: input.Name,
		Size: int64(len(content)),
		ContentType: input.ContentType,
		CreatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	s.entries[file.ID] = entry{
		file: file,
		content: content,
	}
	s.mu.Unlock()

	return file, nil
}

func (s *Store) Fetch(ctx context.Context, id string) (io.ReadCloser, domain.File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[id]
	if !ok {
		return nil, domain.File{}, apperror.NotFound("file not found")
	}

	return io.NopCloser(strings.NewReader(string(entry.content))), entry.file, nil
}

func (s *Store) Metadata(ctx context.Context, id string) (domain.File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[id]
	if !ok {
		return domain.File{}, apperror.NotFound("file not found")
	}

	return entry.file, nil
}

func (s *Store) List(ctx context.Context, page, pageSize int) ([]domain.File, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files := make([]domain.File, 0, len(s.entries))
	for _, entry := range s.entries {
		files = append(files, entry.file)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].CreatedAt.Before(files[j].CreatedAt)
	})

	total := len(files)
	start := (page - 1) * pageSize
	if start >= total {
		return []domain.File{}, total, nil
	}

	end := start + pageSize
	if end > total {
		end = total
	}

	return files[start:end], total, nil
}

func (s *Store) DownloadArchive(ctx context.Context, ids []string) (io.ReadCloser, error) {
    pr, pw := io.Pipe()
    go func() {
        defer pw.Close()
        zw := zip.NewWriter(pw)
        defer zw.Close()

        for _, id := range ids {
            entry, ok := s.entries[id]
            if !ok {
                pw.CloseWithError(apperror.NotFound("file not found"))
                return
            }
            f, err := zw.Create(entry.file.Name)
            if err != nil {
                pw.CloseWithError(err)
                return
            }
            if _, err := f.Write(entry.content); err != nil {
                pw.CloseWithError(err)
                return
            }
        }
    }()
    return pr, nil
}

func newID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return strings.ToUpper("01j" + hex.EncodeToString(buf))
}
