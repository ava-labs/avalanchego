// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package cchaintest provides shared helpers for constructing C-Chain blocks in
// tests.
package cchaintest

import (
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// BlockOption configures the block produced by [NewTestBlock].
type BlockOption = options.Option[blockProperties]

type blockProperties struct {
	number        uint64
	parent        common.Hash
	ethTxs        []*types.Transaction
	crossChainTxs []*tx.Tx
	extDataHash   *common.Hash
}

// WithNumber sets the block's header number.
func WithNumber(n uint64) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.number = n
	})
}

// WithParent sets the block's parent hash.
func WithParent(h common.Hash) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.parent = h
	})
}

// WithEthTxs sets the block's EVM transactions.
func WithEthTxs(txs ...*types.Transaction) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.ethTxs = slices.Clone(txs)
	})
}

// WithCrossChainTxs sets the cross-chain transactions encoded in the block's
// ExtData.
func WithCrossChainTxs(txs ...*tx.Tx) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.crossChainTxs = slices.Clone(txs)
	})
}

// WithMismatchedExtDataHash commits a random ExtDataHash that does not match the
// block's ExtData and disables recomputation, simulating a tampered block.
func WithMismatchedExtDataHash() BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		h := common.Hash(ids.GenerateTestID())
		p.extDataHash = &h
	})
}

// NewTestBlock builds a [*types.Block] from the provided options. By default the
// block has number 1, a zero parent hash, no Ethereum or cross-chain transactions,
// and an ExtDataHash computed from its (empty) ExtData.
func NewTestBlock(tb testing.TB, opts ...BlockOption) *types.Block {
	tb.Helper()

	props := options.ApplyTo(&blockProperties{number: 1}, opts...)

	extData, err := tx.MarshalSlice(props.crossChainTxs)
	require.NoErrorf(tb, err, "tx.MarshalSlice(%d txs)", len(props.crossChainTxs))

	header := &types.Header{
		ParentHash: props.parent,
		Number:     new(big.Int).SetUint64(props.number),
	}
	setExtDataHash := true
	if props.extDataHash != nil {
		header = customtypes.WithHeaderExtra(
			header,
			&customtypes.HeaderExtra{ExtDataHash: *props.extDataHash},
		)
		setExtDataHash = false
	}

	return customtypes.NewBlockWithExtData(
		header,
		props.ethTxs,
		nil, // uncles
		nil, // receipts
		saetest.TrieHasher(),
		extData,
		setExtDataHash,
	)
}

// NewBlock returns a block whose ExtData encodes txs and whose header is
// configured for ancestor traversal (parent hash + number).
func NewBlock(tb testing.TB, number uint64, parent common.Hash, txs ...*tx.Tx) *types.Block {
	tb.Helper()
	return NewTestBlock(tb, WithNumber(number), WithParent(parent), WithCrossChainTxs(txs...))
}

// NewTamperedBlock returns a block that encodes txs but whose header commits an
// ExtDataHash that does not match its ExtData, simulating tampering.
func NewTamperedBlock(tb testing.TB, number uint64, parent common.Hash, txs ...*tx.Tx) *types.Block {
	tb.Helper()
	return NewTestBlock(tb,
		WithNumber(number),
		WithParent(parent),
		WithCrossChainTxs(txs...),
		WithMismatchedExtDataHash(),
	)
}
