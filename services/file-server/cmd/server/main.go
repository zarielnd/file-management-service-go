package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"

	"github.com/XSAM/otelsql"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/auth"
	connectrpc "github.com/zarielnd/file-management-service-go/services/file-server/internal/client/grpc"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/config"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/handler/file"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/handler/file_ws"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/handler/health"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/observability"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/service"
	temporalClient "go.temporal.io/sdk/client"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	shutdown, err := observability.Init(ctx, observability.InitOptions{
		ServiceName:    "file-server",
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

	db, err := otelsql.Open("pgx", cfg.DatabaseURL,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL, attribute.String("db.name", cfg.DatabaseName)),
	)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})

	if err := redisotel.InstrumentTracing(redisClient); err != nil {
		log.Fatalf("failed to instrument redis tracing: %v", err)
	}

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	authRepo := auth.NewPostgresRepository(db)
	authSvc := auth.NewService(authRepo, redisClient, cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	authHandler := auth.NewHandler(authSvc)

	storageClient, closeConn, err := connectrpc.NewStorageClient(cfg.StorageGRPCTarget, cfg.ServiceKey)
	if err != nil {
		log.Fatalf("failed to connect to storage service: %v", err)
	}
	defer closeConn()

	tc, err := temporalClient.Dial(temporalClient.Options{HostPort: cfg.TemporalHost})
	if err != nil {
		log.Fatalf("failed to connect to temporal: %v", err)
	}
	defer tc.Close()

	fileSvc := service.NewFileService(storageClient, tc, cfg.TemporalQueue, cfg.MaxUploadSize)
	fileHandler := file.NewFileHandler(fileSvc, cfg, cfg.MaxMultipartMemory)
	healthHandler := health.NewHealthHandler()
	archiveWS := file_ws.NewArchiveWSHandler(tc, authSvc)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler.Health)

	mux.HandleFunc("POST /auth/signup", authHandler.SignUp)
	mux.HandleFunc("POST /auth/signin", authHandler.SignIn)
	mux.HandleFunc("POST /auth/refresh", authHandler.Refresh)

	requireAuth := authSvc.RequireAuth

	mux.HandleFunc("POST /auth/logout", requireAuth(http.HandlerFunc(authHandler.Logout)).ServeHTTP)
	mux.HandleFunc("POST /auth/logout-all", requireAuth(http.HandlerFunc(authHandler.LogoutAll)).ServeHTTP)

	mux.HandleFunc("POST /files", requireAuth(http.HandlerFunc(fileHandler.Upload)).ServeHTTP)
	mux.HandleFunc("GET /files", requireAuth(http.HandlerFunc(fileHandler.List)).ServeHTTP)
	mux.HandleFunc("GET /files/{id}/download", requireAuth(http.HandlerFunc(fileHandler.Download)).ServeHTTP)
	mux.HandleFunc("POST /files/download-many", requireAuth(http.HandlerFunc(fileHandler.DownloadMultiple)).ServeHTTP)
	mux.HandleFunc("GET /files/{id}/metadata", requireAuth(http.HandlerFunc(fileHandler.Metadata)).ServeHTTP)
	mux.HandleFunc("GET /api/archives/{id}/ws", requireAuth(http.HandlerFunc(archiveWS.ServeHTTP)).ServeHTTP)

	handler := observability.HTTPMiddleware(
		stripTrailingSlash(mux),
	)

	server := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: handler,
	}

	log.Println("file-server running on " + cfg.ServerPort)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

func stripTrailingSlash(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, strings.TrimSuffix(r.URL.Path, "/"), http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}
