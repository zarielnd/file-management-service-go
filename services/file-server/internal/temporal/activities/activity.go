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

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/client"
)

type Activities struct {
	storageClient client.StorageClient
	httpClient    *http.Client
}

func NewActivities(storageClient client.StorageClient) *Activities {
	return &Activities{
		storageClient: storageClient,
		httpClient:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (a *Activities) ResolveFilesActivity(ctx context.Context, userID string, fileIDs []string) ([]ResolvedFile, error) {
	ctx = client.WithUserID(ctx, userID)
	urls, err := a.storageClient.GetDownloadURLs(ctx, fileIDs)
	if err != nil {
		return nil, fmt.Errorf("GetDownloadURLs: %w", err)
	}

	var out []ResolvedFile
	for _, u := range urls {
		out = append(out, ResolvedFile{
			FileID:    u.FileID,
			Name:      u.Name,
			URL:       u.URL,
			SizeBytes: u.SizeBytes,
		})
	}
	return out, nil
}

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

func (a *Activities) UploadArchiveActivity(ctx context.Context, input UploadArchiveInput) (string, error) {
	ctx = client.WithUserID(ctx, input.UserID) // NEW: inject userID

	archivePath := fmt.Sprintf("archives/%s", input.Name)

	uploadURL, err := a.storageClient.GetArchiveUploadURL(ctx, archivePath, "application/zip")
	if err != nil {
		return "", fmt.Errorf("get archive upload url: %w", err)
	}

	zipFile, err := os.Open(input.ZipPath)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer zipFile.Close()

	stat, err := zipFile.Stat()
	if err != nil {
		return "", fmt.Errorf("stat zip: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, zipFile)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/zip")
	req.ContentLength = stat.Size()

	httpResp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload http: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return "", fmt.Errorf("upload failed: %d %s", httpResp.StatusCode, string(body))
	}

	downloadURL, err := a.storageClient.GetArchiveDownloadURL(ctx, archivePath)
	if err != nil {
		return "", fmt.Errorf("get archive download url: %w", err)
	}

	return downloadURL, nil
}

func (a *Activities) CleanupActivity(ctx context.Context, tempDir string) error {
	return os.RemoveAll(tempDir)
}
