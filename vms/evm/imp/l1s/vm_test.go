// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"encoding/json"
	"maps"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/stateupgrade"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmtest"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
	"github.com/ava-labs/avalanchego/vms/evm/txgossip/txgossiptest"

	l1params "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	legacy "github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	saeparams "github.com/ava-labs/avalanchego/vms/evm/params"
	saerpc "github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	goleak.VerifyTestMain(
		m,
		append(
			saetest.GoleakOptions(),
			// The legacy VM, started by [TestLegacyVMParsesIglooBlocks], leaks
			// these after Shutdown.
			goleak.IgnoreTopFunction("github.com/ava-labs/libevm/accounts/keystore.(*KeyStore).updater"),
			goleak.IgnoreTopFunction("github.com/ava-labs/libevm/accounts/keystore.(*watcher).loop"),
			goleak.IgnoreTopFunction("github.com/ava-labs/libevm/accounts.(*Manager).update"),
			goleak.IgnoreTopFunction("github.com/ava-labs/avalanchego/graft/subnet-evm/eth/gasprice.NewOracle.func1"),
			goleak.IgnoreTopFunction("github.com/ava-labs/avalanchego/graft/subnet-evm/eth/gasprice.newFeeInfoProvider.func1"),
			// The keystore's file watcher, whose backend varies by platform.
			goleak.IgnoreAnyFunction("github.com/fsnotify/fsnotify.(*inotify).readEvents"),
			goleak.IgnoreAnyFunction("github.com/fsnotify/fsnotify.(*kqueue).readEvents"),
		)...,
	)
}

var _ saetest.Peer = (*SUT)(nil)

// SUT is the system under test for the L1 [VM]. It bundles the [VM] itself and
// an [ethclient.Client] connected to an in-process [httptest.Server].
type SUT struct {
	*VM
	*vmtest.SUT

	snowCtx *snow.Context
	// genesis is the genesis JSON that the VM was initialized with.
	genesis    []byte
	clock      *saetest.Clock
	validators *warptest.Validators
	p2pclient  *saetest.CapturingPeer
}

type (
	sutConfig struct {
		chainID          ids.ID
		genesis          core.Genesis
		upgrades         extras.UpgradeConfig
		networkUpgrades  upgrade.Config
		clock            *saetest.Clock
		vmConfig         config
		db               database.Database
		chainDataDir     string
		currentValidator *validators.GetCurrentValidatorOutput
		state            snow.State
	}
	sutOption = options.Option[sutConfig]
)

// withChainID sets the chain ID, which is random by default.
func withChainID(id ids.ID) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.chainID = id
	})
}

func withMaxAllocFor(addrs ...common.Address) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		maps.Copy(c.genesis.Alloc, saetest.MaxAllocFor(addrs...))
	})
}

// withAccount adds addr to the genesis allocation.
func withAccount(addr common.Address, acc types.Account) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc[addr] = acc
	})
}

// withGenesisPrecompiles enables p at genesis.
func withGenesisPrecompiles(p extras.Precompiles) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		l1params.GetExtra(c.genesis.Config).GenesisPrecompiles = p
	})
}

// withAllowFeeRecipients lets block builders choose the fee recipient.
func withAllowFeeRecipients() sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		l1params.GetExtra(c.genesis.Config).AllowFeeRecipients = true
	})
}

func withUpgrades(u extras.UpgradeConfig) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.upgrades = u
	})
}

// withIglooTime schedules Igloo at t. Earlier upgrades remain initially
// active.
func withIglooTime(t time.Time) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.networkUpgrades.IglooTime = t
	})
}

var testStartTime = upgrade.InitiallyActiveTime.Add(time.Duration(acp226.InitialDelayExcess.Delay()) * time.Millisecond)

// withVMTime sets the clock used by the VM and returns it so that tests can
// control it. The option MAY be passed to multiple SUTs so that they share the
// clock.
func withVMTime(startTime time.Time) (sutOption, *saetest.Clock) {
	c := saetest.NewClock(startTime, time.Millisecond)
	opt := options.Func[sutConfig](func(cfg *sutConfig) {
		cfg.clock = c
	})
	return opt, c
}

