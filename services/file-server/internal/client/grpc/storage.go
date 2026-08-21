package grpc

import (
	"context"
	"fmt"
	"io"

	storagev2 "github.com/zarielnd/file-management-service-go/gen/storage/v2"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type storageClient struct {
	client storagev2.StorageServiceClient
}

func NewStorageClient(target string) (client.StorageClient, func() error, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	c := &storageClient{
		client: storagev2.NewStorageServiceClient(conn),
	}
	return c, conn.Close, nil
}

func (c *storageClient) Store(ctx context.Context, input client.UploadInput) (domain.File, error) {
	stream, err := c.client.UploadFile(ctx)
	if err != nil {
		return domain.File{}, err
	}

	// Send metadata first
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

	// Stream file content in chunks
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

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return domain.File{}, err
	}

	return protoToDomain(resp.File), nil
}

func (c *storageClient) Fetch(ctx context.Context, id string) (io.ReadCloser, domain.File, error) {
	stream, err := c.client.GetFile(ctx, &storagev2.GetFileRequest{Id: id})
	if err != nil {
		return nil, domain.File{}, err
	}

	// First message contains metadata
	resp, err := stream.Recv()
	if err != nil {
		return nil, domain.File{}, err
	}

	file := protoToDomain(resp.Metadata)

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		// Write first chunk's data
		if _, err := pw.Write(resp.Data); err != nil {
			pw.CloseWithError(err)
			return
		}
		// Continue streaming
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			if _, err := pw.Write(resp.Data); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()

	return pr, file, nil
}

func (c *storageClient) Metadata(ctx context.Context, id string) (domain.File, error) {
	resp, err := c.client.GetMetadata(ctx, &storagev2.GetMetadataRequest{Id: id})
	if err != nil {
		return domain.File{}, err
	}
	return protoToDomain(resp), nil
}

func (c *storageClient) List(ctx context.Context, page, pageSize int) ([]domain.File, int, error) {
	resp, err := c.client.ListFiles(ctx, &storagev2.ListFilesRequest{
		Page:     int32(page),
		PageSize: int32(pageSize),
	})
	if err != nil {
		return nil, 0, err
	}

	files := make([]domain.File, 0, len(resp.Files))
	for _, f := range resp.Files {
		files = append(files, protoToDomain(f))
	}
	return files, int(resp.Total), nil
}

func (c *storageClient) DownloadArchive(ctx context.Context, ids []string) (io.ReadCloser, error) {
	stream, err := c.client.DownloadArchive(ctx, &storagev2.DownloadArchiveRequest{FileIds: ids})
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			if _, err := pw.Write(resp.Data); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()
	return pr, nil
}

func (c *storageClient) GetDownloadURLs(ctx context.Context, ids []string) ([]client.DownloadURL, error) {
	resp, err := c.client.GetDownloadURLs(ctx, &storagev2.GetDownloadURLsRequest{
		FileIds: ids,
	})
	if err != nil {
		return nil, mapGRPCError(err)
	}

	var out []client.DownloadURL
	for _, f := range resp.Files {
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
	resp, err := c.client.GetUploadURL(ctx, &storagev2.GetUploadURLRequest{
		Filename:    filename,
		ContentType: contentType,
	})
	if err != nil {
		return client.UploadURL{}, mapGRPCError(err)
	}
	return client.UploadURL{
		UploadURL: resp.UploadUrl,
		FileID:    resp.FileId,
	}, nil
}

func mapGRPCError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return fmt.Errorf("not found: %s", st.Message())
	case codes.InvalidArgument:
		return fmt.Errorf("invalid argument: %s", st.Message())
	default:
		return fmt.Errorf("storage error: %s", st.Message())
	}
}

func protoToDomain(f *storagev2.FileMetadata) domain.File {
	t := f.CreatedAt.AsTime()
	return domain.File{
		ID:          f.Id,
		Name:        f.Name,
		Size:        f.Size,
		ContentType: f.ContentType,
		CreatedAt:   t,
	}
}
