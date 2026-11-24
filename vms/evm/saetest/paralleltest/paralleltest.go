// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package paralleltest provides a test harness for [parallel] precompiles
// executing under SAE.
package paralleltest

import (
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/libevm/precompiles/parallel"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/blocks/blockstest"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
	"github.com/ava-labs/avalanchego/vms/evm/saexec"

	saehookstest "github.com/ava-labs/avalanchego/vms/evm/hook/hookstest"
	libevmhookstest "github.com/ava-labs/libevm/libevm/hookstest"
)

type config struct {
	precompiles map[common.Address]libevm.PrecompiledContract
}

// An Option configures the default behaviour of [NewExecutor].
type Option = options.Option[config]

// NewExecutor returns a new SAE block-execution queue with a precompile,
// registered at the provided address, that sources results from the
// [parallel.Handler]. The [saexec.Executor] will have a single, genesis block,
// derived from the provided alloc.
//
// This function registers libevm hooks and is therefore not safe for concurrent
// use across multiple calls, nor with other libevm registrations.
func NewExecutor[CommonData, Prefetch any, R parallel.PrecompileResult, Aggregated any](
	tb testing.TB,
	logger logging.Logger,
	db ethdb.Database,
	chainConfig *params.ChainConfig,
	alloc types.GenesisAlloc,
	precompileAddr common.Address,
	handler parallel.Handler[CommonData, Prefetch, R, Aggregated],
	prefetchers, processors int,
	opts ...Option,
) (*saexec.Executor, *blockstest.ChainBuilder) {
	tb.Helper()

	// Although not used until later, the [libevmhookstest.Stub] needs to be
	// registered after the call to [core.SetupGenesisBlock].
	genesis := blockstest.NewGenesis(tb, db, chainConfig, alloc)

	par := parallel.New(prefetchers, processors)
	precompile := parallel.AddAsPrecompile(par, handler)

	cfg := options.ApplyTo(&config{
		precompiles: map[common.Address]libevm.PrecompiledContract{
			precompileAddr: vm.NewStatefulPrecompile(precompile),
		},
	}, opts...)

	vm.RegisterHooks(vmHooks{Processor: par})
	tb.Cleanup(vm.TestOnlyClearRegisteredHooks)
	stub := &libevmhookstest.Stub{
		PrecompileOverrides: cfg.precompiles,
	}
	stub.Register(tb)

	hooks := saehookstest.NewStub(100e6)
	hooks.StartExecutingBlockFn = func(r params.Rules, sdb *state.StateDB, _ *types.Header, b *types.Block) error {
		return par.StartBlock(sdb, r, b)
	}
	hooks.FinishExecutingBlockFn = func(sdb *state.StateDB, b *types.Block, rs types.Receipts) error {
		par.FinishBlock(sdb, b, rs)
		return nil
	}

	xdb := saetest.NewExecutionResultsDB()
	chain := blockstest.NewChainBuilder(genesis)
	src := blocks.Source(chain.GetBlock).AsHeaderSource()
	dbConfig := saedb.Config{CommitInterval: 4096}

	tr, err := saedb.NewTracker(db, dbConfig, genesis.Hash(), tb.TempDir(), logger)
	require.NoError(tb, err, "saedb.NewTracker()")

	exec, err := saexec.New(genesis, new(atomic.Pointer[blocks.Block]), src, chainConfig, db, xdb, tr, hooks, logger, prometheus.NewRegistry())
	require.NoError(tb, err, "saexec.New()")

	tb.Cleanup(func() {
		require.NoErrorf(tb, exec.Close(), "%T.Close()", exec)
		assert.NoErrorf(tb, tr.Close(exec.LastExecuted().PostExecutionStateRoot()), "%T.Close()", tr)
		par.Close()
	})

	return exec, chain
}

// WithExtraPrecompile registers the provided precompile in addition to the one
// created from the [parallel.Handler] passed to [NewExecutor].
func WithExtraPrecompile(a common.Address, p vm.PrecompiledStatefulContract) Option {
	return options.Func[config](func(c *config) {
		c.precompiles[a] = vm.NewStatefulPrecompile(p)
	})
}

type vmHooks struct {
	vm.NOOPHooks
	*parallel.Processor
}

func (h vmHooks) PreprocessingGasCharge(tx common.Hash) (uint64, error) {
	return h.Processor.PreprocessingGasCharge(tx)
}
