// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevmtest

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip/txgossiptest"
	"github.com/ava-labs/avalanchego/vms/saevm/vmtest"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	libevmhookstest "github.com/ava-labs/libevm/libevm/hookstest"
)

// ChainID is made a global to keep it constant across multiple SUTs.
var ChainID = ids.GenerateTestID()

type Config struct {
	VMConfig    sae.Config
	LogLevel    logging.Level
	Genesis     core.Genesis
	DB          database.Database
	Precompiles map[common.Address]libevm.PrecompiledContract
}

type Option = options.Option[Config]

// WithVMTime returns an option to configure a new SUT's "now" function along
// with a struct to access and set the time at nanosecond resolution.
func WithVMTime(tb testing.TB, startTime time.Time) (Option, *VMTime) {
	tb.Helper()
	t := &VMTime{Time: startTime}
	opt := options.Func[Config](func(c *Config) {
		c.VMConfig.Now = t.Now
	})
	return opt, t
}

func WithCommitInterval(interval uint64) Option {
	return options.Func[Config](func(c *Config) {
		c.VMConfig.DBConfig.TrieCommitInterval = interval
	})
}

func WithBloomSectionSize(size uint64) Option {
	return options.Func[Config](func(c *Config) {
		c.VMConfig.RPCConfig.BlocksPerBloomSection = size
	})
}

// WithBlockingPrecompile adds a precompile that will block
// all execution until the releasing function returned is called.
// This should be called prior to closing the VM to prevent goroutine
// leaks. The releaser can be called multiple times.
func WithBlockingPrecompile(addr common.Address) (Option, func()) {
	unblock := make(chan struct{})
	p := vm.NewStatefulPrecompile(func(vm.PrecompileEnvironment, []byte) ([]byte, error) {
		<-unblock
		return nil, nil
	})
	return WithPrecompile(addr, p), sync.OnceFunc(func() { close(unblock) })
}

// WithPrecompile adds any precompile at the specified address.
func WithPrecompile(addr common.Address, precompile libevm.PrecompiledContract) Option {
	return options.Func[Config](func(c *Config) {
		if c.Precompiles == nil {
			c.Precompiles = make(map[common.Address]libevm.PrecompiledContract)
		}
		c.Precompiles[addr] = precompile
	})
}

// SUT is the system under test. Testing SHOULD be performed via the embedded
// types as these most accurately reflect the public API. Any access to the
// other fields SHOULD instead be exposed as methods, such as [SUT.StateAt], to
// avoid over-reliance on internal implementation details.
type SUT struct {
	*vmtest.SUT[*sae.VM]
	block.ChainVM
	*ethclient.Client
	RPCClient *rpc.Client

	Genesis *blocks.Block
	Wallet  *saetest.Wallet
	DB      ethdb.Database

	Validators *validatorstest.State
	Sender     *enginetest.Sender
}

