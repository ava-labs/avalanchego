// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// ValidatorChange represents a single validator addition, removal, or modification.
// Weight is the current weight (0 for removals).
type ValidatorChange struct {
	UncompressedPublicKeyBytes [96]byte `serialize:"true" json:"publicKey"`
	Weight                     uint64   `serialize:"true" json:"weight"`
}

// ValidatorSetDiff is sent by the P-chain.
//
// This message reports the difference in validator set of a given blockchain ID
// between two P-Chain heights. It allows external chains to efficiently update
// their validator sets by applying only the changes rather than receiving the
// complete validator set.
//
// Changes is a sorted (by public key) flat list of additions, removals, and
// modifications. Removals have Weight == 0. NumAdded counts how many entries
// in Changes are newly added validators (as opposed to weight modifications).
//
// CurrentValidatorSetHash is the sha256 hash of the serialized resulting
// validator set after applying the diff. It is used by on-chain verifiers to
// confirm the diff was applied correctly.
//
// Wire format (linearcodec, type ID 5):
//
//	[2]  codec version  (0x0000)
//	[4]  type ID        (5)
//	[32] BlockchainID
//	[8]  PreviousHeight
//	[8]  PreviousTimestamp
//	[8]  CurrentHeight
//	[8]  CurrentTimestamp
//	[32] CurrentValidatorSetHash
//	[4]  len(Changes)   — numChanges
//	per change:
//	  [96] UncompressedPublicKeyBytes
//	  [8]  Weight
//	[4]  NumAdded
type ValidatorSetDiff struct {
	payload

	BlockchainID            ids.ID `serialize:"true" json:"blockchainID"`
	PreviousHeight          uint64 `serialize:"true" json:"previousHeight"`
	PreviousTimestamp       uint64 `serialize:"true" json:"previousTimestamp"`
	CurrentHeight           uint64 `serialize:"true" json:"currentHeight"`
	CurrentTimestamp         uint64 `serialize:"true" json:"currentTimestamp"`
	CurrentValidatorSetHash ids.ID `serialize:"true" json:"currentValidatorSetHash"`

	Changes  []ValidatorChange `serialize:"true" json:"changes"`
	NumAdded uint32            `serialize:"true" json:"numAdded"`
}

// NewValidatorSetDiff creates a new initialized ValidatorSetDiff.
func NewValidatorSetDiff(
	blockchainID ids.ID,
	previousHeight uint64,
	previousTimestamp uint64,
	currentHeight uint64,
	currentTimestamp uint64,
	currentValidatorSetHash ids.ID,
	changes []ValidatorChange,
	numAdded uint32,
) (*ValidatorSetDiff, error) {
	msg := &ValidatorSetDiff{
		BlockchainID:            blockchainID,
		PreviousHeight:          previousHeight,
		PreviousTimestamp:       previousTimestamp,
		CurrentHeight:           currentHeight,
		CurrentTimestamp:         currentTimestamp,
		CurrentValidatorSetHash: currentValidatorSetHash,
		Changes:                 changes,
		NumAdded:                numAdded,
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
