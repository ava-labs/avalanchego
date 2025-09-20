// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ledger

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/ledger"
	"github.com/ava-labs/avalanchego/version"
)

// Ledger interface for the ledger wrapper
type Ledger interface {
	Version() (v *version.Semantic, err error)
	Address(displayHRP string, addressIndex uint32) (ids.ShortID, error)
	Addresses(addressIndices []uint32) ([]ids.ShortID, error)
	SignHash(hash []byte, addressIndices []uint32) ([][]byte, error)
	Sign(unsignedTxBytes []byte, addressIndices []uint32) ([][]byte, error)
	Disconnect() error
}

// Verify that the ledger implementation satisfies the interface
var _ Ledger = (*ledger.Ledger)(nil)
