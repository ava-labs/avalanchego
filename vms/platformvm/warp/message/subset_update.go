// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// SubsetUpdate is sent by the P-chain.
//
// This message reports the validator set of a given blockchain ID
// at a specific P-Chain height, sharded into consecutive subsets.
// Each shard hash is the SHA256 of the codec-serialized validators
// in that shard. This allows large validator sets to be delivered
// across multiple transactions on the receiving chain.
type SubsetUpdate struct {
	payload

	BlockchainID    ids.ID   `serialize:"true" json:"blockchainID"`
	PChainHeight    uint64   `serialize:"true" json:"pChainHeight"`
	PChainTimestamp uint64   `serialize:"true" json:"pChainTimestamp"`
	ShardHashes     []ids.ID `serialize:"true" json:"shardHashes"`
}

// NewSubsetUpdate creates a new initialized SubsetUpdate.
func NewSubsetUpdate(
	blockchainID ids.ID,
	pChainHeight uint64,
	pChainTimestamp uint64,
	shardHashes []ids.ID,
) (*SubsetUpdate, error) {
	msg := &SubsetUpdate{
		BlockchainID:    blockchainID,
		PChainHeight:    pChainHeight,
		PChainTimestamp: pChainTimestamp,
		ShardHashes:     shardHashes,
	}
	return msg, Initialize(msg)
}

// ParseSubsetUpdate parses bytes into an initialized SubsetUpdate.
func ParseSubsetUpdate(b []byte) (*SubsetUpdate, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SubsetUpdate)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
