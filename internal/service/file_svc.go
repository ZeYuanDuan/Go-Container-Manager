package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/alsonduan/go-container-manager/internal/domain"
	"github.com/alsonduan/go-container-manager/internal/repository"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
)

// Ensure FileService implements consistency: physical file and DB metadata must stay in sync.

const maxFileSize = 50 * 1024 * 1024 // 50MB

var allowedMIMEs = map[string]bool{
	"text/plain":        true,
	"image/jpeg":        true,
	"image/png":         true,
	"application/json":  true,
	"text/csv":          true,
}

type FileService struct {
	fileRepo    domain.FileRepository
	storageRoot string
	fileMu      sync.Map           // map[fileID]*sync.Mutex for per-file locking
	txManager   repository.TxManager // nil for memory repos; set for postgres (row-level lock)
}

func NewFileService(fileRepo domain.FileRepository, storageRoot string, txManager repository.TxManager) *FileService {
	return &FileService{fileRepo: fileRepo, storageRoot: storageRoot, txManager: txManager}
}

func (s *FileService) Upload(ctx context.Context, userID, filename string, content []byte) (*domain.File, error) {
	if int64(len(content)) > maxFileSize {
		return nil, apperr.ErrFileTooLarge
	}

	// Detect MIME via magic bytes
	detected := http.DetectContentType(content)
	// Strip charset params: "text/plain; charset=utf-8" -> "text/plain"
	mime := strings.SplitN(detected, ";", 2)[0]
	mime = strings.TrimSpace(mime)

	if !allowedMIMEs[mime] {
		return nil, apperr.ErrUnsupportedMIME
	}

	fileID := uuid.New().String()
	// Use Base to prevent path traversal
	safeName := filepath.Base(filename)
	if safeName == "" || safeName == "." {
		safeName = "unnamed"
	}
	storagePath := filepath.Join(s.storageRoot, userID, buildStorageFilename(safeName, fileID))

	file := &domain.File{
		ID:               fileID,
		UserID:           userID,
		OriginalFilename: filename,
		StoragePath:      storagePath,
		MimeType:         mime,
		SizeBytes:        int64(len(content)),
		CreatedAt:        time.Now().UTC(),
	}

	dir := filepath.Dir(storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	if err := os.WriteFile(storagePath, content, 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}
	if err := s.fileRepo.Save(ctx, file); err != nil {
		_ = os.Remove(storagePath) // best-effort cleanup
		return nil, err
	}
	return file, nil
}

func (s *FileService) List(ctx context.Context, userID string, page, limit int) ([]*domain.File, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return s.fileRepo.FindByUserID(ctx, userID, page, limit)
}

func (s *FileService) Delete(ctx context.Context, userID, fileID string) error {
	mu := s.fileLock(fileID)
	mu.Lock()
	defer mu.Unlock()

	var storagePath string
	guard := func(txCtx context.Context) error {
		file, err := s.findFileForGuard(txCtx, fileID)
		if err != nil {
			return err
		}
		if file.UserID != userID {
			return apperr.ErrForbidden
		}
		storagePath = file.StoragePath
		return s.fileRepo.Delete(txCtx, fileID)
	}

	if err := s.runGuarded(ctx, guard); err != nil {
		return err
	}
	// Remove physical file after DB commit. If file is already gone, tolerate.
	if storagePath != "" {
		if err := os.Remove(storagePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove file: %w", err)
		}
	}
	return nil
}

// runGuarded executes fn inside a transaction (with row-level lock) when txManager
// is available, or directly (relying on in-memory mutex) when it is not.
func (s *FileService) runGuarded(ctx context.Context, fn func(context.Context) error) error {
	var err error
	if s.txManager != nil {
		err = s.txManager.WithTx(ctx, fn)
	} else {
		err = fn(ctx)
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrNotFound) {
		return apperr.ErrFileNotFound
	}
	if aerr, ok := err.(*apperr.AppError); ok {
		return aerr
	}
	return err
}

// findFileForGuard uses FOR UPDATE when inside a transaction (txManager != nil),
// or plain FindByID when relying on in-memory mutex only.
func (s *FileService) findFileForGuard(ctx context.Context, fileID string) (*domain.File, error) {
	if s.txManager != nil {
		return s.fileRepo.FindByIDForUpdate(ctx, fileID)
	}
	return s.fileRepo.FindByID(ctx, fileID)
}

func (s *FileService) fileLock(fileID string) *sync.Mutex {
	actual, _ := s.fileMu.LoadOrStore(fileID, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// buildStorageFilename produces {base}_{uuid}.{ext} so editors recognize file type by extension.
// Examples: test.png -> test_{uuid}.png, data.csv -> data_{uuid}.csv, noext -> noext_{uuid}
func buildStorageFilename(safeName, fileID string) string {
	ext := filepath.Ext(safeName)
	base := strings.TrimSuffix(safeName, ext)
	if base == "" {
		base = "unnamed"
	}
	if ext == "" {
		return fmt.Sprintf("%s_%s", base, fileID)
	}
	return fmt.Sprintf("%s_%s%s", base, fileID, ext)
}
