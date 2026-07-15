// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package blocklimit applies the mempool-eligibility rule, which bounds each
// transaction's serialized size by its gas. Block construction must still
// reserve space for framing and non-EVM payloads.
package blocklimit

import (
	"math/bits"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/utils/constants"
)

// SafeMaxBytes is the byte budget to pass as the maxBytes argument of
// [Eligible]: the maximum size of a single P2P message.
const SafeMaxBytes uint64 = constants.DefaultMaxMessageSize

// Eligible reports whether tx MAY be included in a block, using the notation:
//
//   - M = maxBytes, the maximum P2P message size (typically [SafeMaxBytes])
//   - x = blockGasLimit, the block's gas limit
//   - g = tx.Gas(), the transaction's gas limit
//   - y = tx.Size(), the transaction's serialized size in bytes
//
// The transaction's byte share is y/M and its gas share is g/x. The rule rejects
// the transaction if its byte share exceeds its gas share:
//
//	accept if  y/M <= g/x  <->   y·x <= g·M
//
// Equivalently, it must carry at least x/M gas per serialized byte.
func Eligible(tx *types.Transaction, blockGasLimit, maxBytes uint64) bool {
	// Defensive check: if blockGasLimit == 0, all transactions would be
	// incorrectly eligible
	if blockGasLimit == 0 {
		return false
	}

	yxHi, yxLo := bits.Mul64(tx.Size(), blockGasLimit) // y·x
	gmHi, gmLo := bits.Mul64(tx.Gas(), maxBytes)       // g·M
	if yxHi != gmHi {
		return yxHi < gmHi
	}
	return yxLo <= gmLo
}
