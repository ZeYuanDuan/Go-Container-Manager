package errors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppError_ErrorReturnsMessage(t *testing.T) {
	err := New(400, "CODE", "some message")
	assert.Equal(t, "some message", err.Error())
}

func TestNewf_FormatsMessage(t *testing.T) {
	err := Newf(400, "CODE", "val=%d", 42)
	assert.Equal(t, "val=42", err.Message)
	assert.Equal(t, 400, err.HTTPStatus)
	assert.Equal(t, "CODE", err.Code)
}

func TestErrorsAs_WrappedAppError(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", ErrUnauthorized)
	var target *AppError
	assert.True(t, errors.As(wrapped, &target))
	assert.Equal(t, "UNAUTHORIZED", target.Code)
	assert.Equal(t, 401, target.HTTPStatus)
}

func TestSentinels_CorrectStatusAndCode(t *testing.T) {
	tests := []struct {
		err    *AppError
		status int
		code   string
	}{
		{ErrUnauthorized, 401, "UNAUTHORIZED"},
		{ErrBadRequest, 400, "BAD_REQUEST"},
		{ErrForbidden, 403, "FORBIDDEN"},
		{ErrFileTooLarge, 413, "FILE_TOO_LARGE"},
		{ErrUnsupportedMIME, 415, "UNSUPPORTED_MIME_TYPE"},
		{ErrFileNotFound, 404, "FILE_NOT_FOUND"},
		{ErrContainerNotFound, 404, "CONTAINER_NOT_FOUND"},
		{ErrContainerNameDup, 409, "CONTAINER_NAME_DUPLICATE"},
		{ErrConflict, 409, "CONFLICT"},
		{ErrJobNotFound, 404, "JOB_NOT_FOUND"},
		{ErrInvalidEnvironment, 400, "INVALID_ENVIRONMENT"},
		{ErrServiceUnavailable, 503, "SERVICE_UNAVAILABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			assert.Equal(t, tt.status, tt.err.HTTPStatus)
			assert.Equal(t, tt.code, tt.err.Code)
		})
	}
}
