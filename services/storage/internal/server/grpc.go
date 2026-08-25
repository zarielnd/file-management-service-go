package server

import (
	"context"
	"fmt"
	"io"
	"time"

	storagev2 "github.com/zarielnd/file-management-service-go/gen/storage/v2"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type GRPCServer struct {
	storagev2.UnimplementedStorageServiceServer
	service *service.FileService
}

func NewGRPCServer(service *service.FileService) *GRPCServer {
	return &GRPCServer{service: service}
}

func (s *GRPCServer) UploadFile(stream storagev2.StorageService_UploadFileServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}

	infoPayload, ok := req.Payload.(*storagev2.UploadFileRequest_Info)
	if !ok {
		return status.Error(codes.InvalidArgument, "first message must be FileInfo")
	}

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			chunk := req.GetChunkData()
			if _, err := pw.Write(chunk); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()

	file, err := s.service.Store(stream.Context(), infoPayload.Info.Filename, infoPayload.Info.ContentType, pr)
	if err != nil {
		return mapError(err)
	}

	return stream.SendAndClose(&storagev2.UploadFileResponse{
		File: &storagev2.FileMetadata{
			Id:          file.ID,
			Name:        file.Name,
			Size:        file.SizeBytes,
			ContentType: file.ContentType,
			Checksum:    file.Checksum,
			CreatedAt:   timestamppb.New(file.CreatedAt),
		},
	})
}

func (s *GRPCServer) GetFile(req *storagev2.GetFileRequest, stream storagev2.StorageService_GetFileServer) error {
	rc, file, err := s.service.Fetch(stream.Context(), req.Id)
	if err != nil {
		return mapError(err)
	}
	defer rc.Close()

	if err := stream.Send(&storagev2.GetFileResponse{
		Metadata: &storagev2.FileMetadata{
			Id:          file.ID,
			Name:        file.Name,
			Size:        file.SizeBytes,
			ContentType: file.ContentType,
			CreatedAt:   timestamppb.New(file.CreatedAt),
		},
	}); err != nil {
		return err
	}

	buf := make([]byte, 64*1024)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if err := stream.Send(&storagev2.GetFileResponse{Data: buf[:n]}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *GRPCServer) GetMetadata(ctx context.Context, req *storagev2.GetMetadataRequest) (*storagev2.FileMetadata, error) {
	file, err := s.service.Metadata(ctx, req.Id)
	if err != nil {
		return nil, mapError(err)
	}
	return &storagev2.FileMetadata{
		Id:          file.ID,
		Name:        file.Name,
		Size:        file.SizeBytes,
		ContentType: file.ContentType,
		Checksum:    file.Checksum,
		CreatedAt:   timestamppb.New(file.CreatedAt),
	}, nil
}

func (s *GRPCServer) ListFiles(ctx context.Context, req *storagev2.ListFilesRequest) (*storagev2.ListFilesResponse, error) {
	files, total, err := s.service.List(ctx, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, mapError(err)
	}

	resp := &storagev2.ListFilesResponse{
		Files: make([]*storagev2.FileMetadata, 0, len(files)),
		Total: int32(total),
	}
	for _, f := range files {
		resp.Files = append(resp.Files, &storagev2.FileMetadata{
			Id:          f.ID,
			Name:        f.Name,
			Size:        f.SizeBytes,
			ContentType: f.ContentType,
			Checksum:    f.Checksum,
			CreatedAt:   timestamppb.New(f.CreatedAt),
		})
	}
	return resp, nil
}

func (s *GRPCServer) DownloadArchive(req *storagev2.DownloadArchiveRequest, stream storagev2.StorageService_DownloadArchiveServer) error {
	rc, err := s.service.DownloadArchive(stream.Context(), req.FileIds)
	if err != nil {
		return mapError(err)
	}
	defer rc.Close()

	buf := make([]byte, 64*1024)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if err := stream.Send(&storagev2.DownloadArchiveResponse{Data: buf[:n]}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *GRPCServer) GetDownloadURLs(ctx context.Context, req *storagev2.GetDownloadURLsRequest) (*storagev2.GetDownloadURLsResponse, error) {
	files, err := s.service.GetByIDs(ctx, req.FileIds)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if len(files) != len(req.FileIds) {
		return nil, status.Error(codes.NotFound, "one or more files not found")
	}

	resp := &storagev2.GetDownloadURLsResponse{}
	for _, f := range files {
		url, err := s.service.PresignFetch(ctx, f.ID, 15*time.Minute)
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("presign %s: %v", f.ID, err))
		}
		resp.Files = append(resp.Files, &storagev2.FileDownloadURL{
			FileId:    f.ID,
			Name:      f.Name,
			Url:       url,
			SizeBytes: f.SizeBytes,
		})
	}
	return resp, nil
}

func (s *GRPCServer) GetUploadURL(ctx context.Context, req *storagev2.GetUploadURLRequest) (*storagev2.GetUploadURLResponse, error) {
	file, url, err := s.service.ReserveUpload(ctx, req.Filename, req.ContentType)
	if err != nil {
		return nil, mapError(err)
	}
	return &storagev2.GetUploadURLResponse{
		UploadUrl: url,
		FileId:    file.ID,
	}, nil
}

func (s *GRPCServer) ConfirmUpload(ctx context.Context, req *storagev2.ConfirmUploadRequest) (*storagev2.FileMetadata, error) {
	file, err := s.service.ConfirmUpload(ctx, req.FileId, req.SizeBytes, req.Checksum)
	if err != nil {
		return nil, mapError(err)
	}

	return &storagev2.FileMetadata{
		Id:          file.ID,
		Name:        file.Name,
		Size:        file.SizeBytes,
		ContentType: file.ContentType,
		Checksum:    file.Checksum,
		CreatedAt:   timestamppb.New(file.CreatedAt),
	}, nil
}

func mapError(err error) error {
	// TODO: inspect error types and map properly
	return status.Error(codes.Internal, err.Error())
}
