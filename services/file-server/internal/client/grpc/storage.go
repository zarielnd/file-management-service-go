package connectrpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"

	storagev2 "github.com/zarielnd/file-management-service-go/gen/storage/v2"
	storagev2connect "github.com/zarielnd/file-management-service-go/gen/storage/v2/storagev2connect"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
)

type storageClient struct {
	client     storagev2connect.StorageServiceClient
	serviceKey string
}

type authTransport struct {
	base       http.RoundTripper
	source     oauth2.TokenSource
	serviceKey string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.source.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth token: %w", err)
	}

	req = req.Clone(req.Context())
	// This is the magic line that satisfies Cloud Run's "Require authentication"
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	// Keep your custom app-level auth too
	if t.serviceKey != "" {
		req.Header.Set("X-Service-Key", t.serviceKey)
	}

	if userID := client.UserIDFromContext(req.Context()); userID != "" {
		req.Header.Set("X-User-Id", userID)
	}

	return t.base.RoundTrip(req)
}

func NewStorageClient(target, serviceKey string) (client.StorageClient, func() error, error) {

	ts, err := idtoken.NewTokenSource(context.Background(), target)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create token source: %w", err)
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()

	// Wrap OTel transport with our Auth transport
	baseTransport := otelhttp.NewTransport(tr)
	authedTransport := &authTransport{
		base:       baseTransport,
		source:     ts,
		serviceKey: serviceKey,
	}

	httpClient := &http.Client{
		Transport: authedTransport,
	}

	c := &storageClient{
		client:     storagev2connect.NewStorageServiceClient(httpClient, target),
		serviceKey: serviceKey,
	}

	return c, func() error { return nil }, nil
}

func normalizeTarget(target string) string {
	target = strings.TrimRight(target, "/")

	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target
	}
	// Default to HTTPS for production (Cloud Run)
	return "https://" + target
}

