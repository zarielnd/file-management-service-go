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
	query := `INSERT INTO files (id, name, storage_path, content_type, size_bytes, checksum, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := r.db.ExecContext(ctx, query,
		file.ID, file.Name, file.StoragePath, file.ContentType, file.SizeBytes, file.Checksum, file.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert file: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*repository.File, error) {
	query := `SELECT id, name, storage_path, content_type, size_bytes, checksum, created_at FROM files WHERE id = $1`
	row := r.db.QueryRowContext(ctx, query, id)
	var f repository.File
	err := row.Scan(&f.ID, &f.Name, &f.StoragePath, &f.ContentType, &f.SizeBytes, &f.Checksum, &f.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("file not found")
		}
		return nil, fmt.Errorf("failed to get file by ID: %w", err)
	}
	return &f, nil
}

func (r *Repository) GetByIDs(ctx context.Context, ids []string) ([]repository.File, error) {
	if len(ids) == 0 {
		return []repository.File{}, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))

	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`SELECT id, name, storage_path, content_type, size_bytes, checksum, created_at FROM files WHERE id IN (%s)`, 
		strings.Join(placeholders, ","))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get files by IDs: %w", err)
	}
	defer rows.Close()

	var files []repository.File
	for rows.Next() {
		var f repository.File
		if err := rows.Scan(&f.ID, &f.Name, &f.StoragePath, &f.ContentType, &f.SizeBytes, &f.Checksum, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan file: %w", err)
		}
		files = append(files, f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return files, nil
}

func (r *Repository) List(ctx context.Context, limit, offset int) ([]*repository.File, int, error) {
	countQuery := `SELECT COUNT(*) FROM files`
	var totalCount int
	err := r.db.QueryRowContext(ctx, countQuery).Scan(&totalCount)
	if err != nil {
		return nil,0, fmt.Errorf("failed to count files: %w", err)
	}

	query := `SELECT id, name, storage_path, content_type, size_bytes, checksum, created_at FROM files ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil,0, fmt.Errorf("failed to list files: %w", err)
	}
	defer rows.Close()

	var files []*repository.File
	for rows.Next() {
		var f repository.File
		if err := rows.Scan(&f.ID, &f.Name, &f.StoragePath, &f.ContentType, &f.SizeBytes, &f.Checksum, &f.CreatedAt); err != nil {
			return nil,0, fmt.Errorf("failed to scan file: %w", err)
		}
		files = append(files, &f)
	}

	if err := rows.Err(); err != nil {
		return nil,0, fmt.Errorf("error iterating over rows: %w", err)
	}

	return files,totalCount, nil
}