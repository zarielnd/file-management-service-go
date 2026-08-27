package service

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/repository"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/storage"
)

type FileService struct {
	repo    repository.Repository
	storage storage.Provider
	baseDir string
	tempDir string
}

func NewFileService(repo repository.Repository, storage storage.Provider, baseDir, tempDir string) *FileService {
	os.MkdirAll(tempDir, 0755)
	return &FileService{
		repo:    repo,
		storage: storage,
		baseDir: baseDir,
		tempDir: tempDir,
	}
}

func (s *FileService) Store(ctx context.Context, name, contentType, ownerID string, reader io.Reader) (repository.File, error) {
	id := uuid.Must(uuid.NewV7()).String()
	storagePath := s.makePath(id, name)

	hash := sha256.New()
	tee := io.TeeReader(reader, hash)
	size, err := s.storage.Store(ctx, storagePath, tee)
	if err != nil {
		return repository.File{}, fmt.Errorf("failed to store file in storage: %w", err)
	}

	checksum := hex.EncodeToString(hash.Sum(nil))

	file := repository.File{
		ID:          id,
		Name:        name,
		StoragePath: storagePath,
		ContentType: contentType,
		SizeBytes:   size,
		Checksum:    checksum,
		CreatedAt:   time.Now().UTC(),
		OwnerID:     ownerID,
	}
	if err := s.repo.Create(ctx, &file); err != nil {
		return repository.File{}, fmt.Errorf("failed to store file metadata in repository: %w", err)
	}
	return file, nil
}

func (s *FileService) Fetch(ctx context.Context, id, userID string) (io.ReadCloser, repository.File, error) {
	file, err := s.repo.GetByID(ctx, id, userID)
	if err != nil {
		return nil, repository.File{}, fmt.Errorf("failed to fetch file metadata: %w", err)
	}

	reader, err := s.storage.Fetch(ctx, file.StoragePath)
	if err != nil {
		return nil, repository.File{}, fmt.Errorf("failed to fetch file from storage: %w", err)
	}
	return reader, *file, nil
}

func (s *FileService) Metadata(ctx context.Context, id, userID string) (repository.File, error) {
	file, err := s.repo.GetByID(ctx, id, userID)
	if err != nil {
		return repository.File{}, fmt.Errorf("failed to fetch file metadata: %w", err)
	}
	return *file, nil
}

func (s *FileService) List(ctx context.Context, page, pageSize int, userID string) ([]*repository.File, int, error) {
	limit := pageSize
	offset := (page - 1) * pageSize
	files, count, err := s.repo.List(ctx, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list files: %w", err)
	}
	return files, count, nil
}

func (s *FileService) DownloadArchive(ctx context.Context, ids []string, userID string) (io.ReadCloser, error) {
	files, err := s.repo.GetByIDs(ctx, ids, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file metadata: %w", err)
	}

	if len(files) != len(ids) {
		return nil, fmt.Errorf("one or more files not found")
	}

	tmpFile, err := os.CreateTemp(s.tempDir, "archive-*.zip")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	zw := zip.NewWriter(tmpFile)
	for _, file := range files {
		rc, err := s.storage.Fetch(ctx, file.StoragePath)
		if err != nil {
			zw.Close()
			tmpFile.Close()
			os.Remove(tmpPath)
			return nil, fmt.Errorf("Failed to fetch files")
		}

		w, err := zw.Create(file.Name)
		if err != nil {
			zw.Close()
			tmpFile.Close()
			os.Remove(tmpPath)
			return nil, fmt.Errorf("Failed to zip files")
		}
		if _, err := io.Copy(w, rc); err != nil {
			rc.Close()
			zw.Close()
			tmpFile.Close()
			os.Remove(tmpPath)
			return nil, err
		}
		rc.Close()
	}

	if err := zw.Close(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return nil, err
	}
	tmpFile.Close()

	f, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, err
	}

	return &deleteOnClose{File: f, path: tmpPath}, nil
}

func (s *FileService) GetByIDs(ctx context.Context, ids []string, userID string) ([]*repository.File, error) {
	files, err := s.repo.GetByIDs(ctx, ids, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch files: %w", err)
	}
	return files, nil
}

func (s *FileService) PresignFetch(ctx context.Context, id string, userID string, expiry time.Duration) (string, error) {
	file, err := s.repo.GetByID(ctx, id, userID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch file metadata: %w", err)
	}
	return s.storage.PresignFetch(ctx, file.StoragePath, expiry)
}

func (s *FileService) PresignStore(ctx context.Context, id string, expiry time.Duration) (string, error) {
	file, err := s.repo.GetByID(ctx, id, "")
	if err != nil {
		return "", fmt.Errorf("failed to fetch file metadata: %w", err)
	}
	return s.storage.PresignStore(ctx, file.StoragePath, expiry)
}

func (s *FileService) ReserveUpload(ctx context.Context, name, contentType, ownerID string) (*repository.File, string, error) {
	id := uuid.Must(uuid.NewV7()).String()
	storagePath := s.makePath(id, name)

	file := repository.File{
		ID:          id,
		Name:        name,
		StoragePath: storagePath,
		ContentType: contentType,
		CreatedAt:   time.Now().UTC(),
		OwnerID:     ownerID,
	}
	if err := s.repo.Create(ctx, &file); err != nil {
		return &repository.File{}, "", fmt.Errorf("reserve: %w", err)
	}

	url, err := s.storage.PresignStore(ctx, storagePath, 15*time.Minute)
	if err != nil {
		return &repository.File{}, "", fmt.Errorf("presign: %w", err)
	}

	return &file, url, nil
}

func (s *FileService) ConfirmUpload(ctx context.Context, id string, size int64, checksum string) (*repository.File, error) {
	if err := s.repo.ConfirmUpload(ctx, id, size, checksum); err != nil {
		return &repository.File{}, err
	}
	return s.repo.GetByID(ctx, id, "")
}

func (s *FileService) PresignArchiveStore(ctx context.Context, path string, contentType string) (string, error) {
	return s.storage.PresignArchiveStore(ctx, path, contentType, 15*time.Minute)
}

func (s *FileService) PresignArchiveFetch(ctx context.Context, path string) (string, error) {
	return s.storage.PresignArchiveFetch(ctx, path, 15*time.Minute)
}

type deleteOnClose struct {
	*os.File
	path string
}

func (d *deleteOnClose) Close() error {
	err := d.File.Close()
	os.Remove(d.path)
	return err
}

func (s *FileService) makePath(id, filename string) string {
	return filepath.Join(id[:2], id[2:4], id, filename)
}
