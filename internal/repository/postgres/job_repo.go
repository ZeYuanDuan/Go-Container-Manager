package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/alsonduan/go-container-manager/internal/domain"
)

type JobRepository struct {
	pool *pgxpool.Pool
}

func NewJobRepository(pool *pgxpool.Pool) *JobRepository {
	return &JobRepository{pool: pool}
}

func (r *JobRepository) Save(ctx context.Context, job *domain.Job) error {
	q := getQueryable(ctx, r.pool)
	_, err := q.Exec(ctx,
		`INSERT INTO jobs (id, user_id, target_resource_id, type, status, error_message, created_at, completed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		job.ID, job.UserID, job.TargetResourceID, job.Type, job.Status, job.ErrorMessage, job.CreatedAt, job.CompletedAt,
	)
	return err
}

func (r *JobRepository) FindByIDAndUserID(ctx context.Context, jobID, userID string) (*domain.Job, error) {
	q := getQueryable(ctx, r.pool)
	var j domain.Job
	err := q.QueryRow(ctx,
		`SELECT id, user_id, target_resource_id, type, status, error_message, created_at, completed_at
		 FROM jobs WHERE id = $1 AND user_id = $2`,
		jobID, userID,
	).Scan(&j.ID, &j.UserID, &j.TargetResourceID, &j.Type, &j.Status, &j.ErrorMessage, &j.CreatedAt, &j.CompletedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &j, nil
}

func (r *JobRepository) FindPendingJobs(ctx context.Context) ([]*domain.Job, error) {
	q := getQueryable(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT id, user_id, target_resource_id, type, status, error_message, created_at, completed_at
		 FROM jobs WHERE status = $1 ORDER BY created_at ASC`,
		domain.JobStatusPending,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (r *JobRepository) FindPendingOrRunningByTarget(ctx context.Context, targetResourceID string) (*domain.Job, error) {
	q := getQueryable(ctx, r.pool)
	rows, err := q.Query(ctx,
		`SELECT id, user_id, target_resource_id, type, status, error_message, created_at, completed_at
		 FROM jobs WHERE target_resource_id = $1 AND status IN ('PENDING', 'RUNNING') LIMIT 1`,
		targetResourceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, domain.ErrNotFound
	}
	j, err := scanJob(rows)
	if err != nil {
		return nil, err
	}
	return j, rows.Err()
}

func (r *JobRepository) MarkRunningAsFailed(ctx context.Context, msg string) error {
	q := getQueryable(ctx, r.pool)
	_, err := q.Exec(ctx,
		`UPDATE jobs SET status = $1, error_message = $2 WHERE status = $3`,
		domain.JobStatusFailed, msg, domain.JobStatusRunning,
	)
	return err
}

func (r *JobRepository) Update(ctx context.Context, job *domain.Job) error {
	q := getQueryable(ctx, r.pool)
	ct, err := q.Exec(ctx,
		`UPDATE jobs SET status = $2, error_message = $3, completed_at = $4 WHERE id = $1`,
		job.ID, job.Status, job.ErrorMessage, job.CompletedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *JobRepository) DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	q := getQueryable(ctx, r.pool)
	cutoff := time.Now().UTC().Add(-olderThan)
	ct, err := q.Exec(ctx,
		`DELETE FROM jobs WHERE status IN ($1, $2) AND completed_at < $3`,
		domain.JobStatusSuccess, domain.JobStatusFailed, cutoff,
	)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

func scanJob(rows pgx.Rows) (*domain.Job, error) {
	var j domain.Job
	err := rows.Scan(&j.ID, &j.UserID, &j.TargetResourceID, &j.Type, &j.Status, &j.ErrorMessage, &j.CreatedAt, &j.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &j, nil
}
