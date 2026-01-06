// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package status

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

// List of possible blockchain status values:
// - [UnknownChain] This node is not aware of the existence of this blockchain
// - [Created] This node is not currently validating this blockchain
// - [Preferred] This blockchain is currently in the preferred tip
// - [Validating] This node is currently validating this blockchain
// - [Syncing] This node is syncing up to the preferred block height
const (
	UnknownChain BlockchainStatus = iota
	Created
	Preferred
	Validating
	Syncing
)

var (
	errUnknownBlockchainStatus = errors.New("unknown blockchain status")

	_ json.Marshaler    = BlockchainStatus(0)
	_ verify.Verifiable = BlockchainStatus(0)
	_ fmt.Stringer      = BlockchainStatus(0)
)

type BlockchainStatus uint32

func (s BlockchainStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), s.Verify()
}

func (s *BlockchainStatus) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `"Unknown"`:
		*s = UnknownChain
	case `"Created"`:
		*s = Created
	case `"Preferred"`:
		*s = Preferred
	case `"Validating"`:
		*s = Validating
	case `"Syncing"`:
		*s = Syncing
	case "null":
	default:
		return errUnknownBlockchainStatus
	}
	return nil
}

// Verify that this is a valid status.
func (s BlockchainStatus) Verify() error {
	switch s {
	case UnknownChain, Created, Preferred, Validating, Syncing:
		return nil
	default:
		return errUnknownBlockchainStatus
	}
}

func (s BlockchainStatus) String() string {
	switch s {
	case UnknownChain:
		return "Unknown"
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