// withDB initializes the VM on db, which MAY hold the state of a previous VM.
func withDB(db database.Database) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.db = db
	})
}

// withChainDataDir sets the chain data directory, which holds the standalone
// database and the execution results. A restarted VM MUST reuse it.
func withChainDataDir(dir string) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.chainDataDir = dir
		c.vmConfig.DatabasePath = defaultConfig(dir).DatabasePath
	})
}

// withFeeRecipient sets the node's fee recipient.
func withFeeRecipient(addr common.Address) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.FeeRecipient = addr
	})
}

// withGasTarget sets the node's preferred gas target.
func withGasTarget(target gas.Gas) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.GasTarget = &target
	})
}

// withAPIs serves only apis.
func withAPIs(apis set.Set[saerpc.API]) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.APIs = apis
	})
}

// withCurrentValidator makes the given validator the only current one, as
// tracked by the uptime tracker.
func withCurrentValidator(validationID ids.ID, nodeID ids.NodeID, startTime uint64) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.currentValidator = &validators.GetCurrentValidatorOutput{
			ValidationID:  validationID,
			NodeID:        nodeID,
			Weight:        1,
			StartTime:     startTime,
			IsActive:      true,
			IsL1Validator: true,
		}
	})
}

// withState sets the consensus state that the SUT starts in, which is
// [snow.NormalOp] by default.
func withState(state snow.State) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.state = state
	})
}

// withCommitInterval sets the number of blocks between trie commits, which is
// also the interval between state summaries.
func withCommitInterval(n uint64) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.CommitInterval = n
	})
}

// withScheme sets the triedb scheme, which is HashDB by default.
func withScheme(scheme string) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.StateScheme = scheme
	})
}

// withStateSyncEnabled enables state sync, which is disabled by default.
func withStateSyncEnabled() sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.StateSyncEnabled = true
	})
}

// newSUT returns a new SUT in [snow.NormalOp], unless [withState] is used. The returned context is
// cancelled if the VM logs an error.
func newSUT(tb testing.TB, opts ...sutOption) (context.Context, *SUT) {
	tb.Helper()

	ctx, sut, err := tryNewSUT(tb, opts...)
	require.NoError(tb, err, "tryNewSUT()")
	return ctx, sut
}

// tryNewSUT is [newSUT], returning any initialization error. Tests SHOULD use
// [newSUT] unless asserting on such errors.
func tryNewSUT(tb testing.TB, opts ...sutOption) (context.Context, *SUT, error) {
	tb.Helper()

	chainDataDir := tb.TempDir()
	cfg := options.ApplyTo(&sutConfig{
		chainID:         ids.GenerateTestID(),
		genesis:         *testGenesis(),
		networkUpgrades: upgradetest.GetConfig(upgrade.Latest),
		clock:           saetest.NewClock(testStartTime, time.Millisecond),
		vmConfig:        defaultConfig(chainDataDir),
		db:              memdb.New(),
		chainDataDir:    chainDataDir,
		state:           snow.NormalOp,
	}, opts...)
	vm := &VM{
		now: cfg.clock.Now,
	}

	snowCtx := snowtest.Context(tb, cfg.chainID)
	snowCtx.ChainDataDir = cfg.chainDataDir
	snowCtx.NetworkUpgrades = cfg.networkUpgrades
	log := loggingtest.New(tb, logging.Debug)
	snowCtx.Log = log

	var (
		currentValidators = map[ids.ID]*validators.GetCurrentValidatorOutput{}
		validatorOpts     = []warptest.Option{warptest.WithMinimum(2)}
	)
	if v := cfg.currentValidator; v != nil {
		currentValidators[v.ValidationID] = v
		validatorOpts = append(validatorOpts, warptest.WithNodeIDs(v.NodeID))
	}
	vdrs := warptest.NewValidators(tb, validatorOpts...)
	warptest.SetValidators(tb, snowCtx, vdrs)
	vdrState := snowCtx.ValidatorState.(*validatorstest.State)
	vdrState.GetCurrentValidatorSetF = func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
		return currentValidators, 0, nil
	}
	// The chain belongs to the subnet that the validators validate.
	getSubnetID := vdrState.GetSubnetIDF
	vdrState.GetSubnetIDF = func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
		if chainID == snowCtx.ChainID {
			return snowCtx.SubnetID, nil
		}
		return getSubnetID(ctx, chainID)
	}

	genesisBytes, err := json.Marshal(cfg.genesis)
	require.NoErrorf(tb, err, "json.Marshal(%T)", cfg.genesis)
	upgradeBytes, err := json.Marshal(cfg.upgrades)
	require.NoErrorf(tb, err, "json.Marshal(%T)", cfg.upgrades)
	configBytes, err := json.Marshal(cfg.vmConfig)
	require.NoErrorf(tb, err, "json.Marshal(%T)", cfg.vmConfig)

	validatorIDs := vdrs.NodeIDs()
	appSender := saetest.NewSender(tb, validatorIDs)

	ctx := log.CancelOnError(tb.Context())
	vmSUT, err := vmtest.New(ctx, tb, vm, snowCtx, vmtest.Config{
		DB:       cfg.db,
		Genesis:  genesisBytes,
		Upgrades: upgradeBytes,
		Config:   configBytes,
		Sender:   appSender,
		State:    cfg.state,
	})
	if err != nil {
		return nil, nil, err
	}
	sut := &SUT{
		VM:         vm,
		SUT:        vmSUT,
		snowCtx:    snowCtx,
		genesis:    genesisBytes,
		clock:      cfg.clock,
		validators: vdrs,
		p2pclient:  saetest.NewCapturingPeer(tb, validatorIDs),
	}

	appSender.Start(tb, sut)
	saetest.ConnectTo[saetest.Peer](tb, sut, sut.p2pclient)
	return ctx, sut, nil
}

