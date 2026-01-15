// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package status

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

// List of possible status values:
// - [Unknown] The transaction is not known
// - [Committed] The transaction was proposed and committed
// - [Aborted] The transaction was proposed and aborted
// - [Processing] The transaction was proposed and is currently in the preferred chain
// - [Dropped] The transaction was dropped due to failing verification
const (
	Unknown    Status = 0
	Committed  Status = 4
	Aborted    Status = 5
	Processing Status = 6
	Dropped    Status = 8
)

var (
	errUnknownStatus = errors.New("unknown status")

	_ json.Marshaler    = Status(0)
	_ verify.Verifiable = Status(0)
	_ fmt.Stringer      = Status(0)
)

type Status uint32

func (s Status) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), s.Verify()
}

func (s *Status) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `"Unknown"`:
		*s = Unknown
	case `"Committed"`:
		*s = Committed
	case `"Aborted"`:
		*s = Aborted
	case `"Processing"`:
		*s = Processing
	case `"Dropped"`:
		*s = Dropped
	case "null":
	default:
		return errUnknownStatus
	}
	return nil
}

// Verify that this is a valid status.
func (s Status) Verify() error {
	switch s {
	case Unknown, Committed, Aborted, Processing, Dropped:
		return nil
	default:
		return errUnknownStatus
	}
}

func (s Status) String() string {
	switch s {
	case Unknown:
		return "Unknown"
	case Committed:
		return "Committed"
	case Aborted:
		return "Aborted"
	case Processing:
		return "Processing"
	case Dropped:
		return "Dropped"
	default:
		return "Invalid status"
	}
}
