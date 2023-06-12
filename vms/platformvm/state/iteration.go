// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	startWeightKeyLength  = hashing.HashLen + database.Uint64Size
	weightKeyLength       = startWeightKeyLength + hashing.AddrLen
	weightKeyHeightOffset = hashing.HashLen
	weightKeyNodeIDOffset = weightKeyHeightOffset + database.Uint64Size

	startBLSKeyLength  = database.Uint64Size
	blsKeyLength       = startBLSKeyLength + hashing.AddrLen
	blsKeyNodeIDOffset = database.Uint64Size
)

var (
	errUnexpectedWeightKeyLength = fmt.Errorf("expected weight key length %d", weightKeyLength)
	errUnexpectedBLSKeyLength    = fmt.Errorf("expected bls key length %d", blsKeyLength)
)

func getStartWeightKey(subnetID ids.ID, height uint64) []byte {
	key := make([]byte, startWeightKeyLength)
	copy(key, subnetID[:])
	binary.BigEndian.PutUint64(key[weightKeyHeightOffset:], ^height)
	return key
}

func getWeightKey(subnetID ids.ID, height uint64, nodeID ids.NodeID) []byte {
	key := make([]byte, weightKeyLength)
	copy(key, subnetID[:])
	binary.BigEndian.PutUint64(key[weightKeyHeightOffset:], ^height)
	copy(key[weightKeyNodeIDOffset:], nodeID[:])
	return key
}

func parseWeightKey(key []byte) (ids.ID, uint64, ids.NodeID, error) {
	if len(key) != weightKeyLength {
		return ids.Empty, 0, ids.EmptyNodeID, errUnexpectedWeightKeyLength
	}
	var (
		subnetID ids.ID
		nodeID   ids.NodeID
	)
	copy(subnetID[:], key)
	height := ^binary.BigEndian.Uint64(key[weightKeyHeightOffset:])
	copy(nodeID[:], key[weightKeyNodeIDOffset:])
	return subnetID, height, nodeID, nil
}

func getStartBLSKey(height uint64) []byte {
	key := make([]byte, startBLSKeyLength)
	binary.BigEndian.PutUint64(key, ^height)
	return key
}

func getBLSKey(height uint64, nodeID ids.NodeID) []byte {
	key := make([]byte, blsKeyLength)
	binary.BigEndian.PutUint64(key, ^height)
	copy(key[blsKeyNodeIDOffset:], nodeID[:])
	return key
}

func parseBLSKey(key []byte) (uint64, ids.NodeID, error) {
	if len(key) != blsKeyLength {
		return 0, ids.EmptyNodeID, errUnexpectedBLSKeyLength
	}
	var nodeID ids.NodeID
	height := ^binary.BigEndian.Uint64(key)
	copy(nodeID[:], key[blsKeyNodeIDOffset:])
	return height, nodeID, nil
}
