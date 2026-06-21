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
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// BlockOption configures the block produced by [NewTestBlock].
type BlockOption = options.Option[blockProperties]

type blockProperties struct {
	number        uint64
	timestamp     uint64
	parent        common.Hash
	ethTxs        []*types.Transaction
	crossChainTxs []*tx.Tx
	extData       *[]byte
	extDataHash   *common.Hash
	version       uint32
}

// WithNumber sets the block's header number.
func WithNumber(n uint64) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.number = n
	})
}

// WithTimestamp sets the block's header timestamp (in seconds), which selects
// the network upgrade whose rules govern the block — notably whether
// [github.com/ava-labs/avalanchego/vms/saevm/cchain.VM.ParseBlock] enforces the
// post-ApricotPhase1 ExtDataHash commitment.
func WithTimestamp(t uint64) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.timestamp = t
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

// WithExtData sets the raw ExtData bytes for the block.
func WithExtData(data []byte) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.extData = &data
	})
}

// WithExtDataHash uses h during block building and disables recomputation.
func WithExtDataHash(h common.Hash) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.extDataHash = &h
	})
}

// WithBlockVersion sets the block's BlockBodyExtra Version. The default of 0 is
// the only version accepted by the C-Chain ParseBlock; a non-zero value
// simulates a block declaring an unsupported version.
func WithBlockVersion(v uint32) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.version = v
	})
}

// NewTestBlock builds a [*types.Block] from the provided options. By default the
// block has number 1, a zero parent hash and timestamp, no Ethereum or
// cross-chain transactions, and an ExtDataHash computed from its (empty) ExtData.
func NewTestBlock(tb testing.TB, opts ...BlockOption) *types.Block {
	tb.Helper()

	props := options.ApplyTo(&blockProperties{number: 1}, opts...)

	extData, err := tx.MarshalSlice(props.crossChainTxs)
	require.NoErrorf(tb, err, "tx.MarshalSlice(%d txs)", len(props.crossChainTxs))
	if props.extData != nil {
		extData = *props.extData
	}

	// By default the header commits the ExtDataHash computed from the block's
	// own ExtData; a caller-supplied hash overrides this to simulate tampering.
	extDataHash := customtypes.CalcExtDataHash(extData)
	if props.extDataHash != nil {
		extDataHash = *props.extDataHash
	}
	header := customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash: props.parent,
			Number:     new(big.Int).SetUint64(props.number),
			Time:       props.timestamp,
		},
		&customtypes.HeaderExtra{ExtDataHash: extDataHash},
	)

	block := types.NewBlock(header, props.ethTxs, nil /* uncles */, nil /* receipts */, saetest.TrieHasher())
	customtypes.SetBlockExtra(block, &customtypes.BlockBodyExtra{
		Version: props.version,
		ExtData: &extData,
	})
	return block
}

// NewBlock returns a block whose ExtData encodes txs and whose header is
// configured for ancestor traversal (parent hash + number).
func NewBlock(tb testing.TB, number uint64, parent common.Hash, txs ...*tx.Tx) *types.Block {
	tb.Helper()
	return NewTestBlock(tb, WithNumber(number), WithParent(parent), WithCrossChainTxs(txs...))
}
