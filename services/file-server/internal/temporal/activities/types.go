package activities

type ResolvedFile struct {
	FileID    string `json:"file_id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	SizeBytes int64  `json:"size_bytes"`
}

type DownloadFileInput struct {
	URL      string `json:"url"`
	TempPath string `json:"temp_path"`
}

type ZipInput struct {
	Files      []ResolvedFile `json:"files"`
	TempDir    string         `json:"temp_dir"`
	OutputPath string         `json:"output_path"`
}

type UploadArchiveInput struct {
	ZipPath string `json:"zip_path"`
	Name    string `json:"name"`
}
