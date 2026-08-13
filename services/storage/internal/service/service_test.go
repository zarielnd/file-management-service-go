package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/repository"
	repomock "github.com/zarielnd/file-management-service-go/services/storage/internal/repository/mock"
	storagemock "github.com/zarielnd/file-management-service-go/services/storage/internal/storage/mock"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// newService wires a FileService against fakes, using the test's own
// t.TempDir() as both baseDir and tempDir. tempDir is real: DownloadArchive
// legitimately stages a zip file on local disk before returning it (that's
// the production design, not a mock boundary), so tests exercise that with a
// throwaway directory rather than faking os.CreateTemp/os.Open.
func newService(t *testing.T, repo *repomock.Repository, storage *storagemock.Provider) *FileService {
	t.Helper()
	dir := t.TempDir()
	return NewFileService(repo, storage, dir, dir)
}

// archiveFilesRemaining reports how many "archive-*.zip" temp files are
// currently sitting in dir, to assert DownloadArchive cleans up after itself
// (both on success, once the returned reader is closed, and on every error
// path, immediately).
func archiveFilesRemaining(t *testing.T, dir string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "archive-*.zip"))
	if err != nil {
		t.Fatalf("glob temp dir: %v", err)
	}
	return len(matches)
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

func TestFileService_Store_Success(t *testing.T) {
	repo := &repomock.Repository{}
	storage := &storagemock.Provider{}
	svc := newService(t, repo, storage)

	content := []byte("hello world, this is file content")
	before := time.Now().UTC()

	got, err := svc.Store(context.Background(), "hello.txt", "text/plain", bytes.NewReader(content))

	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("Store() error = %v, want nil", err)
	}

	// ID must be a valid, freshly generated UUIDv7.
	parsedID, err := uuid.Parse(got.ID)
	if err != nil {
		t.Fatalf("Store() ID = %q is not a valid UUID: %v", got.ID, err)
	}
	if parsedID.Version() != 7 {
		t.Errorf("Store() ID version = %d, want 7 (uuid.NewV7)", parsedID.Version())
	}

	if got.Name != "hello.txt" {
		t.Errorf("Name = %q, want %q", got.Name, "hello.txt")
	}
	if got.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want %q", got.ContentType, "text/plain")
	}
	if got.SizeBytes != int64(len(content)) {
		t.Errorf("SizeBytes = %d, want %d", got.SizeBytes, len(content))
	}

	wantChecksum := sha256Hex(content)
	if got.Checksum != wantChecksum {
		t.Errorf("Checksum = %q, want %q", got.Checksum, wantChecksum)
	}

	wantPath := filepath.Join(got.ID[:2], got.ID[2:4], got.ID, "hello.txt")
	if got.StoragePath != wantPath {
		t.Errorf("StoragePath = %q, want %q", got.StoragePath, wantPath)
	}

	if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want between %v and %v", got.CreatedAt, before, after)
	}
	if got.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", got.CreatedAt.Location())
	}

	// storage.Provider received the exact bytes at the sharded path.
	if len(storage.StoredPaths) != 1 || storage.StoredPaths[0] != wantPath {
		t.Fatalf("storage.Store paths = %v, want [%q]", storage.StoredPaths, wantPath)
	}
	if !bytes.Equal(storage.StoredContent[wantPath], content) {
		t.Errorf("storage.Store content = %q, want %q", storage.StoredContent[wantPath], content)
	}

	// repo.Create received the same file that was returned.
	if repo.CreatedFile == nil {
		t.Fatalf("repo.Create was not called")
	}
	if *repo.CreatedFile != got {
		t.Errorf("repo.Create received %+v, want %+v", *repo.CreatedFile, got)
	}
}

func TestFileService_Store_StorageError(t *testing.T) {
	repo := &repomock.Repository{}
	storage := &storagemock.Provider{StoreErr: errors.New("disk full")}
	svc := newService(t, repo, storage)

	_, err := svc.Store(context.Background(), "f.txt", "text/plain", bytes.NewReader([]byte("x")))

	if err == nil {
		t.Fatal("Store() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to store file in storage") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "failed to store file in storage")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error = %q, want it to wrap the underlying %q", err.Error(), "disk full")
	}
	if repo.CreatedFile != nil {
		t.Errorf("repo.Create was called, want not called")
	}
}

