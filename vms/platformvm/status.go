// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
)

var errUnknownStatus = errors.New("unknown status")

// Status ...
type Status uint32

// List of possible status values
// [Unknown] Zero value, means the status is not known
// [Preferred] means the operation is known and preferred, but hasn't been decided yet
// [Created] means the operation occurred, but isn't managed locally
// [Validating] means the operation was accepted and is managed locally
const (
	Unknown Status = iota
	Preferred
	Created
	Validating
	Committed
	Aborted
	Processing
	Syncing
	Dropped
)

// MarshalJSON ...
func (s Status) MarshalJSON() ([]byte, error) {
	if err := s.Valid(); err != nil {
		return nil, err
	}
	return []byte("\"" + s.String() + "\""), nil
}

// UnmarshalJSON ...
func (s *Status) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == "null" {
		return nil
	}
	switch str {
	case "\"Unknown\"":
		*s = Unknown
	case "\"Preferred\"":
		*s = Preferred
	case "\"Created\"":
		*s = Created
	case "\"Validating\"":
		*s = Validating
	case "\"Committed\"":
		*s = Committed
	case "\"Aborted\"":
		*s = Aborted
	case "\"Processing\"":
		*s = Processing
	case "\"Syncing\"":
		*s = Syncing
	case "\"Dropped\"":
		*s = Dropped
	default:
		return errUnknownStatus
	}
	return nil
}

// Valid returns nil if the status is a valid status.
func (s Status) Valid() error {
	switch s {
	case Unknown, Preferred, Created, Validating, Committed, Aborted, Processing, Syncing, Dropped:
		return nil
	default:
		return errUnknownStatus
	}
}

func (s Status) String() string {
	switch s {
	case Unknown:
		return "Unknown"
	case Preferred:
		return "Preferred"
	case Created:
		return "Created"
	case Validating:
		return "Validating"
	case Committed:
		return "Committed"
	case Aborted:
		return "Aborted"
	case Processing:
		return "Processing"
	case Syncing:
		return "Syncing"
	case Dropped:
		return "Dropped"
	default:
		return "Invalid status"
	}
}
