// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
)

var errUnknownBlockchainStatus = errors.New("unknown blockchain status")

// Status ...
type BlockchainStatus uint32

// List of possible status values
// [Created] This node is not currently validating this blockchain
// [Preferred] This node is currently at the preferred tip
// [Validating] This node is currently validating this blockchain
// [Syncing] This node is syncing up to the preferred block height
const (
	Created BlockchainStatus = iota
	Preferred
	Validating
	Syncing
)

// MarshalJSON ...
func (s BlockchainStatus) MarshalJSON() ([]byte, error) {
	if err := s.Valid(); err != nil {
		return nil, err
	}
	return []byte("\"" + s.String() + "\""), nil
}

// UnmarshalJSON ...
func (s *BlockchainStatus) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == "null" {
		return nil
	}
	switch str {
	case "\"Created\"":
		*s = Created
	case "\"Preferred\"":
		*s = Preferred
	case "\"Validating\"":
		*s = Validating
	case "\"Syncing\"":
		*s = Syncing
	default:
		return errUnknownStatus
	}
	return nil
}

// Valid returns nil if the status is a valid status.
func (s BlockchainStatus) Valid() error {
	switch s {
	case Created, Preferred, Validating, Syncing:
		return nil
	default:
		return errUnknownBlockchainStatus
	}
}

func (s BlockchainStatus) String() string {
	switch s {
	case Created:
		return "Created"
	case Preferred:
		return "Preferred"
	case Validating:
		return "Validating"
	case Syncing:
		return "Syncing"
	default:
		return "Invalid blockchain status"
	}
}
