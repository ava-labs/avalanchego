// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/version"
)

// Ledger interface for the ledger wrapper
type Ledger interface {
	Version() (v *version.Semantic, err error)
	PubKey(addressIndex uint32) (*secp256k1.PublicKey, error)
	PubKeys(addressIndices []uint32) ([]*secp256k1.PublicKey, error)
	SignHash(hash []byte, addressIndices []uint32) ([][]byte, error)
	Sign(unsignedTxBytes []byte, addressIndices []uint32) ([][]byte, error)
	Disconnect() error
}
