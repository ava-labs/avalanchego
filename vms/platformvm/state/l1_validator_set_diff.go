// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

// The L1 validator set diffs record the full membership of every L1, active and
// inactive validators alike, so that the set of an L1 can be reconstructed at
// any indexed height.
//
// A row is written for every L1 validator whose existence or weight changed at
// a height. It holds the validator's state at the previous height:
//
//	key   = subnetID + inverseHeight + validationID
//	value = empty                          if it did not exist
//	        nodeID + weight + publicKey    otherwise
//
// The public key is the uncompressed BLS key stored in [L1Validator].
//
// The NodeID and public key of an L1 validator never change, and changes to its
// balance or active status do not produce a row.
const (
	// l1ValidatorSetDiffStartKey = subnetID + inverseHeight
	l1ValidatorSetDiffKeyHeightOffset = ids.IDLen
	l1ValidatorSetDiffStartKeyLength  = l1ValidatorSetDiffKeyHeightOffset + database.Uint64Size

	// l1ValidatorSetDiffKey = subnetID + inverseHeight + validationID
	l1ValidatorSetDiffKeyValidationIDOffset = l1ValidatorSetDiffStartKeyLength
	l1ValidatorSetDiffKeyLength             = l1ValidatorSetDiffKeyValidationIDOffset + ids.IDLen

	// l1ValidatorSetDiffValue = nodeID + weight + publicKey
	l1ValidatorSetDiffWeightOffset    = ids.NodeIDLen
	l1ValidatorSetDiffPublicKeyOffset = l1ValidatorSetDiffWeightOffset + database.Uint64Size
	// The uncompressed public key is twice the size of the compressed one.
	l1ValidatorSetDiffValueLength = l1ValidatorSetDiffPublicKeyOffset + 2*bls.PublicKeyLen
)

var (
	errUnexpectedL1ValidatorSetDiffKeyLength   = fmt.Errorf("expected L1 validator set diff key length %d", l1ValidatorSetDiffKeyLength)
	errUnexpectedL1ValidatorSetDiffValueLength = fmt.Errorf("expected L1 validator set diff value length 0 or %d", l1ValidatorSetDiffValueLength)
)

// l1ValidatorSetEntry is the state of an L1 validator that matters for set
// membership.
type l1ValidatorSetEntry struct {
	NodeID    ids.NodeID
	Weight    uint64
	PublicKey []byte // uncompressed
}

func newL1ValidatorSetEntry(vdr L1Validator) l1ValidatorSetEntry {
	return l1ValidatorSetEntry{
		NodeID:    vdr.NodeID,
		Weight:    vdr.Weight,
		PublicKey: vdr.PublicKey,
	}
}

// marshalL1ValidatorSetDiffStartKey returns the key to start iterating the
// diffs of subnetID from height towards genesis. It is a prefix of the keys
// returned by marshalL1ValidatorSetDiffKey with the same subnetID and height.
func marshalL1ValidatorSetDiffStartKey(subnetID ids.ID, height uint64) []byte {
	key := make([]byte, l1ValidatorSetDiffStartKeyLength, l1ValidatorSetDiffKeyLength)
	copy(key, subnetID[:])
	// The height is inverted so that iterating goes from the highest height to
	// the lowest.
	binary.BigEndian.PutUint64(key[l1ValidatorSetDiffKeyHeightOffset:], ^height)
	return key
}

func marshalL1ValidatorSetDiffKey(subnetID ids.ID, height uint64, validationID ids.ID) []byte {
	key := marshalL1ValidatorSetDiffStartKey(subnetID, height)
	return append(key, validationID[:]...)
}

func unmarshalL1ValidatorSetDiffKey(key []byte) (uint64, ids.ID, error) {
	if len(key) != l1ValidatorSetDiffKeyLength {
		return 0, ids.Empty, fmt.Errorf("%w, got %d", errUnexpectedL1ValidatorSetDiffKeyLength, len(key))
	}
	height := ^binary.BigEndian.Uint64(key[l1ValidatorSetDiffKeyHeightOffset:])
	var validationID ids.ID
	copy(validationID[:], key[l1ValidatorSetDiffKeyValidationIDOffset:])
	return height, validationID, nil
}

// marshalL1ValidatorSetDiffValue returns the empty value if entry is nil, which
// marks a validator that did not exist.
//
// Invariant: entry.PublicKey is an uncompressed BLS public key, as stored in
// [L1Validator]. The length isn't checked here, so a key of any other length
// would be written as is and then rejected by unmarshalL1ValidatorSetDiffValue
// on every query that reads the row.
func marshalL1ValidatorSetDiffValue(entry *l1ValidatorSetEntry) []byte {
	if entry == nil {
		return []byte{}
	}

	value := make([]byte, l1ValidatorSetDiffPublicKeyOffset+len(entry.PublicKey))
	copy(value, entry.NodeID[:])
	binary.BigEndian.PutUint64(value[l1ValidatorSetDiffWeightOffset:], entry.Weight)
	copy(value[l1ValidatorSetDiffPublicKeyOffset:], entry.PublicKey)
	return value
}

// unmarshalL1ValidatorSetDiffValue returns nil if the value marks a validator
// that did not exist.
func unmarshalL1ValidatorSetDiffValue(value []byte) (*l1ValidatorSetEntry, error) {
	if len(value) == 0 {
		return nil, nil
	}
	if len(value) != l1ValidatorSetDiffValueLength {
		return nil, fmt.Errorf("%w, got %d", errUnexpectedL1ValidatorSetDiffValueLength, len(value))
	}

	entry := &l1ValidatorSetEntry{
		Weight:    binary.BigEndian.Uint64(value[l1ValidatorSetDiffWeightOffset:]),
		PublicKey: bytes.Clone(value[l1ValidatorSetDiffPublicKeyOffset:]),
	}
	copy(entry.NodeID[:], value)
	return entry, nil
}
