package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alsonduan/go-container-manager/internal/domain"
	"github.com/alsonduan/go-container-manager/internal/service"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock ---

type mockFileRepo struct {
	saveErr   error
	savedFile *domain.File

	files   []*domain.File
	total   int
	listErr error

	foundFile *domain.File
	findErr   error

	deleteErr error

	// Captured args for pagination assertions
	capturedPage  int
	capturedLimit int
}

func (m *mockFileRepo) Save(_ context.Context, f *domain.File) error {
	m.savedFile = f
	return m.saveErr
}

func (m *mockFileRepo) FindByUserID(_ context.Context, _ string, page, limit int) ([]*domain.File, int, error) {
	m.capturedPage = page
	m.capturedLimit = limit
	return m.files, m.total, m.listErr
}

func (m *mockFileRepo) FindByID(_ context.Context, _ string) (*domain.File, error) {
	return m.foundFile, m.findErr
}

func (m *mockFileRepo) FindByIDForUpdate(_ context.Context, _ string) (*domain.File, error) {
	return m.foundFile, m.findErr
}

func (m *mockFileRepo) Delete(_ context.Context, _ string) error {
	return m.deleteErr
}

// --- Upload tests ---

func TestUpload_ExactlyMaxSize_Passes(t *testing.T) {
	tmpDir := t.TempDir()
	repo := &mockFileRepo{}
	svc := service.NewFileService(repo, tmpDir, nil)

	// 50MB exactly — first 512 bytes are "hello " repeated to ensure text/plain detection
	content := make([]byte, 50*1024*1024)
	filler := []byte("hello world ")
	for i := 0; i < 512; i++ {
		content[i] = filler[i%len(filler)]
	}

	f, err := svc.Upload(context.Background(), "user1", "big.txt", content)
	require.NoError(t, err)
	assert.Equal(t, int64(50*1024*1024), f.SizeBytes)
}

func TestUpload_ExceedsMaxSize_413(t *testing.T) {
	svc := service.NewFileService(&mockFileRepo{}, t.TempDir(), nil)

	content := make([]byte, 50*1024*1024+1)
	_, err := svc.Upload(context.Background(), "user1", "too-big.txt", content)
	assert.ErrorIs(t, err, apperr.ErrFileTooLarge)
}

func TestUpload_TextPlain_Passes(t *testing.T) {
	svc := service.NewFileService(&mockFileRepo{}, t.TempDir(), nil)

	f, err := svc.Upload(context.Background(), "user1", "hello.txt", []byte("hello world"))
	require.NoError(t, err)
	assert.Equal(t, "text/plain", f.MimeType)
}

func TestUpload_GIFMagicBytes_415(t *testing.T) {
	svc := service.NewFileService(&mockFileRepo{}, t.TempDir(), nil)

	_, err := svc.Upload(context.Background(), "user1", "img.gif", []byte("GIF89a\x01\x02\x03"))
	assert.ErrorIs(t, err, apperr.ErrUnsupportedMIME)
}

func TestUpload_CharsetStripped(t *testing.T) {
	repo := &mockFileRepo{}
	svc := service.NewFileService(repo, t.TempDir(), nil)

	f, err := svc.Upload(context.Background(), "user1", "test.txt", []byte("hello world"))
	require.NoError(t, err)
	// http.DetectContentType returns "text/plain; charset=utf-8" for text — verify charset is stripped
	assert.Equal(t, "text/plain", f.MimeType)
	assert.False(t, strings.Contains(f.MimeType, "charset"))
}

func TestUpload_PathTraversal_Prevented(t *testing.T) {
	repo := &mockFileRepo{}
	svc := service.NewFileService(repo, t.TempDir(), nil)

	f, err := svc.Upload(context.Background(), "user1", "../../etc/passwd", []byte("hello"))
	require.NoError(t, err)
	// StoragePath must not contain ".."
	assert.False(t, strings.Contains(f.StoragePath, ".."), "StoragePath should not contain path traversal: %s", f.StoragePath)
	// filepath.Base("../../etc/passwd") == "passwd"
	assert.True(t, strings.Contains(f.StoragePath, "passwd"))
}

