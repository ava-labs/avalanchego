// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import "github.com/ava-labs/avalanchego/utils/units"

const (
	AvalancheAtomicTxFee = units.MilliAvax

	// The base cost to charge per atomic transaction. Added in Apricot Phase 5.
	AtomicTxBaseCost uint64 = 10_000
)