func TestFileService_Store_RepositoryError(t *testing.T) {
	repo := &repomock.Repository{Err: errors.New("unique violation")}
	storage := &storagemock.Provider{}
	svc := newService(t, repo, storage)

	got, err := svc.Store(context.Background(), "f.txt", "text/plain", bytes.NewReader([]byte("x")))

	if err == nil {
		t.Fatal("Store() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to store file metadata in repository") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "failed to store file metadata in repository")
	}
	if !strings.Contains(err.Error(), "unique violation") {
		t.Errorf("error = %q, want it to wrap the underlying %q", err.Error(), "unique violation")
	}
	if got != (repository.File{}) {
		t.Errorf("Store() file = %+v, want zero value", got)
	}
	// The underlying storage write is not rolled back on a repo failure -
	// documenting current behavior, not asserting it's desirable.
	if len(storage.StoredPaths) != 1 {
		t.Errorf("storage.Store called %d times, want 1 (write happens before repo.Create)", len(storage.StoredPaths))
	}
}

// ---------------------------------------------------------------------------
// Fetch
// ---------------------------------------------------------------------------

func TestFileService_Fetch_Success(t *testing.T) {
	want := repository.File{ID: "abc-123", Name: "report.pdf", StoragePath: "ab/c-/abc-123/report.pdf", ContentType: "application/pdf", SizeBytes: 9}
	body := "pdf bytes"

	repo := &repomock.Repository{File: &want}
	storage := &storagemock.Provider{Content: map[string]string{want.StoragePath: body}}
	svc := newService(t, repo, storage)

	reader, got, err := svc.Fetch(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("Fetch() error = %v, want nil", err)
	}
	defer reader.Close()

	if got != want {
		t.Errorf("Fetch() file = %+v, want %+v", got, want)
	}
	gotBody, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read fetched reader: %v", err)
	}
	if string(gotBody) != body {
		t.Errorf("Fetch() body = %q, want %q", gotBody, body)
	}

	if repo.GetByIDArg != "abc-123" {
		t.Errorf("repo.GetByID arg = %q, want %q", repo.GetByIDArg, "abc-123")
	}
	if len(storage.FetchedPaths) != 1 || storage.FetchedPaths[0] != want.StoragePath {
		t.Errorf("storage.Fetch calls = %v, want [%q]", storage.FetchedPaths, want.StoragePath)
	}
}

func TestFileService_Fetch_RepositoryError(t *testing.T) {
	repo := &repomock.Repository{Err: errors.New("not found")}
	storage := &storagemock.Provider{}
	svc := newService(t, repo, storage)

	reader, got, err := svc.Fetch(context.Background(), "missing")

	if err == nil {
		t.Fatal("Fetch() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to fetch file metadata") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "failed to fetch file metadata")
	}
	if reader != nil {
		t.Errorf("Fetch() reader = %v, want nil", reader)
	}
	if got != (repository.File{}) {
		t.Errorf("Fetch() file = %+v, want zero value", got)
	}
	if len(storage.FetchedPaths) != 0 {
		t.Errorf("storage.Fetch called %d times, want 0", len(storage.FetchedPaths))
	}
}

func TestFileService_Fetch_StorageError(t *testing.T) {
	repo := &repomock.Repository{File: &repository.File{ID: "abc-123", StoragePath: "some/path"}}
	storage := &storagemock.Provider{FetchErr: errors.New("object not found")}
	svc := newService(t, repo, storage)

	reader, got, err := svc.Fetch(context.Background(), "abc-123")

	if err == nil {
		t.Fatal("Fetch() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to fetch file from storage") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "failed to fetch file from storage")
	}
	if reader != nil {
		t.Errorf("Fetch() reader = %v, want nil", reader)
	}
	if got != (repository.File{}) {
		t.Errorf("Fetch() file = %+v, want zero value", got)
	}
}

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func TestFileService_Metadata_Success(t *testing.T) {
	want := repository.File{ID: "abc-123", Name: "report.pdf", SizeBytes: 42}
	repo := &repomock.Repository{File: &want}
	svc := newService(t, repo, &storagemock.Provider{})

	got, err := svc.Metadata(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("Metadata() error = %v, want nil", err)
	}
	if got != want {
		t.Errorf("Metadata() = %+v, want %+v", got, want)
	}
	if repo.GetByIDArg != "abc-123" {
		t.Errorf("repo.GetByID arg = %q, want %q", repo.GetByIDArg, "abc-123")
	}
}

