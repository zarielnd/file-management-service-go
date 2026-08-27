package main

import (
	"context"
	"log"
	"net"
	"os"

	storagev2 "github.com/zarielnd/file-management-service-go/gen/storage/v2"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/config"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/repository/postgres"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/server"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/service"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	provider, err := storage.NewProviderFromConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("failed to init storage: %v", err)
	}

	repo, err := postgres.NewRepository(cfg.DBConnectionString)
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	defer repo.Close()

	fileSvc := service.NewFileService(repo, provider, cfg.StoragePath, cfg.TempPath)

	serviceKey := os.Getenv("SERVICE_KEY")
	if serviceKey == "" {
		log.Println("WARNING: SERVICE_KEY not set, gRPC auth disabled")
	}

	grpcServer := server.NewGRPCServer(fileSvc, serviceKey)

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(s, healthServer)

	storagev2.RegisterStorageServiceServer(s, grpcServer)

	log.Printf("storage service running on :%s", cfg.GRPCPort)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
