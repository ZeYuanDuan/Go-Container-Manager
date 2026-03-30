package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
)

// RespondError writes the appropriate HTTP error response for the given error.
// AppError maps to its HTTPStatus and code (supports wrapped errors); other errors return 500 INTERNAL_ERROR.
func RespondError(c *gin.Context, err error) {
	var appErr *apperr.AppError
	if errors.As(err, &appErr) {
		c.JSON(appErr.HTTPStatus, gin.H{
			"error": gin.H{
				"code":    appErr.Code,
				"message": appErr.Message,
			},
		})
		return
	}
	// Also check for MaxBytesError (413) when body exceeds limit
	if isMaxBytesError(err) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": gin.H{
				"code":    "FILE_TOO_LARGE",
				"message": "File size exceeds 50MB limit",
			},
		})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"code":    "INTERNAL_ERROR",
			"message": "Internal server error",
		},
	})
}

func isMaxBytesError(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}
