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
// [Committed] Reached finality
// [Aborted] Block proposal was aborted
// [Processing] Not found in the db but is in the preferred blocks db
// [Dropped] The transaction was dropped most likely because it was invalid
const (
	Unknown    Status = 0
	Committed  Status = 4
	Aborted    Status = 5
	Processing Status = 6
	Dropped    Status = 8
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
	case "\"Committed\"":
		*s = Committed
	case "\"Aborted\"":
		*s = Aborted
	case "\"Processing\"":
		*s = Processing
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
