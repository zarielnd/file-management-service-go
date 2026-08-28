package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	storagev2 "github.com/zarielnd/file-management-service-go/gen/storage/v2"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/repository"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/service"
)

type ConnectServer struct {
	service    *service.FileService
	serviceKey string
}

func NewConnectServer(service *service.FileService, serviceKey string) *ConnectServer {
	return &ConnectServer{service: service, serviceKey: serviceKey}
}

func userIDFromHeader(header http.Header) string {
	return header.Get("X-User-Id")
}

func validateServiceKey(header http.Header, expectedKey string) error {
	if expectedKey == "" {
		return nil
	}
	if header.Get("X-Service-Key") != expectedKey {
		return connect.NewError(connect.CodePermissionDenied, errors.New("invalid service key"))
	}
	return nil
}

func (s *ConnectServer) UploadFile(ctx context.Context, stream *connect.ClientStream[storagev2.UploadFileRequest]) (*connect.Response[storagev2.UploadFileResponse], error) {
	if err := validateServiceKey(stream.RequestHeader(), s.serviceKey); err != nil {
		return nil, err
	}

	if !stream.Receive() {
		if err := stream.Err(); err != nil {
			return nil, err
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("empty upload stream"))
	}

	infoPayload, ok := stream.Msg().Payload.(*storagev2.UploadFileRequest_Info)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("first message must be FileInfo"))
	}
	info := infoPayload.Info

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for stream.Receive() {
			chunk := stream.Msg().GetChunkData()
			if _, err := pw.Write(chunk); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		if err := stream.Err(); err != nil {
			pw.CloseWithError(err)
		}
	}()

	file, err := s.service.Store(ctx, info.Filename, info.ContentType, info.OwnerId, pr)
	if err != nil {
		return nil, mapError(err)
	}

	return connect.NewResponse(&storagev2.UploadFileResponse{
		File: domainToProto(file),
	}), nil
}

func (s *ConnectServer) GetFile(ctx context.Context, req *connect.Request[storagev2.GetFileRequest], stream *connect.ServerStream[storagev2.GetFileResponse]) error {
	if err := validateServiceKey(req.Header(), s.serviceKey); err != nil {
		return err
	}
	userID := userIDFromHeader(req.Header())
	if userID == "" {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("missing user id"))
	}

	rc, file, err := s.service.Fetch(ctx, req.Msg.Id, userID)
	if err != nil {
		return mapError(err)
	}
	defer rc.Close()

	if err := stream.Send(&storagev2.GetFileResponse{
		Metadata: domainToProto(file),
	}); err != nil {
		return err
	}

	buf := make([]byte, 64*1024)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&storagev2.GetFileResponse{Data: buf[:n]}); sendErr != nil {
				return sendErr
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

func (s *ConnectServer) GetMetadata(ctx context.Context, req *connect.Request[storagev2.GetMetadataRequest]) (*connect.Response[storagev2.FileMetadata], error) {
	if err := validateServiceKey(req.Header(), s.serviceKey); err != nil {
		return nil, err
	}
	userID := userIDFromHeader(req.Header())
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing user id"))
	}

	file, err := s.service.Metadata(ctx, req.Msg.Id, userID)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(domainToProto(file)), nil
}

func (s *ConnectServer) ListFiles(ctx context.Context, req *connect.Request[storagev2.ListFilesRequest]) (*connect.Response[storagev2.ListFilesResponse], error) {
	if err := validateServiceKey(req.Header(), s.serviceKey); err != nil {
		return nil, err
	}
	userID := userIDFromHeader(req.Header())
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing user id"))
	}

	files, total, err := s.service.List(ctx, int(req.Msg.Page), int(req.Msg.PageSize), userID)
	if err != nil {
		return nil, mapError(err)
	}

	resp := &storagev2.ListFilesResponse{
		Files: make([]*storagev2.FileMetadata, 0, len(files)),
		Total: int32(total),
	}
	for _, f := range files {
		resp.Files = append(resp.Files, domainToProto(*f))
	}
	return connect.NewResponse(resp), nil
}

