package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/temporal/activities"
)

type ArchiveRequest struct {
	FileIDs   []string `json:"file_ids"`
	ArchiveID string   `json:"archive_id"`
	UserID    string   `json:"user_id"` // NEW
}

type ArchiveResult struct {
	DownloadURL string `json:"download_url"`
}

func BulkDownloadWorkflow(ctx workflow.Context, req ArchiveRequest) (*ArchiveResult, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        time.Second,
			BackoffCoefficient:     2.0,
			MaximumAttempts:        3,
			NonRetryableErrorTypes: []string{"NotFound", "InvalidArgument"},
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// 1. Resolve presigned URLs (pass UserID)
	var resolved []activities.ResolvedFile
	if err := workflow.ExecuteActivity(ctx, activities.ResolveFilesActivityName, req.UserID, req.FileIDs).Get(ctx, &resolved); err != nil {
		return nil, fmt.Errorf("resolve files: %w", err)
	}

	// Session for single-worker execution
	sessionCtx, err := workflow.CreateSession(ctx, &workflow.SessionOptions{
		CreationTimeout:  time.Minute,
		ExecutionTimeout: 30 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer workflow.CompleteSession(sessionCtx)

	tempDir := fmt.Sprintf("/tmp/archives/%s", req.ArchiveID)

	defer func() {
		cleanupCtx, cancel := workflow.NewDisconnectedContext(sessionCtx)
		defer cancel()
		_ = workflow.ExecuteActivity(cleanupCtx, activities.CleanupActivityName, tempDir).Get(cleanupCtx, nil)
	}()

	// Downloads
	futures := make([]workflow.Future, len(resolved))
	for i, file := range resolved {
		futures[i] = workflow.ExecuteActivity(sessionCtx, activities.DownloadFileActivityName, activities.DownloadFileInput{
			URL:      file.URL,
			TempPath: fmt.Sprintf("%s/%s", tempDir, file.FileID),
		})
	}
	for i, f := range futures {
		if err := f.Get(sessionCtx, nil); err != nil {
			return nil, fmt.Errorf("download %s: %w", resolved[i].FileID, err)
		}
	}

	// Zip
	zipPath := fmt.Sprintf("%s/archive.zip", tempDir)
	if err := workflow.ExecuteActivity(sessionCtx, activities.ZipFilesActivityName, activities.ZipInput{
		Files: resolved, TempDir: tempDir, OutputPath: zipPath,
	}).Get(sessionCtx, nil); err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}

	// Upload (pass UserID)
	var downloadURL string
	if err := workflow.ExecuteActivity(sessionCtx, activities.UploadArchiveActivityName, activities.UploadArchiveInput{
		ZipPath: zipPath,
		Name:    fmt.Sprintf("archive-%s.zip", req.ArchiveID),
		UserID:  req.UserID, // NEW
	}).Get(sessionCtx, &downloadURL); err != nil {
		return nil, fmt.Errorf("upload archive: %w", err)
	}

	return &ArchiveResult{DownloadURL: downloadURL}, nil
}