func (c *storageClient) Store(ctx context.Context, input client.UploadInput) (domain.File, error) {
	stream := c.client.UploadFile(ctx)

	if err := stream.Send(&storagev2.UploadFileRequest{
		Payload: &storagev2.UploadFileRequest_Info{
			Info: &storagev2.FileInfo{
				Filename:    input.Name,
				ContentType: input.ContentType,
				Size:        input.Size,
			},
		},
	}); err != nil {
		return domain.File{}, err
	}

	buf := make([]byte, 64*1024)
	for {
		n, err := input.Content.Read(buf)
		if n > 0 {
			if err := stream.Send(&storagev2.UploadFileRequest{
				Payload: &storagev2.UploadFileRequest_ChunkData{ChunkData: buf[:n]},
			}); err != nil {
				return domain.File{}, err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return domain.File{}, err
		}
	}

	resp, err := stream.CloseAndReceive()
	if err != nil {
		return domain.File{}, err
	}

	return protoToDomain(resp.Msg.File), nil
}

func (c *storageClient) Fetch(ctx context.Context, id string) (io.ReadCloser, domain.File, error) {
	req := connect.NewRequest(&storagev2.GetFileRequest{Id: id})

	stream, err := c.client.GetFile(ctx, req)
	if err != nil {
		return nil, domain.File{}, err
	}

	if !stream.Receive() {
		err := stream.Err()
		stream.Close()
		if err != nil {
			return nil, domain.File{}, err
		}
		return nil, domain.File{}, errors.New("empty file stream")
	}

	file := protoToDomain(stream.Msg().Metadata)
	firstChunk := stream.Msg().Data

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer stream.Close()

		if len(firstChunk) > 0 {
			if _, err := pw.Write(firstChunk); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		for stream.Receive() {
			if _, err := pw.Write(stream.Msg().Data); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		if err := stream.Err(); err != nil {
			pw.CloseWithError(err)
		}
	}()

	return pr, file, nil
}

func (c *storageClient) Metadata(ctx context.Context, id string) (domain.File, error) {
	req := connect.NewRequest(&storagev2.GetMetadataRequest{Id: id})

	resp, err := c.client.GetMetadata(ctx, req)
	if err != nil {
		return domain.File{}, err
	}
	return protoToDomain(resp.Msg), nil
}

func (c *storageClient) List(ctx context.Context, page, pageSize int) ([]domain.File, int, error) {
	req := connect.NewRequest(&storagev2.ListFilesRequest{
		Page:     int32(page),
		PageSize: int32(pageSize),
	})

	resp, err := c.client.ListFiles(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	files := make([]domain.File, 0, len(resp.Msg.Files))
	for _, f := range resp.Msg.Files {
		files = append(files, protoToDomain(f))
	}
	return files, int(resp.Msg.Total), nil
}

func (c *storageClient) DownloadArchive(ctx context.Context, ids []string) (io.ReadCloser, error) {
	req := connect.NewRequest(&storagev2.DownloadArchiveRequest{FileIds: ids})

	stream, err := c.client.DownloadArchive(ctx, req)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer stream.Close()
		for stream.Receive() {
			if _, err := pw.Write(stream.Msg().Data); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		if err := stream.Err(); err != nil {
			pw.CloseWithError(err)
		}
	}()
	return pr, nil
}

func (c *storageClient) GetDownloadURLs(ctx context.Context, ids []string) ([]client.DownloadURL, error) {
	req := connect.NewRequest(&storagev2.GetDownloadURLsRequest{FileIds: ids})

	resp, err := c.client.GetDownloadURLs(ctx, req)
	if err != nil {
		return nil, mapConnectError(err)
	}

	var out []client.DownloadURL
	for _, f := range resp.Msg.Files {
		out = append(out, client.DownloadURL{
			FileID:    f.FileId,
			Name:      f.Name,
			URL:       f.Url,
			SizeBytes: f.SizeBytes,
		})
	}
	return out, nil
}

func (c *storageClient) GetUploadURL(ctx context.Context, filename, contentType string) (client.UploadURL, error) {
	req := connect.NewRequest(&storagev2.GetUploadURLRequest{
		Filename:    filename,
		ContentType: contentType,
	})

	resp, err := c.client.GetUploadURL(ctx, req)
	if err != nil {
		return client.UploadURL{}, mapConnectError(err)
	}
	return client.UploadURL{
		UploadURL: resp.Msg.UploadUrl,
		FileID:    resp.Msg.FileId,
	}, nil
}

func (c *storageClient) ConfirmUpload(ctx context.Context, fileID string, size int64, checksum string) (domain.File, error) {
	req := connect.NewRequest(&storagev2.ConfirmUploadRequest{
		FileId:    fileID,
		SizeBytes: size,
		Checksum:  checksum,
	})

	resp, err := c.client.ConfirmUpload(ctx, req)
	if err != nil {
		return domain.File{}, mapConnectError(err)
	}
	return protoToDomain(resp.Msg), nil
}

func (c *storageClient) GetArchiveUploadURL(ctx context.Context, path, contentType string) (string, error) {
	req := connect.NewRequest(&storagev2.GetArchiveUploadURLRequest{
		Path:        path,
		ContentType: contentType,
	})

	resp, err := c.client.GetArchiveUploadURL(ctx, req)
	if err != nil {
		return "", mapConnectError(err)
	}
	return resp.Msg.UploadUrl, nil
}

func (c *storageClient) GetArchiveDownloadURL(ctx context.Context, path string) (string, error) {
	req := connect.NewRequest(&storagev2.GetArchiveDownloadURLRequest{Path: path})

	resp, err := c.client.GetArchiveDownloadURL(ctx, req)
	if err != nil {
		return "", mapConnectError(err)
	}
	return resp.Msg.DownloadUrl, nil
}

func mapConnectError(err error) error {
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return err
	}
	switch connectErr.Code() {
	case connect.CodeNotFound:
		return fmt.Errorf("not found: %s", connectErr.Message())
	case connect.CodeInvalidArgument:
		return fmt.Errorf("invalid argument: %s", connectErr.Message())
	default:
		return fmt.Errorf("storage error: %s", connectErr.Message())
	}
}

func protoToDomain(f *storagev2.FileMetadata) domain.File {
	t := f.CreatedAt.AsTime()
	return domain.File{
		ID:          f.Id,
		Name:        f.Name,
		Size:        f.Size,
		ContentType: f.ContentType,
		Checksum:    f.Checksum,
		CreatedAt:   t,
		OwnerID:     f.OwnerId,
	}
}
