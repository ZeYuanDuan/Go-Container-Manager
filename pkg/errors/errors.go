package errors

import "fmt"

type AppError struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *AppError) Error() string {
	return e.Message
}

func New(httpStatus int, code, message string) *AppError {
	return &AppError{HTTPStatus: httpStatus, Code: code, Message: message}
}

func Newf(httpStatus int, code, format string, args ...interface{}) *AppError {
	return &AppError{HTTPStatus: httpStatus, Code: code, Message: fmt.Sprintf(format, args...)}
}

var (
	ErrUnauthorized       = New(401, "UNAUTHORIZED", "Invalid credentials")
	ErrBadRequest         = New(400, "BAD_REQUEST", "Invalid request")
	ErrForbidden          = New(403, "FORBIDDEN", "You do not have permission")
	ErrFileTooLarge       = New(413, "FILE_TOO_LARGE", "File size exceeds 50MB limit")
	ErrUnsupportedMIME    = New(415, "UNSUPPORTED_MIME_TYPE", "File type not allowed. Allowed: image/jpeg, image/png, text/csv, application/json, text/plain")
	ErrFileNotFound       = New(404, "FILE_NOT_FOUND", "File does not exist or has been deleted")
	ErrContainerNotFound  = New(404, "CONTAINER_NOT_FOUND", "Container does not exist")
	ErrContainerNameDup   = New(409, "CONTAINER_NAME_DUPLICATE", "Container name already exists for this user")
	ErrConflict           = New(409, "CONFLICT", "Resource conflict")
	ErrJobNotFound        = New(404, "JOB_NOT_FOUND", "Job does not exist or has expired")
	ErrInvalidEnvironment = New(400, "INVALID_ENVIRONMENT", "Invalid environment specified")
	ErrServiceUnavailable = New(503, "SERVICE_UNAVAILABLE", "Job queue is full, please retry later")
)
