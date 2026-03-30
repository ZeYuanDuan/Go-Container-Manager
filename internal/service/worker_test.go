package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/alsonduan/go-container-manager/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mocks (internal package, separate from service_test mocks) ---

type stubJobRepo struct {
	updates []*domain.Job
	saveErr error
}

func (m *stubJobRepo) Save(_ context.Context, j *domain.Job) error        { return m.saveErr }
func (m *stubJobRepo) FindByIDAndUserID(context.Context, string, string) (*domain.Job, error) {
	return nil, nil
}
func (m *stubJobRepo) FindPendingJobs(context.Context) ([]*domain.Job, error) { return nil, nil }
func (m *stubJobRepo) FindPendingOrRunningByTarget(context.Context, string) (*domain.Job, error) {
	return nil, errors.New("none")
}
func (m *stubJobRepo) MarkRunningAsFailed(context.Context, string) error { return nil }
func (m *stubJobRepo) Update(_ context.Context, j *domain.Job) error {
	// Deep copy the status at the time of call
	cp := *j
	m.updates = append(m.updates, &cp)
	return nil
}
func (m *stubJobRepo) DeleteExpired(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

type stubContainerRepo struct {
	container *domain.Container
	findErr   error
	updateErr error
	deleteErr error
	// Track calls
	updateCalls int
	deleteCalls int
}

func (m *stubContainerRepo) Save(context.Context, *domain.Container) error   { return nil }
func (m *stubContainerRepo) FindByUserID(context.Context, string, int, int) ([]*domain.Container, int, error) {
	return nil, 0, nil
}
func (m *stubContainerRepo) FindByID(_ context.Context, _ string) (*domain.Container, error) {
	return m.container, m.findErr
}
func (m *stubContainerRepo) FindByIDForUpdate(_ context.Context, _ string) (*domain.Container, error) {
	return m.container, m.findErr
}
func (m *stubContainerRepo) Update(_ context.Context, _ *domain.Container) error {
	m.updateCalls++
	return m.updateErr
}
func (m *stubContainerRepo) Delete(_ context.Context, _ string) error {
	m.deleteCalls++
	return m.deleteErr
}
func (m *stubContainerRepo) FindByUserIDAndName(context.Context, string, string) (*domain.Container, error) {
	return nil, domain.ErrNotFound
}

type stubRuntime struct {
	createID  string
	createErr error
	startErr  error
	stopErr   error
	removeErr error
	state     string
	stateErr  error
	exited    bool // WaitExit return value
	// Track calls
	removeCalled bool
}

func (m *stubRuntime) CreateAndStart(_ context.Context, _ *domain.Container, _ string, _ string) (string, error) {
	return m.createID, m.createErr
}
func (m *stubRuntime) Start(_ context.Context, _ string) error  { return m.startErr }
func (m *stubRuntime) Stop(_ context.Context, _ string) error   { return m.stopErr }
func (m *stubRuntime) Remove(_ context.Context, _ string) error {
	m.removeCalled = true
	return m.removeErr
}
func (m *stubRuntime) InspectState(_ context.Context, _ string) (string, error) {
	return m.state, m.stateErr
}
func (m *stubRuntime) WaitExit(_ context.Context, _ string, _ time.Duration) bool {
	return m.exited
}

func newTestWorker(jr *stubJobRepo, cr *stubContainerRepo, rt *stubRuntime) *Worker {
	return &Worker{
		jobRepo:         jr,
		containerRepo:   cr,
		runtime:         rt,
		envMap:          map[string]string{"python-3.10": "python:3.10-slim"},
		storageRoot:     "/tmp/test-storage",
		storageHostPath: "/tmp/test-storage",
	}
}

// --- process tests ---

func TestProcess_SetsRunningThenSuccess(t *testing.T) {
	jr := &stubJobRepo{}
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", UserID: "u1", DockerContainerID: "docker-1"},
	}
	rt := &stubRuntime{state: "running"}
	w := newTestWorker(jr, cr, rt)

	job := &domain.Job{ID: "j1", Type: domain.JobTypeStopContainer, TargetResourceID: "cid"}
	w.process(job)

	require.GreaterOrEqual(t, len(jr.updates), 2)
	assert.Equal(t, domain.JobStatusRunning, jr.updates[0].Status)
	assert.Equal(t, domain.JobStatusSuccess, jr.updates[len(jr.updates)-1].Status)
	assert.NotNil(t, jr.updates[len(jr.updates)-1].CompletedAt)
}

func TestProcess_SetsFailedOnError(t *testing.T) {
	jr := &stubJobRepo{}
	cr := &stubContainerRepo{findErr: errors.New("container not found")}
	rt := &stubRuntime{}
	w := newTestWorker(jr, cr, rt)

	job := &domain.Job{ID: "j1", Type: domain.JobTypeCreateContainer, TargetResourceID: "cid"}
	w.process(job)

	require.GreaterOrEqual(t, len(jr.updates), 2)
	last := jr.updates[len(jr.updates)-1]
	assert.Equal(t, domain.JobStatusFailed, last.Status)
	assert.NotNil(t, last.ErrorMessage)
	assert.NotNil(t, last.CompletedAt)
}

// --- processCreate tests ---

func TestProcessCreate_InspectRunning_SetsRunning(t *testing.T) {
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", UserID: "u1", Environment: "python-3.10"},
	}
	rt := &stubRuntime{createID: "docker-1", state: "running"}
	w := newTestWorker(&stubJobRepo{}, cr, rt)

	job := &domain.Job{ID: "j1", TargetResourceID: "cid"}
	err := w.processCreate(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, domain.ContainerStatusRunning, cr.container.Status)
	assert.Equal(t, "docker-1", cr.container.DockerContainerID)
}

