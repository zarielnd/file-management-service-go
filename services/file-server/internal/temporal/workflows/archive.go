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
}

type ArchiveResult struct {
	ArchiveFileID string `json:"archive_file_id"`
	SizeBytes     int64  `json:"size_bytes"`
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

	// 1. Resolve presigned URLs
	var resolved []activities.ResolvedFile
	if err := workflow.ExecuteActivity(ctx, activities.ResolveFilesActivityName, req.FileIDs).Get(ctx, &resolved); err != nil {
		return nil, fmt.Errorf("resolve files: %w", err)
	}

	// 2. Download files in parallel
	dlCtx, _ := workflow.NewDisconnectedContext(ctx)
	futures := make([]workflow.Future, len(resolved))
	for i, file := range resolved {
		futures[i] = workflow.ExecuteActivity(dlCtx, activities.DownloadFileActivityName, activities.DownloadFileInput{
			URL:      file.URL,
			TempPath: fmt.Sprintf("/tmp/archives/%s/%s", req.ArchiveID, file.FileID),
		})
	}
	for i, f := range futures {
		if err := f.Get(ctx, nil); err != nil {
			return nil, fmt.Errorf("download %s: %w", resolved[i].FileID, err)
		}
	}

	// 3. Zip
	zipPath := fmt.Sprintf("/tmp/archives/%s/archive.zip", req.ArchiveID)
	if err := workflow.ExecuteActivity(ctx, activities.ZipFilesActivityName, activities.ZipInput{
		Files:      resolved,
		TempDir:    fmt.Sprintf("/tmp/archives/%s", req.ArchiveID),
		OutputPath: zipPath,
	}).Get(ctx, nil); err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}

	// 4. Upload archive
	var archiveID string
	if err := workflow.ExecuteActivity(ctx, activities.UploadArchiveActivityName, activities.UploadArchiveInput{
		ZipPath: zipPath,
		Name:    fmt.Sprintf("archive-%s.zip", req.ArchiveID),
	}).Get(ctx, &archiveID); err != nil {
		return nil, fmt.Errorf("upload archive: %w", err)
	}

	// 5. Cleanup
	_ = workflow.ExecuteActivity(ctx, activities.CleanupActivityName, fmt.Sprintf("/tmp/archives/%s", req.ArchiveID)).Get(ctx, nil)

	return &ArchiveResult{
		ArchiveFileID: archiveID,
	}, nil
}