// NewSUT constructs a [SUT] using the provided hooks.
func NewSUT[T hook.Transaction](tb testing.TB, numAccounts uint, hooks hook.PointsG[T], opts ...Option) (context.Context, *SUT) {
	tb.Helper()

	mempoolConf := legacypool.DefaultConfig // copies
	mempoolConf.Journal = "/dev/null"

	keys := saetest.NewUNSAFEKeyChain(tb, numAccounts)

	conf := options.ApplyTo(&Config{
		VMConfig: sae.Config{
			MempoolConfig: mempoolConf,
		},
		LogLevel: logging.Debug,
		Genesis: core.Genesis{
			Config:     saetest.ChainConfig(),
			Alloc:      saetest.MaxAllocFor(keys.Addresses()...),
			Timestamp:  saeparams.TauSeconds,
			Difficulty: big.NewInt(0), // irrelevant but required
		},
		DB: memdb.New(),
	}, opts...)

	vm := sae.NewSinceGenesis(hooks, conf.VMConfig)
	snow := adaptor.Convert(vm)
	tb.Cleanup(func() {
		ctx := context.WithoutCancel(tb.Context())
		require.NoError(tb, snow.Shutdown(ctx), "Shutdown()")
	})

	logger := loggingtest.New(tb, conf.LogLevel)
	ctx := logger.CancelOnError(tb.Context())
	snowCtx := snowtest.Context(tb, ChainID)
	snowCtx.Log = logger

	sender := &enginetest.Sender{
		SendAppGossipF: func(context.Context, snowcommon.SendConfig, []byte) error {
			return nil
		},
	}

	require.NoError(tb, snow.Initialize(
		ctx,
		snowCtx,
		conf.DB,
		marshalJSON(tb, conf.Genesis),
		nil, // upgrade bytes
		nil, // config bytes (not ChainConfig)
		nil, // Fxs
		sender,
	), "Initialize()")

	if len(conf.Precompiles) > 0 {
		// All precompile registrations must occur after the VM is initialized,
		// since [libevmhookstest.Stub] doesn't support JSON round-tripping,
		// However, it must occur before the cleanup ensuring that all blocks
		// are executed, since registering the precompile also adds a cleanup to
		// remove the libevm registration.
		registerPrecompiles(tb, conf.Precompiles)
	}
	tb.Cleanup(func() {
		ctx := context.WithoutCancel(tb.Context())
		require.NoError(tb, vm.LastAcceptedBlock().WaitUntilExecuted(ctx), "{last-accepted block}.WaitUntilExecuted()")
	})

	rpcClient, ethClient := dialRPC(ctx, tb, snow)

	validators, ok := snowCtx.ValidatorState.(*validatorstest.State)
	require.Truef(tb, ok, "unexpected type %T for snowCtx.ValidatorState", snowCtx.ValidatorState)
	return ctx, &SUT{
		SUT:       &vmtest.SUT[*sae.VM]{RawVM: vm.VM, Logger: logger},
		ChainVM:   snow,
		Client:    ethClient,
		RPCClient: rpcClient,
		Genesis:   vm.LastSettledBlock(),
		Wallet: saetest.NewWalletWithKeyChain(
			keys,
			types.LatestSigner(conf.Genesis.Config),
		),
		DB: sae.NewEthDB(conf.DB),

		Validators: validators,
		Sender:     sender,
	}
}

func dialRPC(ctx context.Context, tb testing.TB, snow block.ChainVM) (*rpc.Client, *ethclient.Client) {
	tb.Helper()

	handlers, err := snow.CreateHandlers(ctx)
	require.NoErrorf(tb, err, "%T.CreateHandlers()", snow)
	server := httptest.NewServer(handlers[sae.WSHTTPExtensionPath])
	tb.Cleanup(server.Close)

	return vmtest.DialWS(tb, "ws://"+server.Listener.Addr().String())
}

func marshalJSON(tb testing.TB, v any) []byte {
	tb.Helper()
	buf, err := json.Marshal(v)
	require.NoErrorf(tb, err, "json.Marshal(%T)", v)
	return buf
}

// registerPrecompiles registers all `precompiles` as a libevm precompile.
// As a side effect, a [testing.TB.Cleanup] will also be added, removing
// the registration. This cleanup must run AFTER all transactions are
// executed.
func registerPrecompiles(tb testing.TB, precompiles map[common.Address]libevm.PrecompiledContract) {
	tb.Helper()
	(&libevmhookstest.Stub{PrecompileOverrides: precompiles}).Register(tb)
}

// CallContext propagates its arguments to and from [SUT.RPCClient.CallContext].
// Embedding both the [ethclient.Client] and the underlying [rpc.Client] isn't
// possible due to a name conflict, so this method is manually exposed.
func (s *SUT) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return s.RPCClient.CallContext(ctx, result, method, args...)
}

func (s *SUT) NodeID() ids.NodeID {
	return s.RawVM.NodeID()
}

// MustSendTx guarantees all transactions are delivered to the mempool, which triggers
// an asynchronous reorg to move all possible transactions from the source addresses
// to the pending label.
func (s *SUT) MustSendTx(tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()

	ctx := s.Context(tb)
	for _, tx := range txs {
		require.NoErrorf(tb, s.Client.SendTransaction(ctx, tx), "%T.SendTransaction([%#x])", s.Client, tx.Hash())
	}
}

