package apierrors

// For Go code

import "errors"

var (
	ErrTestNotFound = errors.New("test not found")
	ErrValidation   = errors.New("validation failed")
	ErrConflict     = errors.New("conflict")
	ErrInternal     = errors.New("internal error")
	ErrFailedConn   = errors.New("failed to create data")
	ErrWorkerNotFound = errors.New("worker not found")
)
