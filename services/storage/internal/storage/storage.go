package storage

import (
	"context"
	"io"
	"time"
)

type Provider interface {
	Store(ctx context.Context, path string, reader io.Reader) (int64, error)
	Fetch(ctx context.Context, path string) (io.ReadCloser, error)
	PresignFetch(ctx context.Context, path string, expiry time.Duration) (string, error)
	PresignStore(ctx context.Context, path string, expiry time.Duration) (string, error)
}
