package storage

import (
	"context"
	"io"
)

type Provider interface {
	Store(ctx context.Context, path string, reader io.Reader) (int64, error)
	Fetch(ctx context.Context, path string) (io.ReadCloser, error)
}