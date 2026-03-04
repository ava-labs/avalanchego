// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "fmt"

var (
	_ error = (*AppError)(nil)

	// ErrUndefined indicates an undefined error
	ErrUndefined = &AppError{
		Code:    0,
		Message: "undefined",
	}

	// ErrTimeout is used to signal a response timeout
	ErrTimeout = &AppError{
		Code:    -1,
		Message: "timed out",
	}
)

// AppError is an application-defined error
type AppError struct {
	// Code is application-defined and should be used for error matching
	Code int32
	// Message is a human-readable error message
	Message string
}

func (a *AppError) Error() string {
	return fmt.Sprintf("%d: %s", a.Code, a.Message)
}

func (a *AppError) Is(target error) bool {
	appErr, ok := target.(*AppError)
	if !ok {
		return false
	}

	return a.Code == appErr.Code
}
