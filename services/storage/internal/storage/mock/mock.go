// Package mock provides a fake storage.Provider for tests.
package mock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/zarielnd/file-management-service-go/services/storage/internal/storage"
)

type Provider struct {
	Content map[string]string        // path -> content, returned by Fetch
	Readers map[string]io.ReadCloser // path -> reader; overrides Content for a path, e.g. to simulate a stream that fails mid-read

	StoreErr error
	FetchErr error // if set, returned by Fetch for every path

	StoredPaths   []string
	StoredContent map[string][]byte
	FetchedPaths  []string
}

func (p *Provider) Store(ctx context.Context, path string, reader io.Reader) (int64, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return 0, err
	}
	p.StoredPaths = append(p.StoredPaths, path)
	if p.StoredContent == nil {
		p.StoredContent = make(map[string][]byte)
	}
	p.StoredContent[path] = content
	if p.StoreErr != nil {
		return 0, p.StoreErr
	}
	return int64(len(content)), nil
}

func (p *Provider) Fetch(ctx context.Context, path string) (io.ReadCloser, error) {
	p.FetchedPaths = append(p.FetchedPaths, path)
	if p.FetchErr != nil {
		return nil, p.FetchErr
	}
	if r, ok := p.Readers[path]; ok {
		return r, nil
	}
	content, ok := p.Content[path]
	if !ok {
		return nil, errors.New("mock: no content configured for path " + path)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (s *Provider) PresignFetch(ctx context.Context, path string, expiry time.Duration) (string, error) {
	// Local dev: return internal file-server route or just error if not supported
	return "", fmt.Errorf("presigned URLs not supported for local storage")
}

func (s *Provider) PresignStore(ctx context.Context, path string, expiry time.Duration) (string, error) {
	// Local dev: return internal file-server route or just error if not supported
	return "", fmt.Errorf("presigned URLs not supported for local storage")
}

var _ storage.Provider = (*Provider)(nil)
