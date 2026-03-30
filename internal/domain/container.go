package domain

import (
	"context"
	"time"
)

type Container struct {
	ID                string
	UserID            string
	Name              string
	Environment       string
	Command           []string
	Status            string
	MountPath         string
	DockerContainerID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

const (
	ContainerStatusCreated = "Created"
	ContainerStatusRunning = "Running"
	ContainerStatusExited  = "Exited"
	ContainerStatusDead    = "Dead"
)

type ContainerRepository interface {
	Save(ctx context.Context, container *Container) error
	FindByUserID(ctx context.Context, userID string, page, limit int) ([]*Container, int, error)
	FindByID(ctx context.Context, containerID string) (*Container, error)
	FindByIDForUpdate(ctx context.Context, containerID string) (*Container, error)
	Update(ctx context.Context, container *Container) error
	Delete(ctx context.Context, containerID string) error
	FindByUserIDAndName(ctx context.Context, userID, name string) (*Container, error)
}

type ContainerRuntime interface {
	// CreateAndStart creates a container with bind mount: hostMountPath -> container.MountPath (/workspace).
	// hostMountPath is the user's storage folder on host, e.g. {STORAGE_ROOT}/{user_id}/.
	CreateAndStart(ctx context.Context, container *Container, image string, hostMountPath string) (dockerID string, err error)
	Start(ctx context.Context, dockerContainerID string) error
	Stop(ctx context.Context, dockerContainerID string) error
	Remove(ctx context.Context, dockerContainerID string) error
	// InspectState returns the actual Docker container state (e.g. running, exited).
	InspectState(ctx context.Context, dockerContainerID string) (state string, err error)
	// WaitExit waits for the container to exit, up to the given timeout.
	// Returns true if the container exited, false if still running after timeout.
	WaitExit(ctx context.Context, dockerContainerID string, timeout time.Duration) (exited bool)
}
