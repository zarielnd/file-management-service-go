package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/zarielnd/file-management-service-go/services/storage/internal/repository"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(connString string) (*Repository, error) {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to the database: %w", err)
	}

	return &Repository{db: db}, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) Create(ctx context.Context, file *repository.File) error {
	query := `INSERT INTO files (id, name, storage_path, content_type, size_bytes, checksum, created_at, owner_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.db.ExecContext(ctx, query,
		file.ID, file.Name, file.StoragePath, file.ContentType, file.SizeBytes, file.Checksum, file.CreatedAt, file.OwnerID)
	if err != nil {
		return fmt.Errorf("failed to insert file: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id string, userID string) (*repository.File, error) {
	query := `SELECT id, name, storage_path, content_type, size_bytes, checksum, created_at, owner_id
	          FROM files WHERE id = $1 AND owner_id = $2`
	row := r.db.QueryRowContext(ctx, query, id, userID)
	var f repository.File
	err := row.Scan(&f.ID, &f.Name, &f.StoragePath, &f.ContentType, &f.SizeBytes, &f.Checksum, &f.CreatedAt, &f.OwnerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("file not found")
		}
		return nil, fmt.Errorf("failed to get file by ID: %w", err)
	}
	return &f, nil
}

func (r *Repository) GetByIDs(ctx context.Context, ids []string, userID string) ([]*repository.File, error) {
	if len(ids) == 0 {
		return []*repository.File{}, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids)+1)

	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	args[len(ids)] = userID

	query := fmt.Sprintf(`SELECT id, name, storage_path, content_type, size_bytes, checksum, created_at, owner_id FROM files WHERE id IN (%s) AND owner_id = $%d`,
		strings.Join(placeholders, ","), len(ids)+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get files by IDs: %w", err)
	}
	defer rows.Close()

	var files []*repository.File
	for rows.Next() {
		var f repository.File
		if err := rows.Scan(&f.ID, &f.Name, &f.StoragePath, &f.ContentType, &f.SizeBytes, &f.Checksum, &f.CreatedAt, &f.OwnerID); err != nil {
			return nil, fmt.Errorf("failed to scan file: %w", err)
		}
		files = append(files, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return files, nil
}

func (r *Repository) List(ctx context.Context, userID string, limit, offset int) ([]*repository.File, int, error) {
	countQuery := `SELECT COUNT(*) FROM files WHERE owner_id = $1`
	var totalCount int
	err := r.db.QueryRowContext(ctx, countQuery, userID).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count files: %w", err)
	}

	query := `SELECT id, name, storage_path, content_type, size_bytes, checksum, created_at, owner_id FROM files WHERE owner_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := r.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list files: %w", err)
	}
	defer rows.Close()

	var files []*repository.File
	for rows.Next() {
		var f repository.File
		if err := rows.Scan(&f.ID, &f.Name, &f.StoragePath, &f.ContentType, &f.SizeBytes, &f.Checksum, &f.CreatedAt, &f.OwnerID); err != nil {
			return nil, 0, fmt.Errorf("failed to scan file: %w", err)
		}
		files = append(files, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating over rows: %w", err)
	}

	return files, totalCount, nil
}

func (r *Repository) Update(ctx context.Context, file *repository.File) error {
	query := `UPDATE files
		SET name = $2, storage_path = $3, content_type = $4,
		    size_bytes = $5, checksum = $6, created_at = $7
		WHERE id = $1`

	res, err := r.db.ExecContext(ctx, query,
		file.ID, file.Name, file.StoragePath, file.ContentType,
		file.SizeBytes, file.Checksum, file.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to update file: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("file not found")
	}
	return nil
}

func (r *Repository) ConfirmUpload(ctx context.Context, id string, size int64, checksum string) error {
	query := `UPDATE files SET size_bytes = $2, checksum = $3 WHERE id = $1`
	res, err := r.db.ExecContext(ctx, query, id, size, checksum)
	if err != nil {
		return fmt.Errorf("failed to confirm upload: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("file not found")
	}
	return nil
}
