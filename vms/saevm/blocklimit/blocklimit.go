// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package blocklimit applies the mempool-eligibility rule, which bounds each
// transaction's serialized size by its gas. Block construction must still
// reserve space for framing and non-EVM payloads.
package blocklimit

import (
	"math/bits"

	"github.com/ava-labs/avalanchego/utils/constants"
)

// MaxBlockBytes is the byte budget M of [Eligible]: the maximum size of a single
// P2P message.
const MaxBlockBytes uint64 = constants.DefaultMaxMessageSize

// Eligible reports whether a transaction may be included in a block, using the
// notation:
//
//   - M = [MaxBlockBytes], the maximum P2P message size
//   - x = blockGasLimit, the block's gas limit
//   - g = gasLimit, the transaction's gas limit
//   - y = size, the transaction's serialized size in bytes
//
// The transaction's byte share is y/M and its gas share is g/x. The rule rejects
// the transaction if its byte share exceeds its gas share:
//
//	reject if  y/M > g/x  <->   y > g·(M/x)
//
// Equivalently, it must carry at least x/M gas per serialized byte.
func Eligible(size, gasLimit, blockGasLimit uint64) bool {
	if blockGasLimit == 0 {
		return false
	}

	byteHi, byteLo := bits.Mul64(size, blockGasLimit)   // y·x
	gasHi, gasLo := bits.Mul64(gasLimit, MaxBlockBytes) // g·M
	if byteHi != gasHi {
		return byteHi < gasHi
	}
	return byteLo <= gasLo
}
