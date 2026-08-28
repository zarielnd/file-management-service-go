package apperror

import "net/http"

type Code string

const (
	CodeInvalidRequest    Code = "INVALID_REQUEST"
	CodeFileNotFound      Code = "FILE_NOT_FOUND"
	CodeFileTooLarge      Code = "FILE_TOO_LARGE"
	CodeInternalError     Code = "INTERNAL_ERROR"
	CodeForbiddenError    Code = "FORBIDDEN"
	CodeUnauthorizedError Code = "UNAUTHORIZED"
)

type Error struct {
	Code       Code
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string {
	return e.Message
}

func Invalid(message string) *Error {
	return &Error{
		Code:       CodeInvalidRequest,
		Message:    message,
		HTTPStatus: http.StatusBadRequest,
	}
}

func NotFound(message string) *Error {
	return &Error{
		Code:       CodeFileNotFound,
		Message:    message,
		HTTPStatus: http.StatusNotFound,
	}
}

func TooLarge(message string) *Error {
	return &Error{
		Code:       CodeFileTooLarge,
		Message:    message,
		HTTPStatus: http.StatusRequestEntityTooLarge,
	}
}

func Internal(message string) *Error {
	return &Error{
		Code:       CodeInternalError,
		Message:    message,
		HTTPStatus: http.StatusInternalServerError,
	}
}

func Forbidden(message string) *Error {
	return &Error{
		Code:       CodeForbiddenError,
		Message:    message,
		HTTPStatus: http.StatusForbidden,
	}
}

func Unauthorized(message string) *Error {
	return &Error{
		Code:       CodeForbiddenError,
		Message:    message,
		HTTPStatus: http.StatusUnauthorized,
	}
}
