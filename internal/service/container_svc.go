package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/alsonduan/go-container-manager/internal/domain"
	"github.com/alsonduan/go-container-manager/internal/repository"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
)

type ContainerService struct {
	containerRepo domain.ContainerRepository
	jobRepo       domain.JobRepository
	runtime       domain.ContainerRuntime
	envMap        map[string]string
	jobChan       chan *domain.Job
	containerMu   sync.Map // map[containerID]*sync.Mutex for per-container locking
	txManager     repository.TxManager // nil for memory repos; set for postgres (row-level lock)
}

func NewContainerService(
	containerRepo domain.ContainerRepository,
	jobRepo domain.JobRepository,
	runtime domain.ContainerRuntime,
	envMap map[string]string,
	jobChan chan *domain.Job,
	txManager repository.TxManager,
) *ContainerService {
	return &ContainerService{
		containerRepo: containerRepo,
		jobRepo:       jobRepo,
		runtime:       runtime,
		envMap:        envMap,
		jobChan:       jobChan,
		txManager:     txManager,
	}
}

func (s *ContainerService) Create(ctx context.Context, userID, name, env string, command []string) (*domain.Job, error) {
	if _, ok := s.envMap[env]; !ok {
		return nil, apperr.ErrInvalidEnvironment
	}

	containerID := uuid.New().String()
	now := time.Now().UTC()
	container := &domain.Container{
		ID:          containerID,
		UserID:      userID,
		Name:        name,
		Environment: env,
		Command:     command,
		Status:      domain.ContainerStatusCreated,
		MountPath:   "/workspace",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.containerRepo.Save(ctx, container); err != nil {
		if errors.Is(err, domain.ErrDuplicate) {
			return nil, apperr.ErrContainerNameDup
		}
		return nil, err
	}

	job := s.createJob(userID, containerID, domain.JobTypeCreateContainer)
	if err := s.jobRepo.Save(ctx, job); err != nil {
		return nil, err
	}
	if err := s.sendJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *ContainerService) List(ctx context.Context, userID string, page, limit int) ([]*domain.Container, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return s.containerRepo.FindByUserID(ctx, userID, page, limit)
}

func (s *ContainerService) Get(ctx context.Context, userID, containerID string) (*domain.Container, error) {
	c, err := s.containerRepo.FindByID(ctx, containerID)
	if err != nil {
		return nil, apperr.ErrContainerNotFound
	}
	if c.UserID != userID {
		return nil, apperr.ErrForbidden
	}
	return c, nil
}

func (s *ContainerService) Start(ctx context.Context, userID, containerID string) (*domain.Job, error) {
	mu := s.containerLock(containerID)
	mu.Lock()
	defer mu.Unlock()

	job := s.createJob(userID, containerID, domain.JobTypeStartContainer)
	guard := func(txCtx context.Context) error {
		c, err := s.findContainerForGuard(txCtx, containerID)
		if err != nil {
			return err
		}
		if c.UserID != userID {
			return apperr.ErrForbidden
		}
		if _, err := s.jobRepo.FindPendingOrRunningByTarget(txCtx, containerID); err == nil {
			return apperr.New(409, "CONFLICT", "Container is already running or another operation is in progress")
		}
		if c.Status == domain.ContainerStatusRunning {
			return apperr.New(409, "CONFLICT", "Container is already running or another operation is in progress")
		}
		return s.jobRepo.Save(txCtx, job)
	}

	if err := s.runGuarded(ctx, guard); err != nil {
		return nil, err
	}
	if err := s.sendJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *ContainerService) Stop(ctx context.Context, userID, containerID string) (*domain.Job, error) {
	mu := s.containerLock(containerID)
	mu.Lock()
	defer mu.Unlock()

	job := s.createJob(userID, containerID, domain.JobTypeStopContainer)
	guard := func(txCtx context.Context) error {
		c, err := s.findContainerForGuard(txCtx, containerID)
		if err != nil {
			return err
		}
		if c.UserID != userID {
			return apperr.ErrForbidden
		}
		if _, err := s.jobRepo.FindPendingOrRunningByTarget(txCtx, containerID); err == nil {
			return apperr.New(409, "CONFLICT", "Container is not running or already stopped")
		}
		if c.Status != domain.ContainerStatusRunning {
			return apperr.New(409, "CONFLICT", "Container is not running or already stopped")
		}
		return s.jobRepo.Save(txCtx, job)
	}

	if err := s.runGuarded(ctx, guard); err != nil {
		return nil, err
	}
	if err := s.sendJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *ContainerService) Delete(ctx context.Context, userID, containerID string) (*domain.Job, error) {
	mu := s.containerLock(containerID)
	mu.Lock()
	defer mu.Unlock()

	job := s.createJob(userID, containerID, domain.JobTypeDeleteContainer)
	guard := func(txCtx context.Context) error {
		c, err := s.findContainerForGuard(txCtx, containerID)
		if err != nil {
			return err
		}
		if c.UserID != userID {
			return apperr.ErrForbidden
		}
		if _, err := s.jobRepo.FindPendingOrRunningByTarget(txCtx, containerID); err == nil {
			return apperr.New(409, "CONFLICT", "Container must be stopped before deletion. Call POST /containers/{container_id}/stop first")
		}
		if c.Status == domain.ContainerStatusRunning {
			return apperr.New(409, "CONFLICT", "Container must be stopped before deletion. Call POST /containers/{container_id}/stop first")
		}
		return s.jobRepo.Save(txCtx, job)
	}

	if err := s.runGuarded(ctx, guard); err != nil {
		return nil, err
	}
	if err := s.sendJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

// runGuarded executes fn inside a transaction (with row-level lock) when txManager
// is available, or directly (relying on in-memory mutex) when it is not.
// Domain errors are mapped to AppErrors.
func (s *ContainerService) runGuarded(ctx context.Context, fn func(context.Context) error) error {
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
		return apperr.ErrContainerNotFound
	}
	if aerr, ok := err.(*apperr.AppError); ok {
		return aerr
	}
	return err
}

// findContainerForGuard uses FOR UPDATE when inside a transaction (txManager != nil),
// or plain FindByID when relying on in-memory mutex only.
func (s *ContainerService) findContainerForGuard(ctx context.Context, containerID string) (*domain.Container, error) {
	if s.txManager != nil {
		return s.containerRepo.FindByIDForUpdate(ctx, containerID)
	}
	return s.containerRepo.FindByID(ctx, containerID)
}

func (s *ContainerService) sendJob(ctx context.Context, job *domain.Job) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case s.jobChan <- job:
		return nil
	case <-ctx.Done():
		return apperr.ErrServiceUnavailable
	}
}

func (s *ContainerService) createJob(userID, targetID, jobType string) *domain.Job {
	return &domain.Job{
		ID:               uuid.New().String(),
		UserID:           userID,
		TargetResourceID: targetID,
		Type:             jobType,
		Status:           domain.JobStatusPending,
		CreatedAt:        time.Now().UTC(),
	}
}

func (s *ContainerService) containerLock(containerID string) *sync.Mutex {
	actual, _ := s.containerMu.LoadOrStore(containerID, &sync.Mutex{})
	return actual.(*sync.Mutex)
}
