// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package blockstest provides test helpers for constructing [Streaming
// Asynchronous Execution] (SAE) blocks.
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package blockstest

import (
	"math"
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/hook/hookstest"
	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saetest"
)

// An EthBlockOption configures the default block properties created by
// [NewEthBlock].
type EthBlockOption = options.Option[ethBlockProperties]

// NewEthBlock constructs a raw Ethereum block with the given arguments.
func NewEthBlock(tb testing.TB, parent *types.Block, txs types.Transactions, opts ...EthBlockOption) *types.Block {
	tb.Helper()
	props := &ethBlockProperties{
		header: &types.Header{
			Number:        new(big.Int).Add(parent.Number(), big.NewInt(1)),
			ParentHash:    parent.Hash(),
			BaseFee:       big.NewInt(0),
			ExcessBlobGas: new(uint64),
		},
	}
	props = options.ApplyTo(props, opts...)
	block, err := hookstest.BuildBlock(props.header, txs, props.receipts, props.ops)
	require.NoError(tb, err, "hookstest.BuildBlock()")
	return block
}

type ethBlockProperties struct {
	header   *types.Header
	receipts types.Receipts
	ops      []hookstest.Op
}

// ModifyHeader returns an option to modify the [types.Header] constructed by
// [NewEthBlock]. It SHOULD NOT modify the `Number` and `ParentHash`, but MAY
// modify any other field.
func ModifyHeader(fn func(*types.Header)) EthBlockOption {
	return options.Func[ethBlockProperties](func(p *ethBlockProperties) {
		fn(p.header)
	})
}

// WithReceipts returns an option to set the receipts of a block constructed by
// [NewEthBlock].
func WithReceipts(rs types.Receipts) EthBlockOption {
	return options.Func[ethBlockProperties](func(p *ethBlockProperties) {
		p.receipts = slices.Clone(rs)
	})
}

// WithOps returns an option to set the ops of a block constructed by
// [NewEthBlock].
func WithOps(ops []hookstest.Op) EthBlockOption {
	return options.Func[ethBlockProperties](func(p *ethBlockProperties) {
		p.ops = slices.Clone(ops)
	})
}

// A BlockOption configures the default block properties created by [NewBlock].
type BlockOption = options.Option[blockProperties]

// NewBlock constructs an SAE block, wrapping the raw Ethereum block.
func NewBlock(tb testing.TB, eth *types.Block, parent, lastSettled *blocks.Block, opts ...BlockOption) *blocks.Block {
	tb.Helper()

	props := options.ApplyTo(&blockProperties{}, opts...)
	if props.logger == nil {
		props.logger = saetest.NewTBLogger(tb, logging.Warn)
	}

	b, err := blocks.New(eth, parent, lastSettled, props.logger)
	require.NoError(tb, err, "blocks.New()")
	return b
}

type blockProperties struct {
	logger logging.Logger
}

// WithLogger overrides the logger passed to [blocks.New] by [NewBlock].
func WithLogger(l logging.Logger) BlockOption {
	return options.Func[blockProperties](func(p *blockProperties) {
		p.logger = l
	})
}

// NewGenesis constructs a new [core.Genesis], writes it to the database, and
// returns wraps [core.Genesis.ToBlock] with [NewBlock]. It assumes a nil
// [triedb.Config] unless overridden by a [WithTrieDBConfig]. The block is
// marked as both executed and synchronous.
func NewGenesis(tb testing.TB, db ethdb.Database, xdb saedb.ExecutionResults, config *params.ChainConfig, alloc types.GenesisAlloc, opts ...GenesisOption) *blocks.Block {
	tb.Helper()
	conf := &genesisConfig{
		gasTarget: math.MaxUint64,
	}
	options.ApplyTo(conf, opts...)

	gen := &core.Genesis{
		Config:    config,
		Timestamp: conf.timestamp,
		Alloc:     alloc,
	}

	tdb := state.NewDatabaseWithConfig(db, conf.tdbConfig).TrieDB()
	_, hash, err := core.SetupGenesisBlock(db, tdb, gen)
	require.NoError(tb, err, "core.SetupGenesisBlock()")
	require.NoErrorf(tb, tdb.Commit(hash, true), "%T.Commit(core.SetupGenesisBlock(...))", tdb)

	b := NewBlock(tb, gen.ToBlock(), nil, nil)
	h := &hookstest.Stub{Target: conf.gasTarget}
	require.NoErrorf(tb, b.MarkSynchronous(h, db, xdb, conf.gasExcess), "%T.MarkSynchronous()", b)
	return b
}

type genesisConfig struct {
	tdbConfig *triedb.Config
	timestamp uint64
	gasTarget gas.Gas
	gasExcess gas.Gas
}

// A GenesisOption configures [NewGenesis].
type GenesisOption = options.Option[genesisConfig]

// WithTrieDBConfig override the [triedb.Config] used by [NewGenesis].
func WithTrieDBConfig(tc *triedb.Config) GenesisOption {
	return options.Func[genesisConfig](func(gc *genesisConfig) {
		gc.tdbConfig = tc
	})
}

// WithTimestamp overrides the timestamp used by [NewGenesis].
func WithTimestamp(timestamp uint64) GenesisOption {
	return options.Func[genesisConfig](func(gc *genesisConfig) {
		gc.timestamp = timestamp
	})
}

// WithGasTarget overrides the gas target used by [NewGenesis].
func WithGasTarget(target gas.Gas) GenesisOption {
	return options.Func[genesisConfig](func(gc *genesisConfig) {
		gc.gasTarget = target
	})
}

// WithGasExcess overrides the gas excess used by [NewGenesis].
func WithGasExcess(excess gas.Gas) GenesisOption {
	return options.Func[genesisConfig](func(gc *genesisConfig) {
		gc.gasExcess = excess
	})
}
