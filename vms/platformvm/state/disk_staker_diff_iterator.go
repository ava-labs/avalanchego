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

	weightValueLength = 1 + database.Uint64Size
	weightValueTrue   = 0x01

	startBLSKeyLength  = database.Uint64Size
	blsKeyLength       = startBLSKeyLength + hashing.AddrLen
	blsKeyNodeIDOffset = database.Uint64Size
)

var (
	errUnexpectedWeightKeyLength   = fmt.Errorf("expected weight key length %d", weightKeyLength)
	errUnexpectedWeightValueLength = fmt.Errorf("expected weight value length %d", weightValueLength)
	errUnexpectedBLSKeyLength      = fmt.Errorf("expected bls key length %d", blsKeyLength)
)

// getStartWeightKey is used to determine the starting key when iterating.
//
// Note: the result should be a prefix of [getWeightKey] if called with the same
// arguments.
func getStartWeightKey(subnetID ids.ID, height uint64) []byte {
	key := make([]byte, startWeightKeyLength)
	copy(key, subnetID[:])
	packIterableHeight(key[weightKeyHeightOffset:], height)
	return key
}

func getWeightKey(subnetID ids.ID, height uint64, nodeID ids.NodeID) []byte {
	key := make([]byte, weightKeyLength)
	copy(key, subnetID[:])
	packIterableHeight(key[weightKeyHeightOffset:], height)
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
	height := unpackIterableHeight(key[weightKeyHeightOffset:])
	copy(nodeID[:], key[weightKeyNodeIDOffset:])
	return subnetID, height, nodeID, nil
}

func getWeightValue(diff *ValidatorWeightDiff) []byte {
	value := make([]byte, weightValueLength)
	if diff.Decrease {
		value[0] = weightValueTrue
	}
	binary.BigEndian.PutUint64(value[1:], diff.Amount)
	return value
}

func parseWeightValue(value []byte) (*ValidatorWeightDiff, error) {
	if len(value) != weightValueLength {
		return nil, errUnexpectedWeightValueLength
	}
	return &ValidatorWeightDiff{
		Decrease: value[0] == weightValueTrue,
		Amount:   binary.BigEndian.Uint64(value[1:]),
	}, nil
}

// getStartBLSKey is used to determine the starting key when iterating.
//
// Note: the result should be a prefix of [getBLSKey] if called with the same
// arguments.
func getStartBLSKey(height uint64) []byte {
	key := make([]byte, startBLSKeyLength)
	packIterableHeight(key, height)
	return key
}

func getBLSKey(height uint64, nodeID ids.NodeID) []byte {
	key := make([]byte, blsKeyLength)
	packIterableHeight(key, height)
	copy(key[blsKeyNodeIDOffset:], nodeID[:])
	return key
}

func parseBLSKey(key []byte) (uint64, ids.NodeID, error) {
	if len(key) != blsKeyLength {
		return 0, ids.EmptyNodeID, errUnexpectedBLSKeyLength
	}
	var nodeID ids.NodeID
	height := unpackIterableHeight(key)
	copy(nodeID[:], key[blsKeyNodeIDOffset:])
	return height, nodeID, nil
}

// Note: [height] is encoded as a bit flipped big endian number so that
// iterating lexicographically results in iterating in decreasing heights.
//
// Invariant: [key] has sufficient length
func packIterableHeight(key []byte, height uint64) {
	binary.BigEndian.PutUint64(key, ^height)
}

// Because we bit flip the height when constructing the key, we must remember to
// bip flip again here.
//
// Invariant: [key] has sufficient length
func unpackIterableHeight(key []byte) uint64 {
	return ^binary.BigEndian.Uint64(key)
}
