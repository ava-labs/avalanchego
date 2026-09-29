// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

// maxUnixTime is the maximum parsable time for a block. This is unnecessarily
// strict, but ensures we don't overflow [time.Time] internals.
const maxUnixTime = 32503680000 // 01/01/3000 @ 12:00am (UTC)

var (
	errBlockTooFarInFuture    = errors.New("block too far in the future")
	errBlockHeightNotUint64   = errors.New("block height not uint64")
	errTxHashMismatch         = errors.New("transaction hash mismatch")
	errUncleHashMismatch      = errors.New("uncle hash mismatch")
	errWithdrawalHashMismatch = errors.New("withdrawals hash mismatch")
)

// ParseEth parses the buffer as the [rlp] encoding of a [types.Block], enforces
// the universal invariants that every block MUST satisfy.
func ParseEth(buf []byte, hooks hook.Points) (*types.Block, error) {
	b, err := parseEthBlock(buf)
	if err != nil {
		return nil, err
	}
	if err := hooks.VerifyBlockSyntax(b); err != nil {
		return nil, err
	}
	return b, nil
}

// parseEthBlock parses the buffer as [rlp] encoding of a [types.Block].
// It also checks some basic invariants that a [types.Block] MUST satisfy.
func parseEthBlock(buf []byte) (*types.Block, error) {
	b := new(types.Block)
	if err := rlp.DecodeBytes(buf, b); err != nil {
		return nil, fmt.Errorf("rlp.DecodeBytes(..., %T): %v", b, err)
	}

	if !b.Number().IsUint64() {
		return nil, errBlockHeightNotUint64
	}

	hdr := b.Header()
	if hdr.Time > maxUnixTime {
		return nil, fmt.Errorf("%w: %d seconds provided", errBlockTooFarInFuture, hdr.Time)
	}

	// Block body must match what is declared by the header.
	hasher := trie.NewStackTrie(nil)
	if types.DeriveSha(b.Transactions(), hasher) != hdr.TxHash {
		return nil, errTxHashMismatch
	}
	if types.CalcUncleHash(b.Uncles()) != hdr.UncleHash {
		return nil, errUncleHashMismatch
	}
	{
		// The withdrawals hash was added in the Shanghai hard fork.
		var want *common.Hash
		switch w := b.Withdrawals(); {
		case w == nil:
			want = nil
		case len(w) == 0:
			want = &types.EmptyWithdrawalsHash
		default:
			h := types.DeriveSha(w, hasher)
			want = &h
		}
		if !compareHashPtrs(want, hdr.WithdrawalsHash) {
			return nil, errWithdrawalHashMismatch
		}
	}
	return b, nil
}

func compareHashPtrs(a, b *common.Hash) bool {
	switch an, bn := a == nil, b == nil; {
	case an && bn:
		return true
	case an || bn:
		return false
	default:
		return *a == *b
	}
}
