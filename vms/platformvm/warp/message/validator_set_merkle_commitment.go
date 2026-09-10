// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// ValidatorSetMerkleCommitment is sent by the P-chain.
//
// This message contains a Merkle root commitment over the validator set of a
// given blockchain ID at a specific P-Chain height. The root is computed over
// leaves sha256([16 zeros][48 bytes X][16 zeros][48 bytes Y][8 bytes weight]),
// using the same padded BLS public key format as Solidity's BLST precompile.
//
// Wire layout (linear codec type ID 6):
//
//	[2]  codec version  (0x0000)
//	[4]  type ID        (6)
//	[32] AvalancheBlockchainID
//	[32] RootHash       — Merkle root of the validator set
//	[8]  TotalWeight    — sum of validator weights
//	[8]  PChainHeight
//	[8]  PChainTimestamp
type ValidatorSetMerkleCommitment struct {
	payload

	AvalancheBlockchainID ids.ID   `serialize:"true" json:"blockchainID"`
	RootHash              [32]byte `serialize:"true" json:"rootHash"`
	TotalWeight           uint64   `serialize:"true" json:"totalWeight"`
	PChainHeight          uint64   `serialize:"true" json:"pChainHeight"`
	PChainTimestamp       uint64   `serialize:"true" json:"pChainTimestamp"`
}

// NewValidatorSetMerkleCommitment creates a new initialized ValidatorSetMerkleCommitment.
func NewValidatorSetMerkleCommitment(
	avalancheBlockchainID ids.ID,
	rootHash [32]byte,
	totalWeight uint64,
	pChainHeight uint64,
	pChainTimestamp uint64,
) (*ValidatorSetMerkleCommitment, error) {
	msg := &ValidatorSetMerkleCommitment{
		AvalancheBlockchainID: avalancheBlockchainID,
		RootHash:              rootHash,
		TotalWeight:           totalWeight,
		PChainHeight:          pChainHeight,
		PChainTimestamp:       pChainTimestamp,
	}
	return msg, Initialize(msg)
}

// ParseValidatorSetMerkleCommitment parses bytes into an initialized ValidatorSetMerkleCommitment.
func ParseValidatorSetMerkleCommitment(b []byte) (*ValidatorSetMerkleCommitment, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*ValidatorSetMerkleCommitment)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}