package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alsonduan/go-container-manager/internal/domain"
	"github.com/stretchr/testify/assert"
)

type spyJobRepo struct {
	stubJobRepo
	mu             sync.Mutex
	deleteExpCalls int
	deleteExpErr   error
	deleteExpN     int64
}

func (m *spyJobRepo) DeleteExpired(_ context.Context, _ time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteExpCalls++
	return m.deleteExpN, m.deleteExpErr
}

func (m *spyJobRepo) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deleteExpCalls
}

func TestJobCleaner_RunsAndStops(t *testing.T) {
	repo := &spyJobRepo{}
	c := NewJobCleaner(repo, 10*time.Millisecond)
	// Wait enough time for at least one tick
	time.Sleep(50 * time.Millisecond)
	c.Stop()
	assert.GreaterOrEqual(t, repo.calls(), 1)
}

func TestJobCleaner_HandlesErrors(t *testing.T) {
	repo := &spyJobRepo{deleteExpErr: errors.New("db error")}
	c := NewJobCleaner(repo, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	c.Stop()
	// Should not panic even with errors
	assert.GreaterOrEqual(t, repo.calls(), 1)
}

// Ensure spyJobRepo also implements full JobRepository interface
var _ domain.JobRepository = (*spyJobRepo)(nil)