const (
	// blockBuildAdvance exceeds the ACP-226 minimum block delay.
	blockBuildAdvance = 3 * time.Second
	// settleAdvance exceeds [saeparams.Tau], so the next block settles the
	// last-accepted one.
	settleAdvance = saeparams.Tau + time.Second
)

// newWallet returns a wallet of n accounts that signs for the test chain.
func newWallet(tb testing.TB, n uint) *saetest.Wallet {
	tb.Helper()
	return saetest.NewUNSAFEWallet(tb, n, types.LatestSignerForChainID(big.NewInt(testChainID)))
}

// transferTx returns a tx sending value to to.
func transferTx(to common.Address, value *big.Int) *types.DynamicFeeTx {
	return &types.DynamicFeeTx{
		To:        &to,
		Gas:       ethparams.TxGas,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		Value:     value,
	}
}

// callTx returns a tx calling to with data.
func callTx(to common.Address, data []byte, gasLimit uint64) *types.DynamicFeeTx {
	return &types.DynamicFeeTx{
		To:        &to,
		Gas:       gasLimit,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		Data:      data,
	}
}

// deployTx returns a tx deploying 1 byte of code, so code presence shows
// whether the deploy succeeded.
func deployTx() *types.DynamicFeeTx {
	return &types.DynamicFeeTx{
		Gas:       100_000,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		Data: []byte{
			// mem[0] = 0x01
			byte(vm.PUSH1), 0x01,
			byte(vm.PUSH1), 0x00,
			byte(vm.MSTORE8),
			// return mem[0..1]
			byte(vm.PUSH1), 0x01,
			byte(vm.PUSH1), 0x00,
			byte(vm.RETURN),
		},
	}
}

// send signs data as account from of w, sends it over RPC, and waits until it
// is pending.
func (s *SUT) send(ctx context.Context, tb testing.TB, w *saetest.Wallet, from int, data types.TxData) *types.Transaction {
	tb.Helper()

	tx := w.SetNonceAndSign(tb, from, data)
	client := s.EthClient()
	require.NoErrorf(tb, client.SendTransaction(ctx, tx), "%T.SendTransaction()", client)
	txgossiptest.WaitUntilPending(tb, ctx, s.GethRPCBackends(), tx)
	return tx
}

// requireReceiptStatus asserts that tx was included with status want, and
// returns its receipt.
func (s *SUT) requireReceiptStatus(ctx context.Context, tb testing.TB, tx *types.Transaction, want uint64) *types.Receipt {
	tb.Helper()

	client := s.EthClient()
	receipt, err := client.TransactionReceipt(ctx, tx.Hash())
	require.NoErrorf(tb, err, "%T.TransactionReceipt(%#x)", client, tx.Hash())
	require.Equalf(tb, want, receipt.Status, "status of tx %#x", tx.Hash())
	return receipt
}

