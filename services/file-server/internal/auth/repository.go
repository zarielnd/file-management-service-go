package auth

import (
	"context"
	"time"
)

type Repository interface {
	CreateUser(ctx context.Context, email, passwordHash string) (string, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
	CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	ValidateAndRotateRefreshToken(ctx context.Context, tokenHash string) (string, error)
	RevokeAllRefreshTokens(ctx context.Context, userID string) error
	IncrementTokenVersion(ctx context.Context, userID string) error
}
