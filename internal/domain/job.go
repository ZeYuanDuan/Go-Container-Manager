package domain

import (
	"context"
	"time"
)

type Job struct {
	ID               string
	UserID           string
	TargetResourceID string
	Type             string
	Status           string
	ErrorMessage     *string
	CreatedAt        time.Time
	CompletedAt      *time.Time
}

const (
	JobTypeCreateContainer = "CREATE_CONTAINER"
	JobTypeStartContainer  = "START_CONTAINER"
	JobTypeStopContainer   = "STOP_CONTAINER"
	JobTypeDeleteContainer = "DELETE_CONTAINER"
)

const (
	JobStatusPending = "PENDING"
	JobStatusRunning = "RUNNING"
	JobStatusSuccess = "SUCCESS"
	JobStatusFailed  = "FAILED"
)

// JobRetention is the duration after which completed jobs are eligible for cleanup.
const JobRetention = 7 * 24 * time.Hour

type JobRepository interface {
	Save(ctx context.Context, job *Job) error
	FindByIDAndUserID(ctx context.Context, jobID, userID string) (*Job, error)
	FindPendingJobs(ctx context.Context) ([]*Job, error)
	FindPendingOrRunningByTarget(ctx context.Context, targetResourceID string) (*Job, error)
	MarkRunningAsFailed(ctx context.Context, msg string) error
	Update(ctx context.Context, job *Job) error
	// DeleteExpired removes completed/failed jobs older than the given retention period.
	DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error)
}