// fetchAllowListRole returns the role of addr in the allow list of precompile,
// at blockNumber.
func (s *SUT) fetchAllowListRole(
	ctx context.Context,
	tb testing.TB,
	precompile common.Address,
	addr common.Address,
	blockNumber rpc.BlockNumber,
) allowlist.Role {
	tb.Helper()

	stateDB, _, err := s.GethRPCBackends().StateAndHeaderByNumber(ctx, blockNumber)
	require.NoErrorf(tb, err, "StateAndHeaderByNumber(%d)", blockNumber)
	return allowlist.GetAllowListStatus(stateDB, precompile, addr)
}

// isPrecompileEnabledAtLatest reports whether precompile is enabled at the
// SUT's clock time.
func (s *SUT) isPrecompileEnabledAtLatest(precompile common.Address) bool {
	chainConfig := s.GethRPCBackends().ChainConfig()
	return l1params.GetExtra(chainConfig).IsPrecompileEnabled(precompile, uint64(s.clock.Now().Unix())) //#nosec G115 -- Known non-negative
}

// TestTransfer builds blocks on one SUT and verifies them on another, which
// MUST rebuild the blocks from their headers.
func TestTransfer(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	sender := wallet.Addresses()[0]
	timeOpt, clock := withVMTime(testStartTime)
	var (
		ctx, builder = newSUT(t, withMaxAllocFor(sender), timeOpt)
		_, verifier  = newSUT(t, withMaxAllocFor(sender), timeOpt)
	)

	recipient := common.Address{2}
	const value = 42
	transfer := func() (built, parsed *blocks.Block) {
		t.Helper()

		tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &recipient,
			Value:     big.NewInt(value),
			Gas:       ethparams.TxGas,
			GasTipCap: big.NewInt(1),
			GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		})
		require.NoErrorf(t, builder.EthClient().SendTransaction(ctx, tx), "%T.SendTransaction()", builder.EthClient())
		builder.WaitForPendingTxs(ctx, t, tx)

		blk := builder.BuildVerifyAccept(ctx, t)
		// Advancing the clock ensures that the verifier can't rely on it
		// matching the builder's.
		clock.Advance(time.Second)
		parsed = verifier.ParseVerifyAccept(ctx, t, blk)
		for _, b := range []*blocks.Block{blk, parsed} {
			require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
		}
		return blk, parsed
	}

	first, _ := transfer()
	for _, sut := range []*SUT{builder, verifier} {
		assert.Equal(t, *uint256.NewInt(value), sut.Balance(t, recipient), "balance(recipient) after first transfer")
	}

	clock.AdvanceToSettle(ctx, t, first)
	built, parsed := transfer()
	for _, sut := range []*SUT{builder, verifier} {
		assert.Equal(t, *uint256.NewInt(2 * value), sut.Balance(t, recipient), "balance(recipient) after second transfer")
	}
	for _, b := range []*blocks.Block{built, parsed} {
		settled := b.LastSettled()
		require.NotNilf(t, settled, "%T.LastSettled()", b)
		assert.Equal(t, first.Height(), settled.Height(), "%T.LastSettled().Height()", b)
	}
}

