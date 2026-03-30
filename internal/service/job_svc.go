package service

import (
	"context"

	"github.com/alsonduan/go-container-manager/internal/domain"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
)

type JobService struct {
	jobRepo domain.JobRepository
}

func NewJobService(jobRepo domain.JobRepository) *JobService {
	return &JobService{jobRepo: jobRepo}
}

func (s *JobService) GetJob(ctx context.Context, userID, jobID string) (*domain.Job, error) {
	job, err := s.jobRepo.FindByIDAndUserID(ctx, jobID, userID)
	if err != nil {
		return nil, apperr.ErrJobNotFound
	}
	return job, nil
}
