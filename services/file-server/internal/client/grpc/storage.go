package grpc

import (
	storagev1 "github.com/zarielnd/file-management-service-go/gen/storage/v1"
)

type storageClient struct {
	client storagev1.StorageServiceClient
}