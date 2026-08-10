package main

import (
	"log"
	"net/http"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/handler"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/service"
)

func main(){

	storageClient, closeConn, err := grpclient.NewStorageClient("localhost:50051")
	if err != nil {
		log.Fatalf("failed to connect to storage service: %v", err)
	}
	defer closeConn()

	fileSvc := service.NewFileService(storageClient)

	fileHandler := handler.NewFileHandler(fileSvc)
	healthHandler := handler.NewHealthHandler()

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /health", healthHandler.Health)

	// Files
	mux.HandleFunc("POST /files", fileHandler.Upload)
	mux.HandleFunc("GET /files", fileHandler.List)
	mux.HandleFunc("GET /files/{id}", fileHandler.Download)
	mux.HandleFunc("POST /files/download", fileHandler.DownloadMultiple)
	mux.HandleFunc("GET /files/{id}/metadata", fileHandler.Metadata)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("file-server running on :8080")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}