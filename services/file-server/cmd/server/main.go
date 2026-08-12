package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client/grpc"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/config"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/handler/file"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/handler/health"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/service"
)

func main(){

	config, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	storageClient, closeConn, err := grpc.NewStorageClient(config.StorageGRPCTarget)
	if err != nil {
		log.Fatalf("failed to connect to storage service: %v", err)
	}
	defer closeConn()

	fileSvc := service.NewFileService(storageClient, config.MaxUploadSize)

	fileHandler := file.NewFileHandler(fileSvc, config.MaxMultipartMemory)
	healthHandler := health.NewHealthHandler()

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /health", healthHandler.Health)

	// Files
	mux.HandleFunc("POST /files", fileHandler.Upload)
	mux.HandleFunc("GET /files", fileHandler.List)
	mux.HandleFunc("GET /files/{id}/download", fileHandler.Download)
	mux.HandleFunc("POST /files/download-many", fileHandler.DownloadMultiple)
	mux.HandleFunc("GET /files/{id}/metadata", fileHandler.Metadata)

	server := &http.Server{
		Addr:    ":" + config.ServerPort,
		Handler: stripTrailingSlash(mux),
	}

	log.Println("file-server running on " + config.ServerPort)

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