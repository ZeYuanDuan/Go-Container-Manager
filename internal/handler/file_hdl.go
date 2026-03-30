package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/alsonduan/go-container-manager/internal/service"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
)

type FileHandler struct {
	fileSvc *service.FileService
}

func NewFileHandler(fileSvc *service.FileService) *FileHandler {
	return &FileHandler{fileSvc: fileSvc}
}

func (h *FileHandler) Upload(c *gin.Context) {
	userID := c.GetString("userID")

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		if isMaxBytesError(err) {
			RespondError(c, apperr.ErrFileTooLarge)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "BAD_REQUEST",
				"message": "No file provided",
			},
		})
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		if isMaxBytesError(err) {
			RespondError(c, apperr.ErrFileTooLarge)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "BAD_REQUEST",
				"message": "Failed to read file",
			},
		})
		return
	}

	result, err := h.fileSvc.Upload(c.Request.Context(), userID, header.Filename, content)
	if err != nil {
		// Log internal errors for debugging (e.g. permission denied when server in Docker)
		var appErr *apperr.AppError
		if !errors.As(err, &appErr) && !isMaxBytesError(err) {
			slog.Error("file upload failed", "error", err, "filename", header.Filename, "userID", userID)
		}
		RespondError(c, err) // handles AppError and MaxBytesError (wrapped)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"file_id":    result.ID,
		"filename":   result.OriginalFilename,
		"size_bytes": result.SizeBytes,
		"created_at": result.CreatedAt,
	})
}

func (h *FileHandler) List(c *gin.Context) {
	userID := c.GetString("userID")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	files, total, err := h.fileSvc.List(c.Request.Context(), userID, page, limit)
	if err != nil {
		RespondError(c, err)
		return
	}

	data := make([]gin.H, 0, len(files))
	for _, f := range files {
		data = append(data, gin.H{
			"file_id":    f.ID,
			"filename":   f.OriginalFilename,
			"size_bytes": f.SizeBytes,
			"created_at": f.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  data,
		"total": total,
	})
}

func (h *FileHandler) Delete(c *gin.Context) {
	userID := c.GetString("userID")
	fileID := c.Param("file_id")

	err := h.fileSvc.Delete(c.Request.Context(), userID, fileID)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
