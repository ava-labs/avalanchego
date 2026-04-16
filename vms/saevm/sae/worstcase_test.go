// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/intmath"
	"github.com/ava-labs/avalanchego/vms/saevm/worstcase"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// A guzzler is both a [params.ChainConfigHooks] and [params.RulesHooks]. When
// registered as libevm extras they result in the [guzzler.guzzle] method being
// a [vm.PrecompiledStatefulContract] instantiated at the address specified in
// the `Addr` field. Furthermore, a guzzler can be JSON round-tripped, allowing
// it to be included in a chain's genesis.
type guzzler struct {
	params.NOOPHooks `json:"-"`
	Addr             common.Address `json:"guzzlerAddress"`
}

func (*guzzler) register(tb testing.TB) params.ExtraPayloads[*guzzler, *guzzler] {
	tb.Helper()
	tb.Cleanup(params.TestOnlyClearRegisteredExtras)
	return params.RegisterExtras(params.Extras[*guzzler, *guzzler]{
		NewRules: func(_ *params.ChainConfig, _ *params.Rules, g *guzzler, _ *big.Int, _ bool, _ uint64) *guzzler {
			return g
		},
	})
}

func (g *guzzler) ActivePrecompiles(active []common.Address) []common.Address {
	return append(active, g.Addr)
}

func (g *guzzler) PrecompileOverride(a common.Address) (libevm.PrecompiledContract, bool) {
	if a != g.Addr {
		return nil, false
	}
	return vm.NewStatefulPrecompile(g.guzzle), true
}

// guzzle consumes an amount of gas configurable via its input (call data),
// which MUST either be an empty slice or a big-endian uint64 indicating the
// amount of gas to _keep_ (not consume).
func (*guzzler) guzzle(env vm.PrecompileEnvironment, input []byte) ([]byte, error) {
	switch len(input) {
	case 0:
		env.UseGas(env.Gas())
	case 8:
		// We don't know the intrinsic gas that has already been spent, so
		// intepreting the calldata as the amount of gas to consume in total
		// would be impossible without some ugly closures.
		keep := binary.BigEndian.Uint64(input)
		use := intmath.BoundedSubtract(env.Gas(), keep, 0)
		env.UseGas(use)
	default:
		panic("bad test setup; calldata MUST be empty or an 8-byte slice")
	}
	return nil, nil
}

func TestWorstCase(t *testing.T) {
	// TODO(alarso16): This test flakes due to a race in the legacypool. When
	// a block executes, it sends an event to the pool, which causes an
	// incorrect nonce update if the pool already had a pending transaction from
	// the same account.
	if os.Getenv("SAEVM_TEST_FLAKY") == "" {
		t.Skip("FLAKY: set SAEVM_TEST_FLAKY to run")
	}

	const (
		numAccounts       = 10
		numBlocks         = 50
		maxNewTxsPerBlock = 100
		maxGasLimit       = 60e6
		maxTxValue        = params.Ether / 1000
	)
	var (
		balance  = uint256.NewInt(params.Ether)
		parallel = runtime.GOMAXPROCS(0)
	)

	guzzle := common.Address{'g', 'u', 'z', 'z', 'l', 'e'}
	g := &guzzler{Addr: guzzle}
	extras := g.register(t)

	sutOpt := options.Func[sutConfig](func(c *sutConfig) {
		// Avoid polluting a global [params.ChainConfig] with our hooks.
		config := *c.genesis.Config
		c.genesis.Config = &config
		extras.ChainConfig.Set(&config, g)

		c.logLevel = logging.Warn

		for _, acc := range c.genesis.Alloc {
			// Note that `acc` isn't a pointer, but `Balance` is.
			acc.Balance.Set(balance.ToBig())
		}
	})

	t.Run("precompile_test_helper", func(t *testing.T) {
		// Although the precompile is part of the test harness, its behaviour is
		// key to the correctness of the rest of the tests, so we run a few
		// tests on it.
		ctx, sut := newSUT(t, 1, sutOpt)

		newU64 := func(u uint64) *uint64 {
			return &u
		}
		precompileTests := []struct {
			limit    uint64
			keep     *uint64
			wantUsed uint64
		}{
			{
				limit:    23_456,
				wantUsed: 23_456,
			},
			{
				limit:    25_000,
				keep:     newU64(1_000),
				wantUsed: 24_000,
			},
			{
				limit:    25_000,
				keep:     newU64(math.MaxUint64), // >25k and no non-zero bytes
				wantUsed: params.TxGas + 8*params.TxDataNonZeroGasEIP2028,
			},
		}
		txs := make([]*types.Transaction, 0, len(precompileTests))
		for _, tt := range precompileTests {
			var data []byte
			if k := tt.keep; k != nil {
				data = binary.BigEndian.AppendUint64(nil, *k)
			}
			txs = append(txs, sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
				To:        &guzzle,
				GasFeeCap: big.NewInt(1),
				Gas:       tt.limit,
				Data:      data,
			}))
		}

		b := sut.runConsensusLoop(t, txs...)
		require.NoError(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
		require.Lenf(t, b.Receipts(), len(precompileTests), "%T.Receipts()", b)
		for i, r := range b.Receipts() {
			assert.Equalf(t, precompileTests[i].wantUsed, r.GasUsed, "%T.GasUsed", r)
		}
	})
	if t.Failed() {
		t.FailNow()
	}

	for range parallel {
		t.Run("fuzz", func(t *testing.T) {
			t.Parallel()

			timeOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

			ctx, sut := newSUT(t, numAccounts, sutOpt, timeOpt)

			addrs := sut.wallet.Addresses()
			numEOAs := len(addrs)
			addrs = append(addrs, guzzle)
			guzzlerIdx := numEOAs

			rng := rand.New(rand.NewPCG(0, 0)) //#nosec G404 -- Allow for reproducibility

			for range numBlocks {
				for range rng.UintN(maxNewTxsPerBlock) {
					from := rng.IntN(numEOAs)
					to := rng.IntN(numEOAs + 1)
					gasLim := params.TxGas + rng.Uint64N(maxGasLimit)
					var data []byte
					if to == guzzlerIdx {
						data = binary.BigEndian.AppendUint64(nil, rng.Uint64N(gasLim))
					}

					tx := sut.wallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
						To:        &addrs[to],
						GasFeeCap: big.NewInt(1 + rng.Int64N(100)),
						Gas:       gasLim,
						Data:      data,
						Value:     uint256.NewInt(rng.Uint64N(maxTxValue)).ToBig(),
					})

					if err := sut.SendTransaction(ctx, tx); err != nil {
						sut.wallet.DecrementNonce(t, from)
						continue
					}
					sut.waitUntilTxsPending(t, tx)
				}

				for accepted := false; !accepted; {
					vmTime.advance(time.Millisecond * time.Duration(rng.IntN(1000*3*saeparams.TauSeconds)))

					require.NoError(t, sut.SetPreference(ctx, sut.lastAcceptedBlock(t).ID()), "SetPreference()")

					switch b, err := sut.BuildBlock(ctx); {
					case errors.Is(err, worstcase.ErrQueueFull):
						// Breathe the (back)pressure... I'll test ya
					case err != nil:
						t.Fatalf("Unexpected BuildBlock() error: %v", err)
					default:
						require.NoError(t, b.Verify(ctx), "Verify()")
						require.NoError(t, b.Accept(ctx), "Accept()")

						// Ensure the execution results are available for future
						// LastToSettleAt calls.
						require.NoError(t, unwrap(t, b).WaitUntilExecuted(ctx), "WaitUntilExecuted()")

						accepted = true
					}
				}
			}
		})
	}
}
