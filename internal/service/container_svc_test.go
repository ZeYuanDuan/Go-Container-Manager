package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alsonduan/go-container-manager/internal/domain"
	"github.com/alsonduan/go-container-manager/internal/service"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mocks ---

type mockContainerRepo struct {
	saveErr       error
	findResult    *domain.Container
	findErr       error
	updateErr     error
	deleteErr     error
	containers    []*domain.Container
	total         int
	listErr       error
	capturedPage  int
	capturedLimit int
}

func (m *mockContainerRepo) Save(_ context.Context, _ *domain.Container) error {
	return m.saveErr
}
func (m *mockContainerRepo) FindByUserID(_ context.Context, _ string, page, limit int) ([]*domain.Container, int, error) {
	m.capturedPage = page
	m.capturedLimit = limit
	return m.containers, m.total, m.listErr
}
func (m *mockContainerRepo) FindByID(_ context.Context, _ string) (*domain.Container, error) {
	return m.findResult, m.findErr
}
func (m *mockContainerRepo) FindByIDForUpdate(_ context.Context, _ string) (*domain.Container, error) {
	return m.findResult, m.findErr
}
func (m *mockContainerRepo) Update(_ context.Context, _ *domain.Container) error {
	return m.updateErr
}
func (m *mockContainerRepo) Delete(_ context.Context, _ string) error {
	return m.deleteErr
}
func (m *mockContainerRepo) FindByUserIDAndName(_ context.Context, _, _ string) (*domain.Container, error) {
	return nil, domain.ErrNotFound
}

type mockJobRepo struct {
	saveErr                error
	pendingOrRunningResult *domain.Job
	pendingOrRunningErr    error
	findResult             *domain.Job
	findErr                error
	updateErr              error
	pendingJobs            []*domain.Job
	pendingJobsErr         error
	markErr                error
	lastSavedJob           *domain.Job
}

