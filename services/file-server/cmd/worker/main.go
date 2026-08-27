package main

import (
	"log"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client/grpc"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/config"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/temporal/activities"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/temporal/workflows"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	c, err := client.Dial(client.Options{
		HostPort: cfg.TemporalHost,
	})
	if err != nil {
		log.Fatalf("failed to create temporal client: %v", err)
	}
	defer c.Close()

	w := worker.New(c, cfg.TemporalQueue, worker.Options{EnableSessionWorker: true})

	storageClient, closeConn, err := grpc.NewStorageClient(cfg.StorageGRPCTarget, cfg.ServiceKey)
	if err != nil {
		log.Fatalf("failed to connect to storage service: %v", err)
	}
	defer closeConn()
	acts := activities.NewActivities(storageClient)
	w.RegisterWorkflow(workflows.BulkDownloadWorkflow)
	w.RegisterActivity(acts)

	log.Printf("temporal worker starting: queue=%s target=%s", cfg.TemporalQueue, cfg.StorageGRPCTarget)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("worker failed: %v", err)
	}
}