func TestProcessCreate_InspectExited_SetsExited(t *testing.T) {
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", UserID: "u1", Environment: "python-3.10"},
	}
	rt := &stubRuntime{createID: "docker-1", state: "exited"}
	w := newTestWorker(&stubJobRepo{}, cr, rt)

	job := &domain.Job{ID: "j1", TargetResourceID: "cid"}
	err := w.processCreate(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, domain.ContainerStatusExited, cr.container.Status)
}

func TestProcessCreate_InspectError_FallbackRunning(t *testing.T) {
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", UserID: "u1", Environment: "python-3.10"},
	}
	rt := &stubRuntime{createID: "docker-1", stateErr: errors.New("inspect failed")}
	w := newTestWorker(&stubJobRepo{}, cr, rt)

	job := &domain.Job{ID: "j1", TargetResourceID: "cid"}
	err := w.processCreate(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, domain.ContainerStatusRunning, cr.container.Status)
}

// --- processStart tests ---

func TestProcessStart_EmptyDockerID_Error(t *testing.T) {
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", DockerContainerID: ""},
	}
	w := newTestWorker(&stubJobRepo{}, cr, &stubRuntime{})

	job := &domain.Job{ID: "j1", TargetResourceID: "cid"}
	err := w.processStart(context.Background(), job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no Docker container ID")
}

func TestProcessStart_RuntimeNotFound_WrappedError(t *testing.T) {
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", DockerContainerID: "docker-1"},
	}
	rt := &stubRuntime{startErr: errdefs.ErrNotFound}
	w := newTestWorker(&stubJobRepo{}, cr, rt)

	job := &domain.Job{ID: "j1", TargetResourceID: "cid"}
	err := w.processStart(context.Background(), job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- processStop tests ---

func TestProcessStop_NotModified_TreatedAsSuccess(t *testing.T) {
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", DockerContainerID: "docker-1"},
	}
	rt := &stubRuntime{stopErr: errdefs.ErrNotModified}
	w := newTestWorker(&stubJobRepo{}, cr, rt)

	job := &domain.Job{ID: "j1", TargetResourceID: "cid"}
	err := w.processStop(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, domain.ContainerStatusExited, cr.container.Status)
}

func TestProcessStop_OtherError_ReturnsFail(t *testing.T) {
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", DockerContainerID: "docker-1"},
	}
	rt := &stubRuntime{stopErr: errors.New("timeout")}
	w := newTestWorker(&stubJobRepo{}, cr, rt)

	job := &domain.Job{ID: "j1", TargetResourceID: "cid"}
	err := w.processStop(context.Background(), job)
	assert.Error(t, err)
}

// --- processDelete tests ---

func TestProcessDelete_DockerNotFound_StillDeletesDB(t *testing.T) {
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", DockerContainerID: "docker-1"},
	}
	rt := &stubRuntime{removeErr: errdefs.ErrNotFound}
	w := newTestWorker(&stubJobRepo{}, cr, rt)

	job := &domain.Job{ID: "j1", TargetResourceID: "cid"}
	err := w.processDelete(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, 1, cr.deleteCalls, "container should still be deleted from DB")
}

func TestProcessDelete_EmptyDockerID_SkipsRemove(t *testing.T) {
	cr := &stubContainerRepo{
		container: &domain.Container{ID: "cid", DockerContainerID: ""},
	}
	rt := &stubRuntime{}
	w := newTestWorker(&stubJobRepo{}, cr, rt)

	job := &domain.Job{ID: "j1", TargetResourceID: "cid"}
	err := w.processDelete(context.Background(), job)
	require.NoError(t, err)
	assert.False(t, rt.removeCalled, "runtime.Remove should not be called for empty DockerContainerID")
	assert.Equal(t, 1, cr.deleteCalls)
}

// --- retryUpdate / retryDelete tests ---

func TestRetryUpdate_SucceedsOnSecondAttempt(t *testing.T) {
	w := newTestWorker(&stubJobRepo{}, &stubContainerRepo{}, &stubRuntime{})

	// Use a custom repo that tracks attempts
	attempts := 0
	customRepo := &retryTestContainerRepo{failUntil: 1, attempts: &attempts}
	w.containerRepo = customRepo

	err := w.retryUpdate(context.Background(), &domain.Container{ID: "cid"})
	require.NoError(t, err)
	assert.Equal(t, 2, *customRepo.attempts) // failed once, succeeded on second
}

func TestRetryUpdate_Exhausted_ReturnsLastError(t *testing.T) {
	attempts := 0
	customRepo := &retryTestContainerRepo{failUntil: 999, attempts: &attempts}
	w := newTestWorker(&stubJobRepo{}, &stubContainerRepo{}, &stubRuntime{})
	w.containerRepo = customRepo

	err := w.retryUpdate(context.Background(), &domain.Container{ID: "cid"})
	assert.Error(t, err)
	assert.Equal(t, 3, *customRepo.attempts) // exactly 3 retries
}

// retryTestContainerRepo is a special mock for retry tests
type retryTestContainerRepo struct {
	stubContainerRepo
	failUntil int
	attempts  *int
}

func (m *retryTestContainerRepo) Update(_ context.Context, _ *domain.Container) error {
	*m.attempts++
	if *m.attempts <= m.failUntil {
		return errors.New("transient error")
	}
	return nil
}

func (m *retryTestContainerRepo) Delete(_ context.Context, _ string) error {
	*m.attempts++
	if *m.attempts <= m.failUntil {
		return errors.New("transient error")
	}
	return nil
}
