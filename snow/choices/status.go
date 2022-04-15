// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package choices

import (
	"errors"

	"github.com/chain4travel/caminogo/utils/wrappers"
)

var errUnknownStatus = errors.New("unknown status")

type Status uint32

// List of possible status values
// [Unknown] Zero value, means the status is not known
// [Processing] means the operation is known, but hasn't been decided yet
// [Rejected] means the operation will never be accepted
// [Accepted] means the operation was accepted
const (
	Unknown Status = iota
	Processing
	Rejected
	Accepted
)

func (s Status) MarshalJSON() ([]byte, error) {
	if err := s.Valid(); err != nil {
		return nil, err
	}
	return []byte("\"" + s.String() + "\""), nil
}

func (s *Status) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == "null" {
		return nil
	}
	switch str {
	case "\"Unknown\"":
		*s = Unknown
	case "\"Processing\"":
		*s = Processing
	case "\"Rejected\"":
		*s = Rejected
	case "\"Accepted\"":
		*s = Accepted
	default:
		return errUnknownStatus
	}
	return nil
}

// Fetched returns true if the status has been set.
func (s Status) Fetched() bool {
	switch s {
	case Processing:
		return true
	default:
		return s.Decided()
	}
}

// Decided returns true if the status is Rejected or Accepted.
func (s Status) Decided() bool {
	switch s {
	case Rejected, Accepted:
		return true
	default:
		return false
	}
}

// Valid returns nil if the status is a valid status.
func (s Status) Valid() error {
	switch s {
	case Unknown, Processing, Rejected, Accepted:
		return nil
	default:
		return errUnknownStatus
	}
}

func (s Status) String() string {
	switch s {
	case Unknown:
		return "Unknown"
	case Processing:
		return "Processing"
	case Rejected:
		return "Rejected"
	case Accepted:
		return "Accepted"
	default:
		return "Invalid status"
	}
}

// Bytes returns the byte repr. of this status
func (s Status) Bytes() []byte {
	p := wrappers.Packer{Bytes: make([]byte, 4)}
	p.PackInt(uint32(s))
	return p.Bytes
}
