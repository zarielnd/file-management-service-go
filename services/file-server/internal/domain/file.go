package domain

import "time"

type File struct {
	ID          string
	Name        string
	Size        int64
	ContentType string
	Checksum    string
	CreatedAt   time.Time
}