func TestFileService_Metadata_RepositoryError(t *testing.T) {
	repo := &repomock.Repository{Err: errors.New("no rows")}
	svc := newService(t, repo, &storagemock.Provider{})

	got, err := svc.Metadata(context.Background(), "missing")

	if err == nil {
		t.Fatal("Metadata() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to fetch file metadata") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "failed to fetch file metadata")
	}
	if got != (repository.File{}) {
		t.Errorf("Metadata() = %+v, want zero value", got)
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestFileService_List_LimitOffsetArithmetic(t *testing.T) {
	tests := []struct {
		name       string
		page       int
		pageSize   int
		wantLimit  int
		wantOffset int
	}{
		{name: "first page", page: 1, pageSize: 20, wantLimit: 20, wantOffset: 0},
		{name: "third page", page: 3, pageSize: 10, wantLimit: 10, wantOffset: 20},
		{name: "large page size", page: 2, pageSize: 100, wantLimit: 100, wantOffset: 100},
		// Service performs no clamping of its own; page<1 is expected to be
		// normalized upstream (file-server's handler already defaults it to
		// 1 before the request reaches this service). Documented here as
		// current behavior, not a correctness claim.
		{name: "page zero is not clamped here", page: 0, pageSize: 20, wantLimit: 20, wantOffset: -20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &repomock.Repository{}
			svc := newService(t, repo, &storagemock.Provider{})

			if _, _, err := svc.List(context.Background(), tt.page, tt.pageSize); err != nil {
				t.Fatalf("List() error = %v, want nil", err)
			}

			if repo.ListLimit != tt.wantLimit || repo.ListOffset != tt.wantOffset {
				t.Errorf("repo.List called with (limit=%d, offset=%d), want (limit=%d, offset=%d)", repo.ListLimit, repo.ListOffset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}

func TestFileService_List_Success(t *testing.T) {
	files := []*repository.File{
		{ID: "1", Name: "a.txt", SizeBytes: 10, ContentType: "text/plain"},
		{ID: "2", Name: "b.txt", SizeBytes: 20, ContentType: "text/plain"},
	}
	repo := &repomock.Repository{ListFiles: files, Total: 2}
	svc := newService(t, repo, &storagemock.Provider{})

	got, total, err := svc.List(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(got) != len(files) {
		t.Fatalf("List() returned %d files, want %d", len(got), len(files))
	}
	for i := range files {
		if got[i] != files[i] {
			t.Errorf("List()[%d] = %+v, want %+v", i, got[i], files[i])
		}
	}
}

func TestFileService_List_RepositoryError(t *testing.T) {
	repo := &repomock.Repository{Err: errors.New("query timeout")}
	svc := newService(t, repo, &storagemock.Provider{})

	got, total, err := svc.List(context.Background(), 1, 20)

	if err == nil {
		t.Fatal("List() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to list files") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "failed to list files")
	}
	if got != nil {
		t.Errorf("List() files = %v, want nil", got)
	}
	if total != 0 {
		t.Errorf("List() total = %d, want 0", total)
	}
}

// ---------------------------------------------------------------------------
// DownloadArchive
// ---------------------------------------------------------------------------

func TestFileService_DownloadArchive_Success(t *testing.T) {
	repo := &repomock.Repository{
		Files: []repository.File{
			{ID: "1", Name: "a.txt", StoragePath: "path/a"},
			{ID: "2", Name: "b.txt", StoragePath: "path/b"},
		},
	}
	storage := &storagemock.Provider{
		Content: map[string]string{
			"path/a": "content of a",
			"path/b": "content of b, a bit longer",
		},
	}
	dir := t.TempDir()
	svc := NewFileService(repo, storage, dir, dir)

	reader, err := svc.DownloadArchive(context.Background(), []string{"1", "2"})
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v, want nil", err)
	}

	if got := archiveFilesRemaining(t, dir); got != 1 {
		t.Fatalf("temp dir has %d archive file(s) while reader is open, want 1", got)
	}

	zipBytes, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read archive reader: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close archive reader: %v", err)
	}

	// Closing must delete the staged temp file.
	if got := archiveFilesRemaining(t, dir); got != 0 {
		t.Errorf("temp dir has %d archive file(s) after Close(), want 0", got)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("archive is not a valid zip: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("zip has %d entries, want 2", len(zr.File))
	}
	gotContents := make(map[string]string, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %q: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %q: %v", f.Name, err)
		}
		gotContents[f.Name] = string(b)
	}
	want := map[string]string{"a.txt": "content of a", "b.txt": "content of b, a bit longer"}
	if len(gotContents) != len(want) {
		t.Fatalf("zip entry names = %v, want %v", gotContents, want)
	}
	for name, wantContent := range want {
		if gotContents[name] != wantContent {
			t.Errorf("zip entry %q content = %q, want %q", name, gotContents[name], wantContent)
		}
	}
}

func TestFileService_DownloadArchive_RepositoryError(t *testing.T) {
	repo := &repomock.Repository{Err: errors.New("db error")}
	storage := &storagemock.Provider{}
	dir := t.TempDir()
	svc := NewFileService(repo, storage, dir, dir)

	reader, err := svc.DownloadArchive(context.Background(), []string{"1"})

	if err == nil {
		t.Fatal("DownloadArchive() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to fetch file metadata") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "failed to fetch file metadata")
	}
	if reader != nil {
		t.Errorf("DownloadArchive() reader = %v, want nil", reader)
	}
	if got := archiveFilesRemaining(t, dir); got != 0 {
		t.Errorf("temp dir has %d archive file(s), want 0", got)
	}
}

func TestFileService_DownloadArchive_SomeFilesNotFound(t *testing.T) {
	repo := &repomock.Repository{
		// Only one of the two requested IDs resolved.
		Files: []repository.File{{ID: "1", Name: "a.txt", StoragePath: "path/a"}},
	}
	storage := &storagemock.Provider{}
	dir := t.TempDir()
	svc := NewFileService(repo, storage, dir, dir)

	reader, err := svc.DownloadArchive(context.Background(), []string{"1", "missing"})

	if err == nil {
		t.Fatal("DownloadArchive() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "not found")
	}
	if reader != nil {
		t.Errorf("DownloadArchive() reader = %v, want nil", reader)
	}
	if len(storage.FetchedPaths) != 0 {
		t.Errorf("storage.Fetch called %d times, want 0 (should fail before fetching)", len(storage.FetchedPaths))
	}
	if got := archiveFilesRemaining(t, dir); got != 0 {
		t.Errorf("temp dir has %d archive file(s), want 0", got)
	}
}

func TestFileService_DownloadArchive_StorageErrorMidLoop(t *testing.T) {
	repo := &repomock.Repository{
		Files: []repository.File{
			{ID: "1", Name: "a.txt", StoragePath: "path/a"},
			{ID: "2", Name: "b.txt", StoragePath: "path/b"},
		},
	}
	// path/b is deliberately left out of Content, so the second Fetch call
	// fails while the first one already succeeded.
	storage := &storagemock.Provider{Content: map[string]string{"path/a": "ok"}}
	dir := t.TempDir()
	svc := NewFileService(repo, storage, dir, dir)

	reader, err := svc.DownloadArchive(context.Background(), []string{"1", "2"})

	if err == nil {
		t.Fatal("DownloadArchive() error = nil, want error")
	}
	if reader != nil {
		t.Errorf("DownloadArchive() reader = %v, want nil", reader)
	}
	// The partially-written temp zip must be cleaned up, not leaked.
	if got := archiveFilesRemaining(t, dir); got != 0 {
		t.Errorf("temp dir has %d archive file(s) after mid-loop failure, want 0", got)
	}
}

type brokenReader struct {
	data []byte
	sent bool
}

func (b *brokenReader) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		n := copy(p, b.data)
		return n, nil
	}
	return 0, errors.New("connection reset")
}

func TestFileService_DownloadArchive_CopyErrorMidStream(t *testing.T) {
	repo := &repomock.Repository{
		Files: []repository.File{{ID: "1", Name: "a.txt", StoragePath: "path/a"}},
	}
	storage := &storagemock.Provider{
		Readers: map[string]io.ReadCloser{
			"path/a": io.NopCloser(&brokenReader{data: []byte("partial")}),
		},
	}
	dir := t.TempDir()
	svc := NewFileService(repo, storage, dir, dir)

	reader, err := svc.DownloadArchive(context.Background(), []string{"1"})

	if err == nil {
		t.Fatal("DownloadArchive() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("error = %q, want it to wrap %q", err.Error(), "connection reset")
	}
	if reader != nil {
		t.Errorf("DownloadArchive() reader = %v, want nil", reader)
	}
	if got := archiveFilesRemaining(t, dir); got != 0 {
		t.Errorf("temp dir has %d archive file(s) after copy failure, want 0", got)
	}
}

func TestFileService_DownloadArchive_TempFileCreationFailure(t *testing.T) {
	repo := &repomock.Repository{
		Files: []repository.File{{ID: "1", Name: "a.txt", StoragePath: "path/a"}},
	}
	storage := &storagemock.Provider{}

	// Point tempDir through a path component that is a regular file, not a
	// directory, so both the constructor's os.MkdirAll and DownloadArchive's
	// os.CreateTemp fail deterministically - real local-filesystem I/O, no
	// mocking of os.CreateTemp itself.
	base := t.TempDir()
	blocker := filepath.Join(base, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}
	tempDir := filepath.Join(blocker, "sub")

	svc := NewFileService(repo, storage, base, tempDir)

	reader, err := svc.DownloadArchive(context.Background(), []string{"1"})

	if err == nil {
		t.Fatal("DownloadArchive() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to create temp file") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "failed to create temp file")
	}
	if reader != nil {
		t.Errorf("DownloadArchive() reader = %v, want nil", reader)
	}
}