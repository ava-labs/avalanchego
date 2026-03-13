// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"errors"
)

type Status uint32

const (
	Unknown Status = iota
	Accepted
)

var errInvalidStatus = errors.New("invalid status")

func (s Status) MarshalJSON() ([]byte, error) {
	switch s {
	case Unknown:
		return []byte(`"Unknown"`), nil
	case Accepted:
		return []byte(`"Accepted"`), nil
	default:
		return nil, errInvalidStatus
	}
}

func (s *Status) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `null`:
	case `"Unknown"`:
		*s = Unknown
	case `"Accepted"`:
		*s = Accepted
	default:
		return errInvalidStatus
	}
	return nil
}
