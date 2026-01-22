// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// ValidatorChange represents a single validator addition, removal, or modification
type ValidatorChange struct {
	NodeID                     ids.NodeID `serialize:"true" json:"nodeID"`
	UncompressedPublicKeyBytes [96]byte   `serialize:"true" json:"publicKey"`
	PreviousWeight             uint64     `serialize:"true" json:"previousWeight"` // 0 for additions
	CurrentWeight              uint64     `serialize:"true" json:"currentWeight"`  // 0 for removals
}

// ValidatorSetDiff is sent by the P-chain.
//
// This message reports the difference in validator set of a given blockchain ID
// between two P-Chain heights. It allows external chains to efficiently update
// their validator sets by applying only the changes rather than receiving the
// complete validator set.
//
// The message includes cryptographic commitments (hashes) to both the previous
// and current validator sets, allowing recipients to verify state continuity
// and detect tampering.
type ValidatorSetDiff struct {
	payload

	BlockchainID ids.ID `serialize:"true" json:"blockchainID"`

	// Previous state (starting point for the diff)
	PreviousHeight           uint64 `serialize:"true" json:"previousHeight"`
	PreviousTimestamp        uint64 `serialize:"true" json:"previousTimestamp"`
	PreviousValidatorSetHash ids.ID `serialize:"true" json:"previousValidatorSetHash"`

	// Current state (ending point for the diff)
	CurrentHeight           uint64 `serialize:"true" json:"currentHeight"`
	CurrentTimestamp        uint64 `serialize:"true" json:"currentTimestamp"`
	CurrentValidatorSetHash ids.ID `serialize:"true" json:"currentValidatorSetHash"`

	// The actual changes
	Added    []ValidatorChange `serialize:"true" json:"added"`
	Removed  []ValidatorChange `serialize:"true" json:"removed"`
	Modified []ValidatorChange `serialize:"true" json:"modified"`
}

// NewValidatorSetDiff creates a new initialized ValidatorSetDiff.
func NewValidatorSetDiff(
	blockchainID ids.ID,
	previousHeight uint64,
	previousTimestamp uint64,
	previousValidatorSetHash ids.ID,
	currentHeight uint64,
	currentTimestamp uint64,
	currentValidatorSetHash ids.ID,
	added []ValidatorChange,
	removed []ValidatorChange,
	modified []ValidatorChange,
) (*ValidatorSetDiff, error) {
	msg := &ValidatorSetDiff{
		BlockchainID:             blockchainID,
		PreviousHeight:           previousHeight,
		PreviousTimestamp:        previousTimestamp,
		PreviousValidatorSetHash: previousValidatorSetHash,
		CurrentHeight:            currentHeight,
		CurrentTimestamp:         currentTimestamp,
		CurrentValidatorSetHash:  currentValidatorSetHash,
		Added:                    added,
		Removed:                  removed,
		Modified:                 modified,
	}
	return msg, Initialize(msg)
}

// ParseValidatorSetDiff parses bytes into an initialized ValidatorSetDiff.
func ParseValidatorSetDiff(b []byte) (*ValidatorSetDiff, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*ValidatorSetDiff)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
