// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import "errors"

// common errors
var (
	ErrClosed   = errors.New("closed")
	ErrNotFound = errors.New("not found")
	// ErrPrevNotSupported is reported by [Iterator.Error] after a call to
	// [Iterator.Prev] on an iterator that does not support backward
	// iteration.
	ErrPrevNotSupported = errors.New("backward iteration not supported")
)
