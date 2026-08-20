package server

import (
	"context"
	"io"

	storagev1 "github.com/zarielnd/file-management-service-go/gen/storage/v1"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type GRPCServerV1 struct {
	storagev1.UnimplementedStorageServiceServer
	service *service.FileService
}

func NewGRPCServerV1(service *service.FileService) *GRPCServerV1 {
	return &GRPCServerV1{service: service}
}

func (s *GRPCServerV1) UploadFile(stream storagev1.StorageService_UploadFileServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}

	infoPayload, ok := req.Payload.(*storagev1.UploadFileRequest_Info)
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

	return stream.SendAndClose(&storagev1.UploadFileResponse{
		File: &storagev1.FileMetadata{
			Id:          file.ID,
			Name:        file.Name,
			Size:        file.SizeBytes,
			ContentType: file.ContentType,
			Checksum:    file.Checksum,
			CreatedAt:   timestamppb.New(file.CreatedAt),
		},
	})
}

func (s *GRPCServerV1) GetFile(req *storagev1.GetFileRequest, stream storagev1.StorageService_GetFileServer) error {
	rc, file, err := s.service.Fetch(stream.Context(), req.Id)
	if err != nil {
		return mapError(err)
	}
	defer rc.Close()

	if err := stream.Send(&storagev1.GetFileResponse{
		Metadata: &storagev1.FileMetadata{
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
			if err := stream.Send(&storagev1.GetFileResponse{Data: buf[:n]}); err != nil {
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

func (s *GRPCServerV1) GetMetadata(ctx context.Context, req *storagev1.GetMetadataRequest) (*storagev1.FileMetadata, error) {
	file, err := s.service.Metadata(ctx, req.Id)
	if err != nil {
		return nil, mapError(err)
	}
	return &storagev1.FileMetadata{
		Id:          file.ID,
		Name:        file.Name,
		Size:        file.SizeBytes,
		ContentType: file.ContentType,
		Checksum:    file.Checksum,
		CreatedAt:   timestamppb.New(file.CreatedAt),
	}, nil
}

func (s *GRPCServerV1) ListFiles(ctx context.Context, req *storagev1.ListFilesRequest) (*storagev1.ListFilesResponse, error) {
	files, total, err := s.service.List(ctx, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, mapError(err)
	}

	resp := &storagev1.ListFilesResponse{
		Files: make([]*storagev1.FileMetadata, 0, len(files)),
		Total: int32(total),
	}
	for _, f := range files {
		resp.Files = append(resp.Files, &storagev1.FileMetadata{
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

func (s *GRPCServerV1) DownloadArchive(req *storagev1.DownloadArchiveRequest, stream storagev1.StorageService_DownloadArchiveServer) error {
	rc, err := s.service.DownloadArchive(stream.Context(), req.FileIds)
	if err != nil {
		return mapError(err)
	}
	defer rc.Close()

	buf := make([]byte, 64*1024)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if err := stream.Send(&storagev1.DownloadArchiveResponse{Data: buf[:n]}); err != nil {
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

func mapError(err error) error {
	// TODO: inspect error types and map properly
	return status.Error(codes.Internal, err.Error())
}
