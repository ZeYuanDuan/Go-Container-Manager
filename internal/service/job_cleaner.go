package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/alsonduan/go-container-manager/internal/domain"
)

// JobCleaner periodically deletes expired jobs (7-day retention).
type JobCleaner struct {
	jobRepo  domain.JobRepository
	interval time.Duration
	quit     chan struct{}
	done     chan struct{}
}

// NewJobCleaner creates and starts a background goroutine that cleans up
// completed/failed jobs older than domain.JobRetention.
func NewJobCleaner(jobRepo domain.JobRepository, interval time.Duration) *JobCleaner {
	c := &JobCleaner{
		jobRepo:  jobRepo,
		interval: interval,
		quit:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go c.run()
	return c
}

func (c *JobCleaner) Stop() {
	close(c.quit)
	<-c.done
}

func (c *JobCleaner) run() {
	defer close(c.done)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.quit:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			n, err := c.jobRepo.DeleteExpired(ctx, domain.JobRetention)
			cancel()
			if err != nil {
				slog.Error("job cleanup failed", "error", err)
			} else if n > 0 {
				slog.Info("expired jobs cleaned up", "count", n)
			}
		}
	}
}
