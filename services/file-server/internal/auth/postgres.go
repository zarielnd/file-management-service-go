package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type postgresRepo struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) Repository {
	return &postgresRepo{db: db}
}

func (r *postgresRepo) CreateUser(ctx context.Context, email, passwordHash string) (string, error) {
	query := `INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`
	var id string
	err := r.db.QueryRowContext(ctx, query, email, passwordHash).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

func (r *postgresRepo) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `SELECT id, email, password_hash, token_version, created_at FROM users WHERE email = $1`
	row := r.db.QueryRowContext(ctx, query, email)
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TokenVersion, &u.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return &u, nil
}

func (r *postgresRepo) GetUserByID(ctx context.Context, id string) (*User, error) {
	query := `SELECT id, email, password_hash, token_version, created_at FROM users WHERE id = $1`
	row := r.db.QueryRowContext(ctx, query, id)
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TokenVersion, &u.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return &u, nil
}

func (r *postgresRepo) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	query := `INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`
	_, err := r.db.ExecContext(ctx, query, userID, tokenHash, expiresAt)
	return err
}

func (r *postgresRepo) ValidateAndRotateRefreshToken(ctx context.Context, tokenHash string) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var userID string
	query := `SELECT user_id FROM refresh_tokens WHERE token_hash = $1 AND expires_at > now() AND revoked_at IS NULL`
	if err := tx.QueryRowContext(ctx, query, tokenHash).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("token not found or expired")
		}
		return "", err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE token_hash = $1`, tokenHash); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return userID, nil
}

func (r *postgresRepo) RevokeAllRefreshTokens(ctx context.Context, userID string) error {
	query := `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

func (r *postgresRepo) IncrementTokenVersion(ctx context.Context, userID string) error {
	query := `UPDATE users SET token_version = token_version + 1 WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}
