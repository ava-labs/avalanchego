// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var errFailedPublicKeyDeserialize = errors.New("couldn't deserialize public key")

// Validator is a struct that contains the base values representing a validator
// of the Avalanche Network.
type Validator struct {
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	TxID      ids.ID
	Weight    uint64

	// index is used to efficiently remove validators from the validator set. It
	// represents the index of this validator in the vdrSlice and weights
	// arrays.
	index int
}

// GetValidatorOutput is a struct that contains the publicly relevant values of
// a validator of the Avalanche Network for the output of GetValidator.
type GetValidatorOutput struct {
	NodeID    ids.NodeID
	PublicKey *PublicKey
	Weight    uint64
}

func NewPublicKey(key *bls.PublicKey) *PublicKey {
	return &PublicKey{
		key:   key,
		bytes: nil,
	}
}

// NewPublicKeyFromBytes assumes that the caller is responsible for providing
// a valid serialized key.
func NewPublicKeyFromBytes(bytes []byte) *PublicKey {
	return &PublicKey{
		key:   nil,
		bytes: bytes,
	}
}

// PublicKey lazily serializes/deserializes the validator's key.
// At least one of (key, bytes) must be provided.
// This data structure is NOT thread-safe.
type PublicKey struct {
	key   *bls.PublicKey
	bytes []byte
}

func (k *PublicKey) Key() (*bls.PublicKey, error) {
	if k.key == nil {
		// We blindly assume that the key is valid if it passes deserialization.
		pk := new(bls.PublicKey).Deserialize(k.bytes)
		if pk == nil {
			return nil, errFailedPublicKeyDeserialize
		}

		k.key = pk
	}

	return k.key, nil
}

func (k *PublicKey) Bytes() []byte {
	if len(k.bytes) == 0 {
		k.bytes = k.key.Serialize()
	}

	return k.bytes
}
