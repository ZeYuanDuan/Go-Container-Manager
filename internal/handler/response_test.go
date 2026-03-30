package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	apperr "github.com/alsonduan/go-container-manager/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func callRespondError(err error) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	RespondError(c, err)
	return w
}

func TestRespondError_AppError(t *testing.T) {
	w := callRespondError(apperr.ErrFileNotFound)
	assert.Equal(t, 404, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errObj := body["error"].(map[string]interface{})
	assert.Equal(t, "FILE_NOT_FOUND", errObj["code"])
}

func TestRespondError_WrappedAppError(t *testing.T) {
	wrapped := fmt.Errorf("something failed: %w", apperr.ErrForbidden)
	w := callRespondError(wrapped)
	assert.Equal(t, 403, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errObj := body["error"].(map[string]interface{})
	assert.Equal(t, "FORBIDDEN", errObj["code"])
}

func TestRespondError_UnknownError_500(t *testing.T) {
	w := callRespondError(errors.New("unexpected"))
	assert.Equal(t, 500, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errObj := body["error"].(map[string]interface{})
	assert.Equal(t, "INTERNAL_ERROR", errObj["code"])
}

func TestRespondError_MaxBytesError_413(t *testing.T) {
	// Simulate MaxBytesError by wrapping http.MaxBytesError
	maxErr := &http.MaxBytesError{Limit: 50 * 1024 * 1024}
	w := callRespondError(maxErr)
	assert.Equal(t, 413, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errObj := body["error"].(map[string]interface{})
	assert.Equal(t, "FILE_TOO_LARGE", errObj["code"])
}
