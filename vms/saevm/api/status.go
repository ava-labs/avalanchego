// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"errors"
	"fmt"
)

type Status uint32

const (
	Unknown Status = iota
	Accepted
	invalid
)

func (s Status) MarshalJSON() ([]byte, error) {
	if err := s.Valid(); err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("%q", s)), nil
}

func (s *Status) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == `null` {
		return nil
	}
	switch str {
	case `"Unknown"`:
		*s = Unknown
	case `"Accepted"`:
		*s = Accepted
	default:
		return errInvalidStatus
	}
	return nil
}

var errInvalidStatus = errors.New("invalid status")

func (s Status) Valid() error {
	if s >= invalid {
		return errInvalidStatus
	}
	return nil
}

func (s Status) String() string {
	switch s {
	case Unknown:
		return "Unknown"
	case Accepted:
		return "Accepted"
	default:
		return fmt.Sprintf("Invalid <%d>", s)
	}
}
