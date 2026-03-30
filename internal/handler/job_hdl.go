package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/alsonduan/go-container-manager/internal/service"
)

type JobHandler struct {
	jobSvc *service.JobService
}

func NewJobHandler(jobSvc *service.JobService) *JobHandler {
	return &JobHandler{jobSvc: jobSvc}
}

func (h *JobHandler) GetJob(c *gin.Context) {
	userID := c.GetString("userID")
	jobID := c.Param("job_id")

	job, err := h.jobSvc.GetJob(c.Request.Context(), userID, jobID)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"job_id":             job.ID,
		"target_resource_id": job.TargetResourceID,
		"type":               job.Type,
		"status":             job.Status,
		"error_message":      job.ErrorMessage,
		"created_at":         job.CreatedAt,
		"completed_at":       job.CompletedAt,
	})
}
