package file

import (
	"context"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"go.temporal.io/sdk/client"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/temporal/workflows"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // tighten in prod
}

type ArchiveWSHandler struct {
	temporalClient client.Client
}

func NewArchiveWSHandler(tc client.Client) *ArchiveWSHandler {
	return &ArchiveWSHandler{temporalClient: tc}
}

func (h *ArchiveWSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	archiveID := r.PathValue("id")
	if archiveID == "" {
		http.Error(w, "missing archive id", http.StatusBadRequest)
		return
	}
	workflowID := "archive-" + archiveID

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	// Context cancels if client disconnects
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Goroutine: detect client disconnect by reading from WS
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel() // stop waiting for workflow
				return
			}
		}
	}()

	// Block until workflow completes
	run := h.temporalClient.GetWorkflow(ctx, workflowID, "")
	var result workflows.ArchiveResult
	err = run.Get(ctx, &result)

	// Client disconnected while waiting
	if ctx.Err() != nil {
		return
	}

	// Workflow failed
	if err != nil {
		conn.WriteJSON(map[string]string{
			"event":   "error",
			"message": err.Error(),
		})
		return
	}

	// Success
	conn.WriteJSON(map[string]string{
		"event":      "completed",
		"archive_id": result.ArchiveFileID,
	})
}
