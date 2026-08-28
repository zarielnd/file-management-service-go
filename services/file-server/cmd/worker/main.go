package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	connectrpc "github.com/zarielnd/file-management-service-go/services/file-server/internal/client/grpc"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/config"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/observability"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/temporal/activities"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/temporal/workflows"
	"go.temporal.io/sdk/client"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
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
		ServiceName:    "file-server-worker",
		ServiceVersion: "1.0.0",
		Environment:    cfg.Environment, // add to config.Config if you want
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

	// ---- 3. Temporal client (no interceptors here) ----
	// Temporal tracing is configured on the worker interceptor.
	c, err := client.Dial(client.Options{
		HostPort: cfg.TemporalHost,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to create temporal client", "error", err)
		os.Exit(1)
	}
	defer c.Close()

	// ---- 4. Tracing interceptor goes on the WORKER ----
	traceInterceptor, err := temporalotel.NewTracingInterceptor(temporalotel.TracerOptions{
		Tracer: observability.Tracer(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to create tracing interceptor", "error", err)
		os.Exit(1)
	}

	w := worker.New(c, cfg.TemporalQueue, worker.Options{
		EnableSessionWorker: true,
		Interceptors:        []interceptor.WorkerInterceptor{traceInterceptor},
	})

	// ---- 5. Storage client ----
	storageClient, closeConn, err := connectrpc.NewStorageClient(cfg.StorageGRPCTarget, cfg.ServiceKey)
	if err != nil {
		slog.ErrorContext(ctx, "failed to connect to storage service", "error", err)
		os.Exit(1)
	}
	defer closeConn()

	acts := activities.NewActivities(storageClient)
	w.RegisterWorkflow(workflows.BulkDownloadWorkflow)
	w.RegisterActivity(acts)

	slog.InfoContext(ctx, "temporal worker starting",
		"queue", cfg.TemporalQueue,
		"target", cfg.StorageGRPCTarget,
	)

	if err := w.Run(worker.InterruptCh()); err != nil {
		slog.ErrorContext(ctx, "worker failed", "error", err)
		os.Exit(1)
	}
}
