// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import "errors"

var (
	// ErrEntryNotFound indicates the requested key/value was not present in the DB.
	ErrEntryNotFound = errors.New("customrawdb: entry not found")
	// ErrInvalidData indicates the stored value exists but is malformed or undecodable.
	ErrInvalidData = errors.New("customrawdb: invalid data")
	// ErrStateSchemeConflict indicates the provided state scheme conflicts with what is on disk.
	ErrStateSchemeConflict = errors.New("customrawdb: state scheme conflict")
	// ErrInvalidArgument indicates a caller-provided argument was invalid.
	ErrInvalidArgument = errors.New("customrawdb: invalid argument")
)
