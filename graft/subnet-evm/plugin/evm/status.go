// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
)

var errUnknownStatus = errors.New("unknown status")

// Status ...
type Status uint32

// List of possible status values
// [Unknown] Zero value, means the status is not known
// [Dropped] means the transaction was in the mempool, but was dropped because it failed verification
// [Processing] means the transaction is in the mempool
// [Accepted] means the transaction was accepted
const (
	Unknown Status = iota
	Dropped
	Processing
	Accepted
)

// MarshalJSON ...
func (s Status) MarshalJSON() ([]byte, error) {
	if err := s.Valid(); err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("%q", s)), nil
}

// UnmarshalJSON ...
func (s *Status) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == "null" {
		return nil
	}
	switch str {
	case `"Unknown"`:
		*s = Unknown
	case `"Dropped"`:
		*s = Dropped
	case `"Processing"`:
		*s = Processing
	case `"Accepted"`:
		*s = Accepted
	default:
		return errUnknownStatus
	}
	return nil
}

// Valid returns nil if the status is a valid status.
func (s Status) Valid() error {
	switch s {
	case Unknown, Dropped, Processing, Accepted:
		return nil
	default:
		return errUnknownStatus
	}
}

func (s Status) String() string {
	switch s {
	case Unknown:
		return "Unknown"
	case Dropped:
		return "Dropped"
	case Processing:
		return "Processing"
	case Accepted:
		return "Accepted"
	default:
		return "Invalid status"
	}
}
