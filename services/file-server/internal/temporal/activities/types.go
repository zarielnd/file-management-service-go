package activities

type ResolvedFile struct {
	FileID    string
	Name      string
	URL       string
	SizeBytes int64
}

type DownloadFileInput struct {
	URL      string
	TempPath string
}

type ZipInput struct {
	Files      []ResolvedFile
	TempDir    string
	OutputPath string
}

type UploadArchiveInput struct {
	ZipPath string
	Name    string
	UserID  string // NEW
}
