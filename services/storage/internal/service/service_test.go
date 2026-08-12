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
)

// ---------------------------------------------------------------------------
// fakeRepository / fakeStorageProvider: hand-written fakes for the
// repository.Repository and storage.Provider interfaces. The module has no
// testify/gomock dependency, so these stay dependency-free.
//
// IMPORTANT (reader lifecycle): FileService.Store passes an io.TeeReader
// wrapping the caller's reader and a sha256 hasher into storage.Store. The
// checksum is only correct if whatever reads storagePath's reader actually
// drains it - exactly like the real local.Store implementation does via
// io.Copy. fakeStorageProvider.Store therefore reads the reader eagerly with
// io.ReadAll, both to compute a correct byte count and to drive the TeeReader
// side effect that produces the SHA-256 checksum FileService.Store computes
// afterwards.
// ---------------------------------------------------------------------------

type storeCall struct {
	Path    string
	Content []byte
}

type fakeStorageProvider struct {
	storeCalls []storeCall
	storeFunc  func(path string, content []byte) (int64, error)

	fetchCalls []string
	fetchFunc  func(path string) (io.ReadCloser, error)
}

func (p *fakeStorageProvider) Store(ctx context.Context, path string, reader io.Reader) (int64, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return 0, err
	}
	p.storeCalls = append(p.storeCalls, storeCall{Path: path, Content: content})
	if p.storeFunc != nil {
		return p.storeFunc(path, content)
	}
	return int64(len(content)), nil
}

