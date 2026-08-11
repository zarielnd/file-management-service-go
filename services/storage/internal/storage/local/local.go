package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Store struct {
	storagePath string
}

func NewStore(storagePath string) *Store {
	return &Store{
		storagePath: storagePath,
	}
}

func (s *Store) Store(ctx context.Context, path string, reader io.Reader) (int64, error) {
	fullPath := filepath.Join(s.storagePath, path)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create directories for path %s: %w", fullPath, err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create file at path %s: %w", fullPath, err)
	}
	defer f.Close()

	written, err := io.Copy(f, reader)
	if err != nil {
		os.Remove(fullPath) // Clean up the file if writing fails
		return 0, fmt.Errorf("failed to write data to file at path %s: %w", fullPath, err)
	}
	return written, nil
}

func (s *Store) Fetch(ctx context.Context, path string) (io.ReadCloser, error) {
	fullPath := filepath.Join(s.storagePath, path)
	f, err := os.Open(fullPath)
	if err != nil {
		if(os.IsNotExist(err)) {
			return nil, fmt.Errorf("file not found")
		}
		return nil, err
	}
	return f, nil
}