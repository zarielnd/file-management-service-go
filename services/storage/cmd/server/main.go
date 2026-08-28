package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"connectrpc.com/otelconnect"
	storagev2connect "github.com/zarielnd/file-management-service-go/gen/storage/v2/storagev2connect"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/config"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/observability"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/repository/postgres"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/server"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/service"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/storage"
)

func main() {
	ctx := context.Background()

	// ---- 1. Config ----
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// ---- 2. Observability ----
	shutdown, err := observability.Init(ctx, observability.InitOptions{
		ServiceName:    "storage-service",
		ServiceVersion: "1.0.0",
		Environment:    cfg.Environment,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to init observability", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			slog.ErrorContext(ctx, "observability shutdown error", "error", err)
		}
	}()

	// ---- 3. Storage provider ----
	provider, err := storage.NewProviderFromConfig(ctx, cfg)
	if err != nil {
		slog.ErrorContext(ctx, "failed to init storage", "error", err)
		os.Exit(1)
	}

	// ---- 4. DB (see note below about otelsql) ----
	repo, err := postgres.NewRepository(ctx, cfg.DBConnectionString)
	if err != nil {
		slog.ErrorContext(ctx, "failed to connect to db", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	fileSvc := service.NewFileService(repo, provider, cfg.StoragePath, cfg.TempPath)

	serviceKey := os.Getenv("SERVICE_KEY")
	if serviceKey == "" {
		slog.WarnContext(ctx, "SERVICE_KEY not set, Connect auth disabled")
	}

	connectServer := server.NewConnectServer(fileSvc, serviceKey)

	// ---- 5. Connect handler with OTel interceptor ----
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		slog.ErrorContext(ctx, "failed to create otel interceptor", "error", err)
		os.Exit(1)
	}

	path, handler := storagev2connect.NewStorageServiceHandler(
		connectServer,
		connect.WithInterceptors(otelInterceptor),
	)

	mux := http.NewServeMux()
	mux.Handle(path, handler)

	checker := grpchealth.NewStaticChecker(storagev2connect.StorageServiceName)
	mux.Handle(grpchealth.NewHandler(checker))

	// ---- 6. HTTP server with observability middleware ----
	srv := &http.Server{
		Addr:    ":" + cfg.GRPCPort,
		Handler: mux,
	}
	srv.Protocols = new(http.Protocols)
	srv.Protocols.SetHTTP1(true)
	srv.Protocols.SetUnencryptedHTTP2(true)

	slog.InfoContext(ctx, "storage service running", "port", cfg.GRPCPort)
	if err := srv.ListenAndServe(); err != nil {
		slog.ErrorContext(ctx, "failed to serve", "error", err)
		os.Exit(1)
	}
}
