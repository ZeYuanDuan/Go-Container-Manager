package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/alsonduan/go-container-manager/internal/domain"
)

type Worker struct {
	jobRepo         domain.JobRepository
	containerRepo   domain.ContainerRepository
	runtime         domain.ContainerRuntime
	envMap          map[string]string
	storageRoot     string
	storageHostPath string // for Docker bind mount; when set (server in container), use this for Docker API
	jobChan         chan *domain.Job
	quit            chan struct{}
	wg              sync.WaitGroup
}

func NewWorker(
	jobRepo domain.JobRepository,
	containerRepo domain.ContainerRepository,
	runtime domain.ContainerRuntime,
	envMap map[string]string,
	storageRoot string,
	storageHostPath string,
	jobChan chan *domain.Job,
	poolSize int,
) *Worker {
	if storageHostPath == "" {
		storageHostPath = storageRoot
	}
	w := &Worker{
		jobRepo:         jobRepo,
		containerRepo:   containerRepo,
		runtime:         runtime,
		envMap:          envMap,
		storageRoot:     storageRoot,
		storageHostPath: storageHostPath,
		jobChan:         jobChan,
		quit:            make(chan struct{}),
	}
	w.wg.Add(poolSize)
	for i := 0; i < poolSize; i++ {
		go w.run()
	}
	return w
}

func (w *Worker) Stop() {
	close(w.quit)
	w.wg.Wait()
}

func (w *Worker) run() {
	defer w.wg.Done()
	for {
		select {
		case <-w.quit:
			return
		case job, ok := <-w.jobChan:
			if !ok {
				return
			}
			w.process(job)
		}
	}
}

func (w *Worker) process(job *domain.Job) {
	ctx := context.Background()

	// Mark as RUNNING
	job.Status = domain.JobStatusRunning
	if err := w.jobRepo.Update(ctx, job); err != nil {
		slog.Error("failed to mark job as RUNNING", "job_id", job.ID, "error", err)
	}

	var processErr error
	switch job.Type {
	case domain.JobTypeCreateContainer:
		processErr = w.processCreate(ctx, job)
	case domain.JobTypeStartContainer:
		processErr = w.processStart(ctx, job)
	case domain.JobTypeStopContainer:
		processErr = w.processStop(ctx, job)
	case domain.JobTypeDeleteContainer:
		processErr = w.processDelete(ctx, job)
	}

	now := time.Now().UTC()
	job.CompletedAt = &now
	if processErr != nil {
		job.Status = domain.JobStatusFailed
		msg := processErr.Error()
		job.ErrorMessage = &msg
	} else {
		job.Status = domain.JobStatusSuccess
	}
	if err := w.jobRepo.Update(ctx, job); err != nil {
		slog.Error("failed to update job final status", "job_id", job.ID, "status", job.Status, "error", err)
	}
}

const updateRetries = 3
const updateRetryDelay = 100 * time.Millisecond

func (w *Worker) retryUpdate(ctx context.Context, c *domain.Container) error {
	var lastErr error
	for i := 0; i < updateRetries; i++ {
		if err := w.containerRepo.Update(ctx, c); err != nil {
			lastErr = err
			if i < updateRetries-1 {
				time.Sleep(updateRetryDelay)
			}
			continue
		}
		return nil
	}
	return lastErr
}

func (w *Worker) retryDelete(ctx context.Context, containerID string) error {
	var lastErr error
	for i := 0; i < updateRetries; i++ {
		if err := w.containerRepo.Delete(ctx, containerID); err != nil {
			lastErr = err
			if i < updateRetries-1 {
				time.Sleep(updateRetryDelay)
			}
			continue
		}
		return nil
	}
	return lastErr
}

func (w *Worker) processCreate(ctx context.Context, job *domain.Job) error {
	container, err := w.containerRepo.FindByID(ctx, job.TargetResourceID)
	if err != nil {
		return err
	}
	// Create dir at storageRoot (container path for file svc); Docker API needs host path
	storageDir := filepath.Join(w.storageRoot, container.UserID)
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return err
	}
	hostMountPath := filepath.Join(w.storageHostPath, container.UserID)
	image := w.envMap[container.Environment]
	dockerID, err := w.runtime.CreateAndStart(ctx, container, image, hostMountPath)
	if err != nil {
		return err
	}
	container.DockerContainerID = dockerID
	// Wait briefly for short-lived commands to exit, then sync actual Docker state.
	container.Status = w.syncContainerState(ctx, dockerID)
	container.UpdatedAt = time.Now().UTC()
	return w.retryUpdate(ctx, container)
}

func (w *Worker) processStart(ctx context.Context, job *domain.Job) error {
	container, err := w.containerRepo.FindByID(ctx, job.TargetResourceID)
	if err != nil {
		return err
	}
	if container.DockerContainerID == "" {
		return fmt.Errorf("container has no Docker container ID (create job may have failed)")
	}
	if err := w.runtime.Start(ctx, container.DockerContainerID); err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("Docker container %s not found (may have been removed): %w", container.DockerContainerID, err)
		}
		return err
	}
	// Wait briefly for short-lived commands to exit, then sync actual Docker state.
	container.Status = w.syncContainerState(ctx, container.DockerContainerID)
	container.UpdatedAt = time.Now().UTC()
	return w.retryUpdate(ctx, container)
}

func (w *Worker) processStop(ctx context.Context, job *domain.Job) error {
	container, err := w.containerRepo.FindByID(ctx, job.TargetResourceID)
	if err != nil {
		return err
	}
	if err := w.runtime.Stop(ctx, container.DockerContainerID); err != nil {
		// 304 Not Modified: container already stopped — treat as success
		if !errdefs.IsNotModified(err) {
			return err
		}
	}
	container.Status = domain.ContainerStatusExited
	container.UpdatedAt = time.Now().UTC()
	return w.retryUpdate(ctx, container)
}

func (w *Worker) processDelete(ctx context.Context, job *domain.Job) error {
	container, err := w.containerRepo.FindByID(ctx, job.TargetResourceID)
	if err != nil {
		return err
	}
	// Remove from Docker first. If already gone (e.g. manual deletion), still clean DB.
	if container.DockerContainerID != "" {
		if err := w.runtime.Remove(ctx, container.DockerContainerID); err != nil && !errdefs.IsNotFound(err) {
			return err
		}
	}
	return w.retryDelete(ctx, job.TargetResourceID)
}

const stateWaitTimeout = 500 * time.Millisecond

// syncContainerState waits briefly for short-lived containers (e.g. echo, python -c)
// to exit, then inspects the actual Docker state. Returns the domain status string.
func (w *Worker) syncContainerState(ctx context.Context, dockerID string) string {
	if w.runtime.WaitExit(ctx, dockerID, stateWaitTimeout) {
		return domain.ContainerStatusExited
	}
	// Container is still running after timeout — confirm via inspect.
	if state, err := w.runtime.InspectState(ctx, dockerID); err == nil {
		if state == "running" {
			return domain.ContainerStatusRunning
		}
		return domain.ContainerStatusExited
	}
	return domain.ContainerStatusRunning // fallback
}
