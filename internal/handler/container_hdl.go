package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/alsonduan/go-container-manager/internal/service"
)

type ContainerHandler struct {
	containerSvc *service.ContainerService
}

func NewContainerHandler(containerSvc *service.ContainerService) *ContainerHandler {
	return &ContainerHandler{containerSvc: containerSvc}
}

func (h *ContainerHandler) Create(c *gin.Context) {
	userID := c.GetString("userID")
	var req struct {
		Name        string   `json:"name" binding:"required"`
		Environment string   `json:"environment" binding:"required"`
		Command     []string `json:"command" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "BAD_REQUEST",
				"message": "Invalid request body",
			},
		})
		return
	}

	job, err := h.containerSvc.Create(c.Request.Context(), userID, req.Name, req.Environment, req.Command)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Container creation job submitted",
		"job_id":  job.ID,
	})
}

func (h *ContainerHandler) List(c *gin.Context) {
	userID := c.GetString("userID")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	containers, total, err := h.containerSvc.List(c.Request.Context(), userID, page, limit)
	if err != nil {
		RespondError(c, err)
		return
	}

	data := make([]gin.H, 0, len(containers))
	for _, ct := range containers {
		data = append(data, gin.H{
			"container_id": ct.ID,
			"name":         ct.Name,
			"environment":  ct.Environment,
			"status":       ct.Status,
			"created_at":   ct.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  data,
		"total": total,
	})
}

func (h *ContainerHandler) Get(c *gin.Context) {
	userID := c.GetString("userID")
	containerID := c.Param("container_id")

	ct, err := h.containerSvc.Get(c.Request.Context(), userID, containerID)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"container_id": ct.ID,
		"name":         ct.Name,
		"environment":  ct.Environment,
		"command":      ct.Command,
		"status":       ct.Status,
		"mount_path":   ct.MountPath,
		"created_at":   ct.CreatedAt,
	})
}

func (h *ContainerHandler) Start(c *gin.Context) {
	userID := c.GetString("userID")
	containerID := c.Param("container_id")

	job, err := h.containerSvc.Start(c.Request.Context(), userID, containerID)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Container start job submitted",
		"job_id":  job.ID,
	})
}

func (h *ContainerHandler) Stop(c *gin.Context) {
	userID := c.GetString("userID")
	containerID := c.Param("container_id")

	job, err := h.containerSvc.Stop(c.Request.Context(), userID, containerID)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Container stop job submitted",
		"job_id":  job.ID,
	})
}

func (h *ContainerHandler) Delete(c *gin.Context) {
	userID := c.GetString("userID")
	containerID := c.Param("container_id")

	job, err := h.containerSvc.Delete(c.Request.Context(), userID, containerID)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Container delete job submitted",
		"job_id":  job.ID,
	})
}