func (m *mockJobRepo) Save(_ context.Context, j *domain.Job) error {
	m.lastSavedJob = j
	return m.saveErr
}
func (m *mockJobRepo) FindByIDAndUserID(_ context.Context, _, _ string) (*domain.Job, error) {
	return m.findResult, m.findErr
}
func (m *mockJobRepo) FindPendingJobs(_ context.Context) ([]*domain.Job, error) {
	return m.pendingJobs, m.pendingJobsErr
}
func (m *mockJobRepo) FindPendingOrRunningByTarget(_ context.Context, _ string) (*domain.Job, error) {
	return m.pendingOrRunningResult, m.pendingOrRunningErr
}
func (m *mockJobRepo) MarkRunningAsFailed(_ context.Context, _ string) error {
	return m.markErr
}
func (m *mockJobRepo) Update(_ context.Context, _ *domain.Job) error {
	return m.updateErr
}
func (m *mockJobRepo) DeleteExpired(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// --- helpers ---

var testEnvMap = map[string]string{
	"python-3.10": "python:3.10-slim",
	"ubuntu-base": "ubuntu:22.04",
}

// newContainerSvc creates a ContainerService with a buffered job channel and a draining goroutine.
func newContainerSvc(cRepo *mockContainerRepo, jRepo *mockJobRepo) (*service.ContainerService, func()) {
	ch := make(chan *domain.Job, 10)
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	svc := service.NewContainerService(cRepo, jRepo, nil, testEnvMap, ch, nil)
	cleanup := func() {
		close(ch)
		<-done
	}
	return svc, cleanup
}

// --- Create tests ---

func TestContainerCreate_InvalidEnvironment_400(t *testing.T) {
	svc, cleanup := newContainerSvc(&mockContainerRepo{}, &mockJobRepo{})
	defer cleanup()

	_, err := svc.Create(context.Background(), "user1", "test", "invalid-env", []string{"echo"})
	assert.ErrorIs(t, err, apperr.ErrInvalidEnvironment)
}

func TestContainerCreate_DuplicateName_409(t *testing.T) {
	svc, cleanup := newContainerSvc(
		&mockContainerRepo{saveErr: domain.ErrDuplicate},
		&mockJobRepo{},
	)
	defer cleanup()

	_, err := svc.Create(context.Background(), "user1", "dup", "python-3.10", []string{"echo"})
	assert.ErrorIs(t, err, apperr.ErrContainerNameDup)
}

func TestContainerCreate_HappyPath_ReturnsJob(t *testing.T) {
	svc, cleanup := newContainerSvc(&mockContainerRepo{}, &mockJobRepo{})
	defer cleanup()

	job, err := svc.Create(context.Background(), "user1", "mycontainer", "python-3.10", []string{"echo", "1"})
	require.NoError(t, err)
	assert.NotEmpty(t, job.ID)
	assert.Equal(t, domain.JobStatusPending, job.Status)
	assert.Equal(t, domain.JobTypeCreateContainer, job.Type)
}

// --- List pagination tests ---

func TestContainerList_PageZero_NormalizedTo1(t *testing.T) {
	repo := &mockContainerRepo{containers: []*domain.Container{}, total: 0}
	svc, cleanup := newContainerSvc(repo, &mockJobRepo{})
	defer cleanup()

	_, _, err := svc.List(context.Background(), "user1", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, 1, repo.capturedPage)
}

func TestContainerList_LimitZero_NormalizedTo20(t *testing.T) {
	repo := &mockContainerRepo{containers: []*domain.Container{}, total: 0}
	svc, cleanup := newContainerSvc(repo, &mockJobRepo{})
	defer cleanup()

	_, _, err := svc.List(context.Background(), "user1", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 20, repo.capturedLimit)
}

func TestContainerList_LimitOver100_NormalizedTo100(t *testing.T) {
	repo := &mockContainerRepo{containers: []*domain.Container{}, total: 0}
	svc, cleanup := newContainerSvc(repo, &mockJobRepo{})
	defer cleanup()

	_, _, err := svc.List(context.Background(), "user1", 1, 999)
	require.NoError(t, err)
	assert.Equal(t, 100, repo.capturedLimit)
}

// --- Get tests ---

func TestContainerGet_NotFound_404(t *testing.T) {
	repo := &mockContainerRepo{findErr: domain.ErrNotFound}
	svc, cleanup := newContainerSvc(repo, &mockJobRepo{})
	defer cleanup()

	_, err := svc.Get(context.Background(), "user1", "cid")
	assert.ErrorIs(t, err, apperr.ErrContainerNotFound)
}

func TestContainerGet_WrongOwner_403(t *testing.T) {
	repo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "other"}}
	svc, cleanup := newContainerSvc(repo, &mockJobRepo{})
	defer cleanup()

	_, err := svc.Get(context.Background(), "user1", "cid")
	assert.ErrorIs(t, err, apperr.ErrForbidden)
}

// --- Start tests (txManager=nil path) ---

func TestContainerStart_NotFound_404(t *testing.T) {
	cRepo := &mockContainerRepo{findErr: domain.ErrNotFound}
	svc, cleanup := newContainerSvc(cRepo, &mockJobRepo{pendingOrRunningErr: errors.New("none")})
	defer cleanup()

	_, err := svc.Start(context.Background(), "user1", "cid")
	assert.ErrorIs(t, err, apperr.ErrContainerNotFound)
}

func TestContainerStart_WrongOwner_403(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "other", Status: domain.ContainerStatusExited}}
	svc, cleanup := newContainerSvc(cRepo, &mockJobRepo{pendingOrRunningErr: errors.New("none")})
	defer cleanup()

	_, err := svc.Start(context.Background(), "user1", "cid")
	assert.ErrorIs(t, err, apperr.ErrForbidden)
}

func TestContainerStart_AlreadyRunning_409(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "user1", Status: domain.ContainerStatusRunning}}
	jRepo := &mockJobRepo{pendingOrRunningErr: errors.New("none")} // no pending job
	svc, cleanup := newContainerSvc(cRepo, jRepo)
	defer cleanup()

	_, err := svc.Start(context.Background(), "user1", "cid")
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)
}

func TestContainerStart_PendingJobExists_409(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "user1", Status: domain.ContainerStatusExited}}
	jRepo := &mockJobRepo{pendingOrRunningResult: &domain.Job{ID: "existing-job"}} // pending job found (nil error)
	svc, cleanup := newContainerSvc(cRepo, jRepo)
	defer cleanup()

	_, err := svc.Start(context.Background(), "user1", "cid")
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)
}

