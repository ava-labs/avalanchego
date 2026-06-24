// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package blocklimit applies the mempool-eligibility rule, which bounds each
// transaction's serialized size by its gas. Block construction must still
// reserve space for framing and non-EVM payloads.
package blocklimit

import (
	"math"
	"math/bits"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// MaxBlockBytes is the byte budget M of [Eligible]: the maximum size of a single
// P2P message.
const MaxBlockBytes uint64 = constants.DefaultMaxMessageSize

// BlockByteOverhead is the fixed per-block framing reserved below the maximum
// message size M: the ProposerVM certificate, the block header, signature,
// and message framing.
const BlockByteOverhead uint64 = staking.MaxCertificateLen + 6*units.KiB

// MaxBodyBytes caps a block's serialized body bytes, reserving
// [BlockByteOverhead] below the maximum message size M.
const MaxBodyBytes = MaxBlockBytes - BlockByteOverhead

// OpSlicePrefix is the framing prepended once to a block's ExtData op slice: a
// [codec.VersionSize]-byte codec version and a [wrappers.IntLen]-byte element
// count. Ops are then concatenated with no per-op framing.
const OpSlicePrefix uint64 = codec.VersionSize + wrappers.IntLen

// MinGasForBytes returns the minimum gas a transaction of the given serialized
// size must consume so that exhausting the block's gas limit cannot exhaust more
// than the byte budget [MaxBodyBytes].
func MinGasForBytes(size, blockGasLimit uint64) uint64 {
	if size >= MaxBodyBytes {
		return math.MaxUint64
	}
	hi, lo := bits.Mul64(size, blockGasLimit)
	q, r := bits.Div64(hi, lo, MaxBodyBytes)
	if r != 0 {
		q++ // round up
	}
	return q
}

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