func (p *fakeStorageProvider) Fetch(ctx context.Context, path string) (io.ReadCloser, error) {
	p.fetchCalls = append(p.fetchCalls, path)
	if p.fetchFunc != nil {
		return p.fetchFunc(path)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

type listCall struct{ Limit, Offset int }

type fakeRepository struct {
	createCalls []repository.File
	createFunc  func(file *repository.File) error

	getByIDCalls []string
	getByIDFunc  func(id string) (*repository.File, error)

	getByIDsCalls [][]string
	getByIDsFunc  func(ids []string) ([]repository.File, error)

	listCalls []listCall
	listFunc  func(limit, offset int) ([]*repository.File, int, error)
}

func (r *fakeRepository) Create(ctx context.Context, file *repository.File) error {
	r.createCalls = append(r.createCalls, *file)
	if r.createFunc != nil {
		return r.createFunc(file)
	}
	return nil
}

func (r *fakeRepository) GetByID(ctx context.Context, id string) (*repository.File, error) {
	r.getByIDCalls = append(r.getByIDCalls, id)
	if r.getByIDFunc != nil {
		return r.getByIDFunc(id)
	}
	return &repository.File{}, nil
}

func (r *fakeRepository) GetByIDs(ctx context.Context, ids []string) ([]repository.File, error) {
	r.getByIDsCalls = append(r.getByIDsCalls, ids)
	if r.getByIDsFunc != nil {
		return r.getByIDsFunc(ids)
	}
	return nil, nil
}

func (r *fakeRepository) List(ctx context.Context, limit, offset int) ([]*repository.File, int, error) {
	r.listCalls = append(r.listCalls, listCall{limit, offset})
	if r.listFunc != nil {
		return r.listFunc(limit, offset)
	}
	return nil, 0, nil
}

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
func newService(t *testing.T, repo *fakeRepository, storage *fakeStorageProvider) *FileService {
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
	repo := &fakeRepository{}
	storage := &fakeStorageProvider{}
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
	if len(storage.storeCalls) != 1 {
		t.Fatalf("storage.Store called %d times, want 1", len(storage.storeCalls))
	}
	if storage.storeCalls[0].Path != wantPath {
		t.Errorf("storage.Store path = %q, want %q", storage.storeCalls[0].Path, wantPath)
	}
	if !bytes.Equal(storage.storeCalls[0].Content, content) {
		t.Errorf("storage.Store content = %q, want %q", storage.storeCalls[0].Content, content)
	}

	// repo.Create received the same file that was returned.
	if len(repo.createCalls) != 1 {
		t.Fatalf("repo.Create called %d times, want 1", len(repo.createCalls))
	}
	if repo.createCalls[0] != got {
		t.Errorf("repo.Create received %+v, want %+v", repo.createCalls[0], got)
	}
}

func TestFileService_Store_StorageError(t *testing.T) {
	repo := &fakeRepository{}
	storage := &fakeStorageProvider{
		storeFunc: func(path string, content []byte) (int64, error) {
			return 0, errors.New("disk full")
		},
	}
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
	if len(repo.createCalls) != 0 {
		t.Errorf("repo.Create called %d times, want 0", len(repo.createCalls))
	}
}

func TestFileService_Store_RepositoryError(t *testing.T) {
	repo := &fakeRepository{
		createFunc: func(file *repository.File) error {
			return errors.New("unique violation")
		},
	}
	storage := &fakeStorageProvider{}
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
	if len(storage.storeCalls) != 1 {
		t.Errorf("storage.Store called %d times, want 1 (write happens before repo.Create)", len(storage.storeCalls))
	}
}

// ---------------------------------------------------------------------------
// Fetch
// ---------------------------------------------------------------------------

func TestFileService_Fetch_Success(t *testing.T) {
	want := repository.File{ID: "abc-123", Name: "report.pdf", StoragePath: "ab/c-/abc-123/report.pdf", ContentType: "application/pdf", SizeBytes: 9}
	body := "pdf bytes"

	repo := &fakeRepository{
		getByIDFunc: func(id string) (*repository.File, error) {
			f := want
			return &f, nil
		},
	}
	storage := &fakeStorageProvider{
		fetchFunc: func(path string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(body)), nil
		},
	}
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

	if len(repo.getByIDCalls) != 1 || repo.getByIDCalls[0] != "abc-123" {
		t.Errorf("repo.GetByID calls = %v, want [\"abc-123\"]", repo.getByIDCalls)
	}
	if len(storage.fetchCalls) != 1 || storage.fetchCalls[0] != want.StoragePath {
		t.Errorf("storage.Fetch calls = %v, want [%q]", storage.fetchCalls, want.StoragePath)
	}
}

func TestFileService_Fetch_RepositoryError(t *testing.T) {
	repo := &fakeRepository{
		getByIDFunc: func(id string) (*repository.File, error) {
			return nil, errors.New("not found")
		},
	}
	storage := &fakeStorageProvider{}
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
	if len(storage.fetchCalls) != 0 {
		t.Errorf("storage.Fetch called %d times, want 0", len(storage.fetchCalls))
	}
}

func TestFileService_Fetch_StorageError(t *testing.T) {
	repo := &fakeRepository{
		getByIDFunc: func(id string) (*repository.File, error) {
			return &repository.File{ID: id, StoragePath: "some/path"}, nil
		},
	}
	storage := &fakeStorageProvider{
		fetchFunc: func(path string) (io.ReadCloser, error) {
			return nil, errors.New("object not found")
		},
	}
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
	repo := &fakeRepository{
		getByIDFunc: func(id string) (*repository.File, error) {
			f := want
			return &f, nil
		},
	}
	svc := newService(t, repo, &fakeStorageProvider{})

	got, err := svc.Metadata(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("Metadata() error = %v, want nil", err)
	}
	if got != want {
		t.Errorf("Metadata() = %+v, want %+v", got, want)
	}
	if len(repo.getByIDCalls) != 1 || repo.getByIDCalls[0] != "abc-123" {
		t.Errorf("repo.GetByID calls = %v, want [\"abc-123\"]", repo.getByIDCalls)
	}
}

func TestFileService_Metadata_RepositoryError(t *testing.T) {
	repo := &fakeRepository{
		getByIDFunc: func(id string) (*repository.File, error) {
			return nil, errors.New("no rows")
		},
	}
	svc := newService(t, repo, &fakeStorageProvider{})

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
			repo := &fakeRepository{}
			svc := newService(t, repo, &fakeStorageProvider{})

			if _, _, err := svc.List(context.Background(), tt.page, tt.pageSize); err != nil {
				t.Fatalf("List() error = %v, want nil", err)
			}

			if len(repo.listCalls) != 1 {
				t.Fatalf("repo.List called %d times, want 1", len(repo.listCalls))
			}
			got := repo.listCalls[0]
			if got.Limit != tt.wantLimit || got.Offset != tt.wantOffset {
				t.Errorf("repo.List called with (limit=%d, offset=%d), want (limit=%d, offset=%d)", got.Limit, got.Offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}

func TestFileService_List_Success(t *testing.T) {
	files := []*repository.File{
		{ID: "1", Name: "a.txt"},
		{ID: "2", Name: "b.txt"},
	}
	repo := &fakeRepository{
		listFunc: func(limit, offset int) ([]*repository.File, int, error) {
			return files, 2, nil
		},
	}
	svc := newService(t, repo, &fakeStorageProvider{})

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
	repo := &fakeRepository{
		listFunc: func(limit, offset int) ([]*repository.File, int, error) {
			return nil, 0, errors.New("query timeout")
		},
	}
	svc := newService(t, repo, &fakeStorageProvider{})

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
	repo := &fakeRepository{
		getByIDsFunc: func(ids []string) ([]repository.File, error) {
			return []repository.File{
				{ID: "1", Name: "a.txt", StoragePath: "path/a"},
				{ID: "2", Name: "b.txt", StoragePath: "path/b"},
			}, nil
		},
	}
	contents := map[string]string{
		"path/a": "content of a",
		"path/b": "content of b, a bit longer",
	}
	storage := &fakeStorageProvider{
		fetchFunc: func(path string) (io.ReadCloser, error) {
			c, ok := contents[path]
			if !ok {
				return nil, errors.New("unexpected path: " + path)
			}
			return io.NopCloser(strings.NewReader(c)), nil
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
	repo := &fakeRepository{
		getByIDsFunc: func(ids []string) ([]repository.File, error) {
			return nil, errors.New("db error")
		},
	}
	storage := &fakeStorageProvider{}
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
	repo := &fakeRepository{
		getByIDsFunc: func(ids []string) ([]repository.File, error) {
			// Only one of the two requested IDs resolved.
			return []repository.File{{ID: "1", Name: "a.txt", StoragePath: "path/a"}}, nil
		},
	}
	storage := &fakeStorageProvider{}
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
	if len(storage.fetchCalls) != 0 {
		t.Errorf("storage.Fetch called %d times, want 0 (should fail before fetching)", len(storage.fetchCalls))
	}
	if got := archiveFilesRemaining(t, dir); got != 0 {
		t.Errorf("temp dir has %d archive file(s), want 0", got)
	}
}

func TestFileService_DownloadArchive_StorageErrorMidLoop(t *testing.T) {
	repo := &fakeRepository{
		getByIDsFunc: func(ids []string) ([]repository.File, error) {
			return []repository.File{
				{ID: "1", Name: "a.txt", StoragePath: "path/a"},
				{ID: "2", Name: "b.txt", StoragePath: "path/b"},
			}, nil
		},
	}
	storage := &fakeStorageProvider{
		fetchFunc: func(path string) (io.ReadCloser, error) {
			if path == "path/a" {
				return io.NopCloser(strings.NewReader("ok")), nil
			}
			return nil, errors.New("object missing")
		},
	}
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

// brokenReader returns a few bytes successfully, then a read error - used to
// exercise the io.Copy failure branch inside DownloadArchive's zip loop.
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
	repo := &fakeRepository{
		getByIDsFunc: func(ids []string) ([]repository.File, error) {
			return []repository.File{{ID: "1", Name: "a.txt", StoragePath: "path/a"}}, nil
		},
	}
	storage := &fakeStorageProvider{
		fetchFunc: func(path string) (io.ReadCloser, error) {
			return io.NopCloser(&brokenReader{data: []byte("partial")}), nil
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
	repo := &fakeRepository{
		getByIDsFunc: func(ids []string) ([]repository.File, error) {
			return []repository.File{{ID: "1", Name: "a.txt", StoragePath: "path/a"}}, nil
		},
	}
	storage := &fakeStorageProvider{}

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