func TestUpload_EmptyFilename_UsesUnnamed(t *testing.T) {
	svc := service.NewFileService(&mockFileRepo{}, t.TempDir(), nil)

	f, err := svc.Upload(context.Background(), "user1", "", []byte("hello"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(f.StoragePath, "unnamed"))
}

func TestUpload_StoragePath_PreservesExtension(t *testing.T) {
	repo := &mockFileRepo{}
	tmpDir := t.TempDir()
	svc := service.NewFileService(repo, tmpDir, nil)

	f, err := svc.Upload(context.Background(), "user1", "test.png", []byte("\x89PNG\r\n\x1a\n"))
	require.NoError(t, err)
	// Format: {base}_{uuid}.{ext} so editors recognize .png
	assert.Regexp(t, `test_[0-9a-f-]+\.png$`, filepath.Base(f.StoragePath))
	assert.True(t, strings.HasSuffix(f.StoragePath, ".png"))
}

func TestUpload_DBSaveFailure_CleansUpFile(t *testing.T) {
	tmpDir := t.TempDir()
	repo := &mockFileRepo{saveErr: errors.New("db error")}
	svc := service.NewFileService(repo, tmpDir, nil)

	_, err := svc.Upload(context.Background(), "user1", "test.txt", []byte("hello"))
	assert.Error(t, err)

	// Verify physical file was cleaned up
	entries, _ := filepath.Glob(filepath.Join(tmpDir, "user1", "*"))
	assert.Empty(t, entries, "physical file should be cleaned up after DB save failure")
}

// --- List pagination tests ---

func TestFileList_PageZero_NormalizedTo1(t *testing.T) {
	repo := &mockFileRepo{files: []*domain.File{}, total: 0}
	svc := service.NewFileService(repo, t.TempDir(), nil)

	_, _, err := svc.List(context.Background(), "user1", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, 1, repo.capturedPage)
}

func TestFileList_LimitZero_NormalizedTo20(t *testing.T) {
	repo := &mockFileRepo{files: []*domain.File{}, total: 0}
	svc := service.NewFileService(repo, t.TempDir(), nil)

	_, _, err := svc.List(context.Background(), "user1", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 20, repo.capturedLimit)
}

func TestFileList_LimitOver100_NormalizedTo100(t *testing.T) {
	repo := &mockFileRepo{files: []*domain.File{}, total: 0}
	svc := service.NewFileService(repo, t.TempDir(), nil)

	_, _, err := svc.List(context.Background(), "user1", 1, 999)
	require.NoError(t, err)
	assert.Equal(t, 100, repo.capturedLimit)
}

// --- Delete tests ---

func TestFileDelete_NotFound_404(t *testing.T) {
	repo := &mockFileRepo{findErr: domain.ErrNotFound}
	svc := service.NewFileService(repo, t.TempDir(), nil)

	err := svc.Delete(context.Background(), "user1", "nonexistent-id")
	assert.ErrorIs(t, err, apperr.ErrFileNotFound)
}

func TestFileDelete_WrongOwner_403(t *testing.T) {
	repo := &mockFileRepo{
		foundFile: &domain.File{ID: "f1", UserID: "user2", StoragePath: "/tmp/x"},
	}
	svc := service.NewFileService(repo, t.TempDir(), nil)

	err := svc.Delete(context.Background(), "user1", "f1")
	assert.ErrorIs(t, err, apperr.ErrForbidden)
}

func TestFileDelete_PhysicalFileGone_Tolerated(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "user1", "gone_file")
	repo := &mockFileRepo{
		foundFile: &domain.File{ID: "f1", UserID: "user1", StoragePath: nonExistentPath},
	}
	svc := service.NewFileService(repo, tmpDir, nil)

	err := svc.Delete(context.Background(), "user1", "f1")
	assert.NoError(t, err, "deleting a file whose physical file is already gone should succeed")
}

func TestFileDelete_DBDeleteFails_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a physical file so os.Remove succeeds
	filePath := filepath.Join(tmpDir, "user1", "test_file")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0755))
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	repo := &mockFileRepo{
		foundFile: &domain.File{ID: "f1", UserID: "user1", StoragePath: filePath},
		deleteErr: errors.New("db delete error"),
	}
	svc := service.NewFileService(repo, tmpDir, nil)

	err := svc.Delete(context.Background(), "user1", "f1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db delete error")
}