func (s *ConnectServer) DownloadArchive(ctx context.Context, req *connect.Request[storagev2.DownloadArchiveRequest], stream *connect.ServerStream[storagev2.DownloadArchiveResponse]) error {
	if err := validateServiceKey(req.Header(), s.serviceKey); err != nil {
		return err
	}
	userID := userIDFromHeader(req.Header())
	if userID == "" {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("missing user id"))
	}

	rc, err := s.service.DownloadArchive(ctx, req.Msg.FileIds, userID)
	if err != nil {
		return mapError(err)
	}
	defer rc.Close()

	buf := make([]byte, 64*1024)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&storagev2.DownloadArchiveResponse{Data: buf[:n]}); sendErr != nil {
				return sendErr
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

func (s *ConnectServer) GetDownloadURLs(ctx context.Context, req *connect.Request[storagev2.GetDownloadURLsRequest]) (*connect.Response[storagev2.GetDownloadURLsResponse], error) {
	if err := validateServiceKey(req.Header(), s.serviceKey); err != nil {
		return nil, err
	}
	userID := userIDFromHeader(req.Header())
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing user id"))
	}

	files, err := s.service.GetByIDs(ctx, req.Msg.FileIds, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if len(files) != len(req.Msg.FileIds) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("one or more files not found"))
	}

	resp := &storagev2.GetDownloadURLsResponse{}
	for _, f := range files {
		url, err := s.service.PresignFetch(ctx, f.ID, userID, 15*time.Minute)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("presign %s: %w", f.ID, err))
		}
		resp.Files = append(resp.Files, &storagev2.FileDownloadURL{
			FileId:    f.ID,
			Name:      f.Name,
			Url:       url,
			SizeBytes: f.SizeBytes,
		})
	}
	return connect.NewResponse(resp), nil
}

func (s *ConnectServer) GetUploadURL(ctx context.Context, req *connect.Request[storagev2.GetUploadURLRequest]) (*connect.Response[storagev2.GetUploadURLResponse], error) {
	if err := validateServiceKey(req.Header(), s.serviceKey); err != nil {
		return nil, err
	}
	userID := userIDFromHeader(req.Header())
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing user id"))
	}

	file, url, err := s.service.ReserveUpload(ctx, req.Msg.Filename, req.Msg.ContentType, userID)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&storagev2.GetUploadURLResponse{
		UploadUrl: url,
		FileId:    file.ID,
	}), nil
}

func (s *ConnectServer) ConfirmUpload(ctx context.Context, req *connect.Request[storagev2.ConfirmUploadRequest]) (*connect.Response[storagev2.FileMetadata], error) {
	if err := validateServiceKey(req.Header(), s.serviceKey); err != nil {
		return nil, err
	}
	userID := userIDFromHeader(req.Header())
	if userID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing user id"))
	}
	file, err := s.service.ConfirmUpload(ctx, req.Msg.FileId, req.Msg.SizeBytes, req.Msg.Checksum, userID)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(domainToProto(*file)), nil
}

func (s *ConnectServer) GetArchiveUploadURL(ctx context.Context, req *connect.Request[storagev2.GetArchiveUploadURLRequest]) (*connect.Response[storagev2.GetArchiveUploadURLResponse], error) {
	if err := validateServiceKey(req.Header(), s.serviceKey); err != nil {
		return nil, err
	}

	url, err := s.service.PresignArchiveStore(ctx, req.Msg.Path, req.Msg.ContentType)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&storagev2.GetArchiveUploadURLResponse{
		UploadUrl: url,
		Path:      req.Msg.Path,
	}), nil
}

func (s *ConnectServer) GetArchiveDownloadURL(ctx context.Context, req *connect.Request[storagev2.GetArchiveDownloadURLRequest]) (*connect.Response[storagev2.GetArchiveDownloadURLResponse], error) {
	if err := validateServiceKey(req.Header(), s.serviceKey); err != nil {
		return nil, err
	}

	url, err := s.service.PresignArchiveFetch(ctx, req.Msg.Path)
	if err != nil {
		return nil, mapError(err)
	}
	return connect.NewResponse(&storagev2.GetArchiveDownloadURLResponse{
		DownloadUrl: url,
	}), nil
}

func domainToProto(f repository.File) *storagev2.FileMetadata {
	return &storagev2.FileMetadata{
		Id:          f.ID,
		Name:        f.Name,
		Size:        f.SizeBytes,
		ContentType: f.ContentType,
		Checksum:    f.Checksum,
		CreatedAt:   timestamppb.New(f.CreatedAt),
		OwnerId:     f.OwnerID,
	}
}

func mapError(err error) error {
	return connect.NewError(connect.CodeInternal, err)
}
