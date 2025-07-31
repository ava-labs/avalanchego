// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

const (
	codecVersion = 0
)

// Addressable is the interface a feature extension must provide to be able to
// be tracked as a part of the utxo set for a set of addresses
type Addressable interface {
	Addresses() [][]byte
}
