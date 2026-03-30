package domain

import (
	"context"
	"time"
)

type File struct {
	ID               string
	UserID           string
	OriginalFilename string
	StoragePath      string
	MimeType         string
	SizeBytes        int64
	CreatedAt        time.Time
}

type FileRepository interface {
	Save(ctx context.Context, file *File) error
	FindByUserID(ctx context.Context, userID string, page, limit int) ([]*File, int, error)
	FindByID(ctx context.Context, fileID string) (*File, error)
	FindByIDForUpdate(ctx context.Context, fileID string) (*File, error)
	Delete(ctx context.Context, fileID string) error
}
