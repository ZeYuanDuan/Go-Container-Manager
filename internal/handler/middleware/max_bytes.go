package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const maxRequestBodyBytes = 50 * 1024 * 1024 // 50MB

// MaxBytes limits the request body size to prevent OOM from large uploads.
// Requests exceeding the limit will fail with 413 Request Entity Too Large.
func MaxBytes(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

// MaxBytesDefault returns a middleware with the default 50MB limit.
func MaxBytesDefault() gin.HandlerFunc {
	return MaxBytes(maxRequestBodyBytes)
}
