package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/alsonduan/go-container-manager/internal/service"
)

type AuthHandler struct {
	authSvc *service.AuthService
}

func NewAuthHandler(authSvc *service.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
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

	token, expiresIn, err := h.authSvc.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":      token,
		"expires_in": expiresIn,
	})
}
