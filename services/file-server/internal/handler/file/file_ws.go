package file

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"go.temporal.io/sdk/client"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/auth"
	grpcClient "github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/temporal/workflows"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type ArchiveWSHandler struct {
	temporalClient client.Client
	authService    *auth.Service
}

func NewArchiveWSHandler(tc client.Client, authSvc *auth.Service) *ArchiveWSHandler {
	return &ArchiveWSHandler{
		temporalClient: tc,
		authService:    authSvc,
	}
}

func (h *ArchiveWSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Read access_token cookie BEFORE upgrade
	cookie, err := r.Cookie("access_token")
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// 2. Validate it
	userID, err := h.authService.ValidateAccessToken(r.Context(), cookie.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

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

	ctx := grpcClient.WithUserID(r.Context(), userID)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	run := h.temporalClient.GetWorkflow(ctx, workflowID, "")
	var result workflows.ArchiveResult
	err = run.Get(ctx, &result)

	if ctx.Err() != nil {
		return
	}

	if err != nil {
		writeJSON(conn, map[string]string{
			"event":   "error",
			"message": err.Error(),
		})
		return
	}

	if err := writeJSON(conn, map[string]string{
		"event":        "completed",
		"download_url": result.DownloadURL,
	}); err != nil {
		return
	}
}

func writeJSON(conn *websocket.Conn, v interface{}) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, buf.Bytes())
}
