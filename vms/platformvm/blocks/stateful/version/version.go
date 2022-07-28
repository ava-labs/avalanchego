// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

// These are the codec versions we use.
const (
	// This codec version marshals/unmarshals Apricot blocks.
	ApricotBlockVersion uint16 = 0
	// This codec version marshals/unmarshals Blueberry blocks
	// and Apricot Blocks.
	BlueberryBlockVersion uint16 = 1

	// This codec version marshals/unmarshals
	// anything that isn't a block (UTXOS, output owners, etc.)
	StateVersion = ApricotBlockVersion
)