// TestExecutePreIglooBlock checks that a block built before Igloo by the legacy
// VM MUST execute to the state root its header commits to.
func TestExecutePreIglooBlock(t *testing.T) {
	wallet := newWallet(t, 1)
	ctx, sut := newSUT(
		t,
		withMaxAllocFor(wallet.Addresses()...),
		withIglooTime(upgrade.InitiallyActiveTime.Add(time.Hour)),
	)

	var genesis legacy.Genesis
	require.NoError(t, json.Unmarshal(sut.genesis, &genesis), "json.Unmarshal()")
	l1params.GetExtra(genesis.Config).NetworkUpgrades.SetDefaults(sut.snowCtx.NetworkUpgrades)
	require.NoError(t, l1params.SetEthUpgrades(genesis.Config), "l1params.SetEthUpgrades()")

	tx := wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1))
	_, chain, _, err := legacy.GenerateChainWithGenesis(&genesis, dummy.NewFaker(), 1, 1, func(_ int, b *legacy.BlockGen) {
		b.SetCoinbase(blackhole.Address)
		b.AddTx(tx)
	})
	require.NoError(t, err, "legacy.GenerateChainWithGenesis()")
	want := chain[0]

	require.NoErrorf(t, sut.VM.SetState(ctx, snow.Bootstrapping), "%T.SetState(Bootstrapping)", sut.VM)
	wantBytes, err := rlp.EncodeToBytes(want)
	require.NoError(t, err, "rlp.EncodeToBytes()")
	blk, err := sut.VM.ParseBlock(ctx, wantBytes)
	require.NoErrorf(t, err, "%T.ParseBlock()", sut.VM)
	got := sut.ParseVerifyAccept(ctx, t, blk)
	require.NoErrorf(t, got.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", got)
	require.Equalf(t, want.Root(), got.PostExecutionStateRoot(), "%T.PostExecutionStateRoot()", got)
}

// TestMinGasConsumptionFloor asserts that the L1 VM charges the ACP-194 gas
// floor of max(actualGasUsed, ceil(gasLimit/2)).
func TestMinGasConsumptionFloor(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	sender := wallet.Addresses()[0]
	ctx, sut := newSUT(t, withMaxAllocFor(sender))

	const highLimit = 100_000
	tests := []struct {
		name        string
		gasLimit    uint64
		wantGasUsed uint64
	}{
		{
			name:        "low_usage_charged_floor",
			gasLimit:    highLimit,
			wantGasUsed: highLimit / 2,
		},
		{
			name:        "usage_above_floor_charged_actual",
			gasLimit:    ethparams.TxGas,
			wantGasUsed: ethparams.TxGas,
		},
	}

	txs := make([]*types.Transaction, len(tests))
	for i, tt := range tests {
		txs[i] = wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &common.Address{1},
			Gas:       tt.gasLimit,
			GasTipCap: big.NewInt(1),
			GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		})
		require.NoErrorf(t, sut.EthClient().SendTransaction(ctx, txs[i]), "%T.SendTransaction(%s)", sut.EthClient(), tt.name)
	}
	sut.WaitForPendingTxs(ctx, t, txs...)

	blk := sut.BuildVerifyAccept(ctx, t)
	require.NoErrorf(t, blk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", blk)
	receipts := blk.Receipts()
	require.Lenf(t, receipts, len(tests), "%T.Receipts()", blk)

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, txs[i].Hash(), receipts[i].TxHash, "receipt TxHash")
			assert.Equal(t, tt.wantGasUsed, receipts[i].GasUsed, "receipt GasUsed")
		})
	}
}

// TestPrecompileActivation asserts that a precompile upgrade activates at the
// time configured in the upgrade config.
func TestPrecompileActivation(t *testing.T) {
	const upgradeOffset = 10
	upgradeTime := time.Unix(int64(testGenesisTime)+upgradeOffset, 0) //#nosec G115 -- Won't overflow
	upgradeUnix := testGenesisTime + upgradeOffset

	var (
		recipient = common.Address{'r', 'e', 'c', 'i', 'p', 'i', 'e', 'n', 't'}
		amount    = uint256.NewInt(42)
	)
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	timeOpt, _ := withVMTime(upgradeTime)
	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		timeOpt,
		withUpgrades(extras.UpgradeConfig{
			PrecompileUpgrades: []extras.PrecompileUpgrade{{
				Config: nativeminter.NewConfig(&upgradeUnix, nil, nil, nil, map[common.Address]*math.HexOrDecimal256{
					recipient: (*math.HexOrDecimal256)(amount.ToBig()),
				}),
			}},
		}),
	)

	// tx is unrelated, but block can't be empty.
	tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &common.Address{1},
		Gas:       ethparams.TxGas,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
	})
	require.NoErrorf(t, sut.EthClient().SendTransaction(ctx, tx), "%T.SendTransaction()", sut.EthClient())
	sut.WaitForPendingTxs(ctx, t, tx)

	activating := sut.BuildVerifyAccept(ctx, t)
	require.NoErrorf(t, activating.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", activating)

	require.Equal(t, upgradeUnix, activating.Header().Time, "block time MUST activate the precompile")
	assert.Equal(t, *amount, sut.Balance(t, recipient), "balance(recipient) after activating block")
}

