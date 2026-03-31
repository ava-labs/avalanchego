// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"runtime"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/strevm/intmath"
	saeparams "github.com/ava-labs/strevm/params"
	"github.com/ava-labs/strevm/worstcase"
)

var worstCaseFuzzFlags struct {
	numAccounts       uint
	balance           uint256.Int
	parallel          uint
	numBlocks         uint
	maxNewTxsPerBlock uint
	maxGasLimit       uint64
	maxTxValue        uint64
	rngSeed           uint64
}

func createWorstCaseFuzzFlags(set *flag.FlagSet) {
	name := func(n string) string {
		return fmt.Sprintf("worstcase.fuzz.%s", n)
	}
	fs := &worstCaseFuzzFlags

	set.UintVar(&fs.numAccounts, name("num_eoa"), 10, "Number of EOAs to send funds between")
	set.TextVar(&fs.balance, name("eoa_balance"), uint256.NewInt(params.Ether), "Starting balance of EOAs")
	set.UintVar(&fs.parallel, name("parallel"), uint(runtime.GOMAXPROCS(0)), "Number of parallel tests to run; defaults to GOMAXPROCS") //nolint:gosec // Known to be positive
	set.UintVar(&fs.numBlocks, name("blocks"), 50, "Number of blocks to build and execute (fixed)")
	set.UintVar(&fs.maxNewTxsPerBlock, name("max_new_txs"), 100, "Maximum number of new transactions to send before building each block (uniform distribution)")
	set.Uint64Var(&fs.maxGasLimit, name("max_gas_limit"), 60e6, "Maximum gas limit per transaction (uniform distribution)")
	set.Uint64Var(&fs.maxTxValue, name("max_tx_value"), params.Ether/1000, "Maximum tx value to send per transaction (uniform distribution)")
	set.Uint64Var(&fs.rngSeed, name("rng_seed"), 0, "Seed for random-number generator; ignored if zero")
}

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
func (g *guzzler) guzzle(env vm.PrecompileEnvironment, input []byte) ([]byte, error) {
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
	flags := worstCaseFuzzFlags
	t.Logf("Flags: %+v", flags)

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
			acc.Balance.Set(flags.balance.ToBig())
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
		for _, tt := range precompileTests {
			var data []byte
			if k := tt.keep; k != nil {
				data = binary.BigEndian.AppendUint64(nil, *k)
			}
			sut.mustSendTx(t, sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
				To:        &guzzle,
				GasFeeCap: big.NewInt(1),
				Gas:       tt.limit,
				Data:      data,
			}))
		}

		b := sut.runConsensusLoop(t)
		require.NoError(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
		require.Lenf(t, b.Receipts(), len(precompileTests), "%T.Receipts()", b)
		for i, r := range b.Receipts() {
			assert.Equalf(t, precompileTests[i].wantUsed, r.GasUsed, "%T.GasUsed", r)
		}
	})
	if t.Failed() {
		t.FailNow()
	}

	for range flags.parallel {
		t.Run("fuzz", func(t *testing.T) {
			t.Parallel()

			timeOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

			ctx, sut := newSUT(t, flags.numAccounts, sutOpt, timeOpt)
			// If we don't wait for blocks to be executed then their results may
			// not be ready once they need to be settled, which will result in a
			// WARNING log, which is considered an error. VMs in a bootstrapping
			// state will automatically wait for execution.
			require.NoError(t, sut.SetState(ctx, snow.Bootstrapping), "SetState(Bootstrapping)")

			addrs := sut.wallet.Addresses()
			numEOAs := len(addrs)
			addrs = append(addrs, guzzle)
			guzzlerIdx := numEOAs

			var seed uint64
			if flags.rngSeed != 0 {
				seed = flags.rngSeed
			} else {
				seed = rand.Uint64() //nolint:gosec // Not for security
			}
			t.Logf("RNG seed: %d", seed)
			rng := rand.New(rand.NewPCG(0, seed)) //nolint:gosec // Allow for reproducibility

			for range flags.numBlocks {
				for range rng.UintN(flags.maxNewTxsPerBlock) {
					from := rng.IntN(numEOAs)
					to := rng.IntN(numEOAs + 1)
					gasLim := params.TxGas + rng.Uint64N(flags.maxGasLimit)
					var data []byte
					if to == guzzlerIdx {
						data = binary.BigEndian.AppendUint64(nil, rng.Uint64N(gasLim))
					}

					tx := sut.wallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
						To:        &addrs[to],
						GasFeeCap: big.NewInt(1 + rng.Int64N(100)),
						Gas:       gasLim,
						Data:      data,
						Value:     uint256.NewInt(rng.Uint64N(flags.maxTxValue)).ToBig(),
					})

					if err := sut.SendTransaction(ctx, tx); err != nil {
						sut.wallet.DecrementNonce(t, from)
					}
				}
				sut.syncMempool(t)

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
						accepted = true
					}
				}
			}
		})
	}
}
