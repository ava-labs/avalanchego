// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const (
	// diffKey = [subnetID] + [inverseHeight] + [nodeID]
	startDiffKeyLength  = ids.IDLen + database.Uint64Size
	diffKeyLength       = startDiffKeyLength + ids.NodeIDLen
	diffKeyHeightOffset = ids.IDLen
	diffKeyNodeIDOffset = diffKeyHeightOffset + database.Uint64Size

	// weightValue = [isNegative] + [weight]
	weightValueLength = 1 + database.Uint64Size
	weightValueTrue   = 0x01
)

var (
	errUnexpectedDiffKeyLength     = fmt.Errorf("expected diff key length %d", diffKeyLength)
	errUnexpectedWeightValueLength = fmt.Errorf("expected weight value length %d", weightValueLength)
)

// getStartDiffKey is used to determine the starting key when iterating.
//
// Note: the result should be a prefix of [getDiffKey] if called with the same
// arguments.
func getStartDiffKey(subnetID ids.ID, height uint64) []byte {
	key := make([]byte, startDiffKeyLength)
	copy(key, subnetID[:])
	packIterableHeight(key[diffKeyHeightOffset:], height)
	return key
}

func getDiffKey(subnetID ids.ID, height uint64, nodeID ids.NodeID) []byte {
	key := make([]byte, diffKeyLength)
	copy(key, subnetID[:])
	packIterableHeight(key[diffKeyHeightOffset:], height)
	copy(key[diffKeyNodeIDOffset:], nodeID[:])
	return key
}

func parseDiffKey(key []byte) (ids.ID, uint64, ids.NodeID, error) {
	if len(key) != diffKeyLength {
		return ids.Empty, 0, ids.EmptyNodeID, errUnexpectedDiffKeyLength
	}
	var (
		subnetID ids.ID
		nodeID   ids.NodeID
	)
	copy(subnetID[:], key)
	height := unpackIterableHeight(key[diffKeyHeightOffset:])
	copy(nodeID[:], key[diffKeyNodeIDOffset:])
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
