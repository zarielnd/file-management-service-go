package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/auth"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client/grpc"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/config"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/handler/file"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/handler/health"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/service"
	temporalClient "go.temporal.io/sdk/client"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	authRepo := auth.NewPostgresRepository(db)
	authSvc := auth.NewService(authRepo, redisClient, cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	authHandler := auth.NewHandler(authSvc)

	storageClient, closeConn, err := grpc.NewStorageClient(cfg.StorageGRPCTarget, cfg.ServiceKey)
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
	archiveWS := file.NewArchiveWSHandler(tc)

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

	server := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: stripTrailingSlash(mux),
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
