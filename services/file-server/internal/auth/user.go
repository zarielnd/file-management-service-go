package auth

import "time"

type User struct {
	ID           string
	Email        string
	PasswordHash string
	TokenVersion int
	CreatedAt    time.Time
}