// SendTxsAndWaitUntilPending sends all `txs` to the mempool, and waits for
// each to be marked as pending.
//
// WARNING: if there is a block executing concurrently with this method,
// the pending state of the transactions may not be accurately reflected,
// resulting in a timeout.
func (s *SUT) SendTxsAndWaitUntilPending(tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()

	s.MustSendTx(tb, txs...)
	s.WaitUntilTxsPending(tb, txs...)
}

// WaitUntilTxsPending waits until all `txs` are marked as pending in the mempool.
//
// WARNING: if there is a block executing concurrently with this method,
// the pending state of the transactions may not be accurately reflected,
// resulting in a timeout.
func (s *SUT) WaitUntilTxsPending(tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()

	txgossiptest.WaitUntilPending(tb, s.Context(tb), s.RawVM.GethRPCBackends(), txs...)
}

func (s *SUT) StateAt(tb testing.TB, root common.Hash) *state.StateDB {
	tb.Helper()
	sdb, err := s.RawVM.StateDB(root)
	require.NoErrorf(tb, err, "state.New(%#x, %T.StateCache())", root, s.RawVM)
	return sdb
}

// Unwrap is a convenience (un)wrapper for calling [adaptor.Block.Unwrap] after
// confirming the concrete type of `b`.
func Unwrap(tb testing.TB, b snowman.Block) *blocks.Block {
	tb.Helper()
	switch b := b.(type) {
	case adaptor.Block[*blocks.Block]:
		return b.Unwrap()
	default:
		tb.Fatalf("snowman.Block of concrete type %T", b)
		return nil
	}
}

// AssertBlockHashInvariants MUST NOT be called concurrently with
// [VM.AcceptBlock] as it depends on the last-accepted block. It also blocks
// until said block has finished execution.
func (s *SUT) AssertBlockHashInvariants(ctx context.Context, t *testing.T) {
	t.Helper()
	t.Run("block_hash_invariants", func(t *testing.T) {
		b := s.LastAcceptedBlock(t)
		require.NoError(t, b.WaitUntilExecuted(ctx), "WaitUntilExecuted()")
		t.Logf("Last accepted (and executed) block: %d", b.Height())

		for num, want := range map[rpc.BlockNumber]common.Hash{
			rpc.BlockNumber(b.Number().Int64()): b.Hash(),
			rpc.LatestBlockNumber:               b.Hash(),               // Because we've waited until it's executed
			rpc.SafeBlockNumber:                 b.LastSettled().Hash(), // Safe from disk corruption, not re-org, as acceptance guarantees finality
			rpc.FinalizedBlockNumber:            b.LastSettled().Hash(), // Because we maintain label monotonicity
		} {
			t.Run(num.String(), func(t *testing.T) {
				got, err := s.Client.HeaderByNumber(ctx, big.NewInt(num.Int64()))
				require.NoErrorf(t, err, "%T.HeaderByNumber(%v)", s.Client, num)
				assert.Equalf(t, want, got.Hash(), "%T.HeaderByNumber(%v).Hash()", s.Client, num)
			})
		}

		// The RPC implementation doesn't use the database to resolve the block
		// labels above, so we still need to check them.
		assert.Equal(t, b.Hash(), rawdb.ReadHeadBlockHash(s.DB), "rawdb.ReadHeadBlockHash() MUST reflect last-executed block")
		assert.Equal(t, b.LastSettled().Hash(), rawdb.ReadFinalizedBlockHash(s.DB), "rawdb.ReadFinalizedBlockHash() MUST reflect last-settled block")
	})
}

type VMTime struct {
	time.Time
}

func (t *VMTime) Now() time.Time {
	return t.Time
}

func (t *VMTime) Set(n time.Time) {
	t.Time = n
}

func (t *VMTime) Advance(d time.Duration) {
	t.Time = t.Time.Add(d)
}

// AdvanceToSettle advances the time such that the next call to [VMTime.Now] is
// at or after the time required to settle `b`. Note that at least one more
// accepted [blocks.Block] is still required to actually settle `b`.
func (t *VMTime) AdvanceToSettle(ctx context.Context, tb testing.TB, b *blocks.Block) {
	tb.Helper()
	require.NoErrorf(tb, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	to := b.ExecutedByGasTime().AsTime().Add(saeparams.Tau)
	if t.Before(to) {
		t.Set(to)
	}
}
