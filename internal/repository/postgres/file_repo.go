package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/alsonduan/go-container-manager/internal/domain"
)

type FileRepository struct {
	pool *pgxpool.Pool
}

func NewFileRepository(pool *pgxpool.Pool) *FileRepository {
	return &FileRepository{pool: pool}
}

func (r *FileRepository) Save(ctx context.Context, file *domain.File) error {
	q := getQueryable(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO files (id, user_id, original_filename, storage_path, mime_type, size_bytes, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		file.ID, file.UserID, file.OriginalFilename, file.StoragePath, file.MimeType, file.SizeBytes, file.CreatedAt,
	)
	return err
}

func (r *FileRepository) FindByUserID(ctx context.Context, userID string, page, limit int) ([]*domain.File, int, error) {
	q := getQueryable(ctx, r.pool)

	var total int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM files WHERE user_id = $1`, userID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	rows, err := q.Query(ctx,
		`SELECT id, user_id, original_filename, storage_path, mime_type, size_bytes, created_at
		 FROM files WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var files []*domain.File
	for rows.Next() {
		var f domain.File
		if err := rows.Scan(&f.ID, &f.UserID, &f.OriginalFilename, &f.StoragePath, &f.MimeType, &f.SizeBytes, &f.CreatedAt); err != nil {
			return nil, 0, err
		}
		files = append(files, &f)
	}
	return files, total, rows.Err()
}

func (r *FileRepository) FindByID(ctx context.Context, fileID string) (*domain.File, error) {
	q := getQueryable(ctx, r.pool)
	var f domain.File
	err := q.QueryRow(ctx,
		`SELECT id, user_id, original_filename, storage_path, mime_type, size_bytes, created_at
		 FROM files WHERE id = $1`,
		fileID,
	).Scan(&f.ID, &f.UserID, &f.OriginalFilename, &f.StoragePath, &f.MimeType, &f.SizeBytes, &f.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

func (r *FileRepository) FindByIDForUpdate(ctx context.Context, fileID string) (*domain.File, error) {
	q := getQueryable(ctx, r.pool)
	var f domain.File
	err := q.QueryRow(ctx,
		`SELECT id, user_id, original_filename, storage_path, mime_type, size_bytes, created_at
		 FROM files WHERE id = $1 FOR UPDATE`,
		fileID,
	).Scan(&f.ID, &f.UserID, &f.OriginalFilename, &f.StoragePath, &f.MimeType, &f.SizeBytes, &f.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

func (r *FileRepository) Delete(ctx context.Context, fileID string) error {
	q := getQueryable(ctx, r.pool)
	ct, err := q.Exec(ctx, `DELETE FROM files WHERE id = $1`, fileID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
