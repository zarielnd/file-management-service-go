package main

import (
	"log"
	"net/http"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/handler"
)

func main(){
	mux := http.NewServeMux()

	fileHandler := handler.NewFileHandler()
	healthHandler := handler.NewHealthHandler()

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