func TestContainerStart_HappyPath(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "user1", Status: domain.ContainerStatusExited}}
	jRepo := &mockJobRepo{pendingOrRunningErr: errors.New("none")}
	svc, cleanup := newContainerSvc(cRepo, jRepo)
	defer cleanup()

	job, err := svc.Start(context.Background(), "user1", "cid")
	require.NoError(t, err)
	assert.Equal(t, domain.JobTypeStartContainer, job.Type)
}

// --- Stop tests ---

func TestContainerStop_NotFound_404(t *testing.T) {
	cRepo := &mockContainerRepo{findErr: domain.ErrNotFound}
	svc, cleanup := newContainerSvc(cRepo, &mockJobRepo{pendingOrRunningErr: errors.New("none")})
	defer cleanup()

	_, err := svc.Stop(context.Background(), "user1", "cid")
	assert.ErrorIs(t, err, apperr.ErrContainerNotFound)
}

func TestContainerStop_WrongOwner_403(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "other", Status: domain.ContainerStatusRunning}}
	svc, cleanup := newContainerSvc(cRepo, &mockJobRepo{pendingOrRunningErr: errors.New("none")})
	defer cleanup()

	_, err := svc.Stop(context.Background(), "user1", "cid")
	assert.ErrorIs(t, err, apperr.ErrForbidden)
}

func TestContainerStop_NotRunning_409(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "user1", Status: domain.ContainerStatusExited}}
	jRepo := &mockJobRepo{pendingOrRunningErr: errors.New("none")}
	svc, cleanup := newContainerSvc(cRepo, jRepo)
	defer cleanup()

	_, err := svc.Stop(context.Background(), "user1", "cid")
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)
}

func TestContainerStop_HappyPath(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "user1", Status: domain.ContainerStatusRunning}}
	jRepo := &mockJobRepo{pendingOrRunningErr: errors.New("none")}
	svc, cleanup := newContainerSvc(cRepo, jRepo)
	defer cleanup()

	job, err := svc.Stop(context.Background(), "user1", "cid")
	require.NoError(t, err)
	assert.Equal(t, domain.JobTypeStopContainer, job.Type)
}

// --- Delete tests ---

func TestContainerDelete_NotFound_404(t *testing.T) {
	cRepo := &mockContainerRepo{findErr: domain.ErrNotFound}
	svc, cleanup := newContainerSvc(cRepo, &mockJobRepo{pendingOrRunningErr: errors.New("none")})
	defer cleanup()

	_, err := svc.Delete(context.Background(), "user1", "cid")
	assert.ErrorIs(t, err, apperr.ErrContainerNotFound)
}

func TestContainerDelete_WrongOwner_403(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "other", Status: domain.ContainerStatusExited}}
	svc, cleanup := newContainerSvc(cRepo, &mockJobRepo{pendingOrRunningErr: errors.New("none")})
	defer cleanup()

	_, err := svc.Delete(context.Background(), "user1", "cid")
	assert.ErrorIs(t, err, apperr.ErrForbidden)
}

func TestContainerDelete_Running_409(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "user1", Status: domain.ContainerStatusRunning}}
	jRepo := &mockJobRepo{pendingOrRunningErr: errors.New("none")}
	svc, cleanup := newContainerSvc(cRepo, jRepo)
	defer cleanup()

	_, err := svc.Delete(context.Background(), "user1", "cid")
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)
}

func TestContainerDelete_PendingJobExists_409(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "user1", Status: domain.ContainerStatusExited}}
	jRepo := &mockJobRepo{pendingOrRunningResult: &domain.Job{ID: "existing"}}
	svc, cleanup := newContainerSvc(cRepo, jRepo)
	defer cleanup()

	_, err := svc.Delete(context.Background(), "user1", "cid")
	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)
}

func TestContainerDelete_HappyPath(t *testing.T) {
	cRepo := &mockContainerRepo{findResult: &domain.Container{ID: "cid", UserID: "user1", Status: domain.ContainerStatusExited}}
	jRepo := &mockJobRepo{pendingOrRunningErr: errors.New("none")}
	svc, cleanup := newContainerSvc(cRepo, jRepo)
	defer cleanup()

	job, err := svc.Delete(context.Background(), "user1", "cid")
	require.NoError(t, err)
	assert.Equal(t, domain.JobTypeDeleteContainer, job.Type)
}
