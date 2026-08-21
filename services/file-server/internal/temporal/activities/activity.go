package activities

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	storagev2 "github.com/zarielnd/file-management-service-go/gen/storage/v2"
)

type Activities struct {
	storageAddr string
	httpClient  *http.Client
}

func NewActivities(storageAddr string) *Activities {
	return &Activities{
		storageAddr: storageAddr,
		httpClient:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (a *Activities) storageClient(ctx context.Context) (storagev2.StorageServiceClient, func(), error) {
	conn, err := grpc.DialContext(ctx, a.storageAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() { conn.Close() }
	return storagev2.NewStorageServiceClient(conn), closeFn, nil
}

// Activity 1: Resolve presigned URLs via Storage Service gRPC
func (a *Activities) ResolveFilesActivity(ctx context.Context, fileIDs []string) ([]ResolvedFile, error) {
	client, closeFn, err := a.storageClient(ctx)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	resp, err := client.GetDownloadURLs(ctx, &storagev2.GetDownloadURLsRequest{FileIds: fileIDs})
	if err != nil {
		return nil, fmt.Errorf("GetDownloadURLs: %w", err)
	}

	var out []ResolvedFile
	for _, f := range resp.Files {
		out = append(out, ResolvedFile{
			FileID:    f.FileId,
			Name:      f.Name,
			URL:       f.Url,
			SizeBytes: f.SizeBytes,
		})
	}
	return out, nil
}

// Activity 2: Download single file via HTTP directly to S3/MinIO
func (a *Activities) DownloadFileActivity(ctx context.Context, input DownloadFileInput) error {
	if err := os.MkdirAll(filepath.Dir(input.TempPath), 0755); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.URL, nil)
	if err != nil {
		return err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	f, err := os.Create(input.TempPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// Activity 3: Zip downloaded files
func (a *Activities) ZipFilesActivity(ctx context.Context, input ZipInput) error {
	if err := os.MkdirAll(filepath.Dir(input.OutputPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(input.OutputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for _, file := range input.Files {
		src, err := os.Open(filepath.Join(input.TempDir, file.FileID))
		if err != nil {
			return fmt.Errorf("open %s: %w", file.FileID, err)
		}

		w, err := zw.Create(file.Name)
		if err != nil {
			src.Close()
			return fmt.Errorf("zip create %s: %w", file.Name, err)
		}

		_, err = io.Copy(w, src)
		src.Close()
		if err != nil {
			return fmt.Errorf("zip copy %s: %w", file.Name, err)
		}
	}
	return zw.Close()
}

// Activity 4: Upload final archive back to Storage Service via gRPC streaming
func (a *Activities) UploadArchiveActivity(ctx context.Context, input UploadArchiveInput) (string, error) {
	// 1. Get presigned PUT URL from Storage Service (lightweight gRPC)
	resp, err := a.storageClient.GetUploadURL(ctx, input.Name, "application/zip")
	if err != nil {
		return "", err
	}

	// 2. Upload zip directly to S3 via HTTP PUT
	zipFile, _ := os.Open(input.ZipPath)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, resp.UploadUrl, zipFile)
	req.Header.Set("Content-Type", "application/zip")

	httpResp, err := a.httpClient.Do(req)
	if err != nil || httpResp.StatusCode != 200 {
		return "", fmt.Errorf("upload failed")
	}

	// 3. Return the file ID so user can download it later
	return resp.FileId, nil
}

// Activity 5: Cleanup temp files
func (a *Activities) CleanupActivity(ctx context.Context, tempDir string) error {
	return os.RemoveAll(tempDir)
}
