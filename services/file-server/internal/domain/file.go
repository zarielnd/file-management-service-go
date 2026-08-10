package domain

import "time"

type File struct {
	ID          string
	Name        string
	Size        int64
	ContentType string
	CreatedAt   time.Time
}