// TestStateUpgradeAppliedAtActivationSAE asserts that a state upgrade MUST
// apply in the first block at or after its timestamp.
func TestStateUpgradeAppliedAtActivationSAE(t *testing.T) {
	var (
		activationTime = testStartTime.Add(saeparams.Tau)
		activationTS   = uint64(activationTime.Unix()) // #nosec G115 -- known positive test timestamp

		// target is unfunded, so the upgrade's effects are unambiguous.
		target       = common.HexToAddress("0x00000000000000000000000000000000DeadBeef")
		balanceBump  = big.NewInt(123_456_789)
		storageSlot  = common.HexToHash("0x01")
		storageValue = common.HexToHash("0xcafe")
	)

	wallet := newWallet(t, 1)
	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		withUpgrades(extras.UpgradeConfig{
			StateUpgrades: []stateupgrade.Block{{
				Timestamp: &activationTS,
				Accounts: map[common.Address]stateupgrade.Account{
					target: {
						BalanceChange: (*math.HexOrDecimal256)(balanceBump),
						Storage:       map[common.Hash]common.Hash{storageSlot: storageValue},
					},
				},
			}},
		}),
	)
	client := sut.EthClient()

	preBalance, err := client.BalanceAt(ctx, target, nil)
	require.NoError(t, err)
	require.Zero(t, preBalance.Sign(), "target balance must be 0 before activation")

	preStorage, err := client.StorageAt(ctx, target, storageSlot, nil)
	require.NoError(t, err)
	require.Equal(t, common.Hash{}.Bytes(), preStorage, "storage slot must be empty before activation")

	// Blocks are only built for pending txs.
	sut.clock.Set(activationTime)
	sut.send(ctx, t, wallet, 0, transferTx(common.Address{1}, common.Big1))
	sut.RunConsensusLoop(ctx, t)

	postBalance, err := client.BalanceAt(ctx, target, nil)
	require.NoError(t, err)
	require.Zerof(t, postBalance.Cmp(balanceBump),
		"target balance must reflect StateUpgrade balance bump (want=%s got=%s)", balanceBump, postBalance)

	postStorage, err := client.StorageAt(ctx, target, storageSlot, nil)
	require.NoError(t, err)
	require.Equal(t, storageValue.Bytes(), postStorage, "storage slot must reflect the StateUpgrade")
}

// TestRestartRejectsActivatedNetworkUpgradeChange asserts that a restart MUST
// fail if it reschedules an upgrade that the chain already activated.
func TestRestartRejectsActivatedNetworkUpgradeChange(t *testing.T) {
	iglooAt := func(timestamp uint64) sutOption {
		return withUpgrades(extras.UpgradeConfig{
			NetworkUpgradeOverrides: &extras.NetworkUpgrades{
				IglooTimestamp: new(timestamp),
			},
		})
	}
	initialIgloo := testGenesisTime + 2

	wallet := newWallet(t, 1)
	timeOpt, _ := withVMTime(time.Unix(int64(initialIgloo+100), 0)) // #nosec G115 -- test timestamp is far below MaxInt64
	shared := []sutOption{
		withMaxAllocFor(wallet.Addresses()...),
		withDB(memdb.New()),
		withChainDataDir(t.TempDir()),
		timeOpt,
	}

	ctx, sut := newSUT(t, slices.Concat(shared, []sutOption{iglooAt(initialIgloo)})...)
	sut.send(ctx, t, wallet, 0, transferTx(common.Address{1}, common.Big1))
	sut.RunConsensusLoop(ctx, t)
	require.NoErrorf(t, sut.Shutdown(ctx), "%T.Shutdown()", sut.VM)

	_, _, err := tryNewSUT(t, slices.Concat(shared, []sutOption{iglooAt(initialIgloo + 1)})...)
	var compatErr *ethparams.ConfigCompatError
	require.ErrorAs(t, err, &compatErr, "tryNewSUT() after rescheduling Igloo")
}
