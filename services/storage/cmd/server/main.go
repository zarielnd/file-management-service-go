package server

import (
	"log"
	"net"

	storagev1 "github.com/zarielnd/file-management-service-go/gen/storage/v1"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/config"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/repository/postgres"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/server"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/service"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/storage/local"
	"google.golang.org/grpc"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	repo, err := postgres.NewRepository(cfg.DBConnectionString)
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	defer repo.Close()

	storageProvider := local.NewStore(cfg.StoragePath)
	fileSvc := service.NewFileService(repo, storageProvider, cfg.StoragePath,cfg.TempPath)

	grpcServer := server.NewGRPCServer(fileSvc)

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	storagev1.RegisterStorageServiceServer(s, grpcServer)

	log.Printf("storage service running on :%s", cfg.GRPCPort)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}