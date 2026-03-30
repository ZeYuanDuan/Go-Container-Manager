package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/alsonduan/go-container-manager/internal/handler/middleware"
	appjwt "github.com/alsonduan/go-container-manager/pkg/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-key-at-least-32-characters-long"

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.AuthMiddleware(testSecret))
	r.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"userID": c.GetString("userID")})
	})
	return r
}

func TestAuthMiddleware_MissingHeader_401(t *testing.T) {
	r := setupRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errObj := body["error"].(map[string]interface{})
	assert.Equal(t, "UNAUTHORIZED", errObj["code"])
}

func TestAuthMiddleware_NonBearerScheme_401(t *testing.T) {
	r := setupRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjE6cGFzcw==")
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestAuthMiddleware_InvalidToken_401(t *testing.T) {
	r := setupRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer not.a.real.jwt")
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestAuthMiddleware_ExpiredToken_401(t *testing.T) {
	token, err := appjwt.GenerateToken("user-123", testSecret, -time.Second)
	require.NoError(t, err)

	r := setupRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestAuthMiddleware_ValidToken_SetsUserID(t *testing.T) {
	token, err := appjwt.GenerateToken("user-123", testSecret, time.Hour)
	require.NoError(t, err)

	r := setupRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "user-123", body["userID"])
}
