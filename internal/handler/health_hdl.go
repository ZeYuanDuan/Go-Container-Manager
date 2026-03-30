package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	environments []string
}

func NewHealthHandler(environments []string) *HealthHandler {
	return &HealthHandler{environments: environments}
}

func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "UP"})
}

func (h *HealthHandler) Environments(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"environments": h.environments})
}
