// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm/ethtest"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/stateupgrade"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/api/l1sclient"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"

	avadb "github.com/ava-labs/avalanchego/database"
	legacyparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	engcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saeparams "github.com/ava-labs/avalanchego/vms/evm/params"
	saerpc "github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
	ethparams "github.com/ava-labs/libevm/params"
)

type l1SUT struct {
	ctx     context.Context
	snowCtx *snow.Context
	vm      *VM

	client *l1sclient.Client

	ethWallet *saetest.Wallet
	// validators is the warp validator set of snowCtx.
	validators *warptest.Validators

	// appResponse and appErr receive the VM's app responses.
	appResponse chan []byte
	appErr      chan *engcommon.AppError

	// Per-boot resources, replaced by [l1SUT.restart].
	shutdownVM func() error
	apiServer  *httptest.Server

	// Inputs reused by [l1SUT.restart].
	baseDB       avadb.Database
	cfg          *l1SUTConfig
	upgrades     upgrade.Config
	genesisBytes []byte
	upgradeBytes []byte
	configBytes  []byte
}

type (
	l1SUTConfig struct {
		fork                    upgrade.Upgrade
		numAccounts             uint
		clockTime               *time.Time
		configureGenesis        func(*core.Genesis, []common.Address)
		iglooGenesis            bool
		configureUpgrade        func([]common.Address) []byte
		feeRecipient            *common.Address
		gasTarget               *gas.Gas
		apis                    set.Set[saerpc.API]
		databasePath            string
		configureValidatorState func(*validatorstest.State)
	}
	l1SUTOption = options.Option[l1SUTConfig]
)

func newL1SUT(t *testing.T, opts ...l1SUTOption) *l1SUT {
	t.Helper()

	cfg := options.ApplyTo(&l1SUTConfig{
		fork:        upgrade.Durango,
		numAccounts: 1,
	}, opts...)

	upgrades := upgradetest.GetConfig(cfg.fork)

	// Error logs from libevm and warn logs from SAE fail the test. Some libevm
	// warnings are expected.
	log.SetDefault(log.NewLogger(ethtest.NewTBLogHandler(t, log.LevelError)))
	logger := loggingtest.New(t, logging.Info)
	ctx := logger.CancelOnError(t.Context())

	baseDB := memdb.New()

	keychain := saetest.NewUNSAFEKeyChain(t, cfg.numAccounts)
	genesis := newTestGenesis(cfg.fork, keychain)
	if cfg.iglooGenesis {
		genesis.Timestamp = uint64(upgrades.IglooTime.Unix()) //#nosec G115 -- known positive test timestamp
	}
	if cfg.configureGenesis != nil {
		cfg.configureGenesis(genesis, keychain.Addresses())
	}
	genesisBytes, err := json.Marshal(genesis)
	require.NoError(t, err)
	var upgradeBytes []byte
	if cfg.configureUpgrade != nil {
		upgradeBytes = cfg.configureUpgrade(keychain.Addresses())
	}

	chainConfig := defaultConfig(t.TempDir())
	if cfg.feeRecipient != nil {
		chainConfig.FeeRecipient = *cfg.feeRecipient
	}
	if cfg.gasTarget != nil {
		chainConfig.GasTarget = cfg.gasTarget
	}
	if cfg.apis != nil {
		chainConfig.APIs = cfg.apis
	}
	chainConfig.UseStandaloneDatabase = cfg.databasePath != ""
	chainConfig.DatabasePath = cfg.databasePath
	configBytes := marshalJSON(t, &chainConfig)

	sut := &l1SUT{
		ctx: ctx,
		ethWallet: saetest.NewWalletWithKeyChain(
			keychain,
			types.LatestSigner(genesis.Config),
		),
		baseDB:       baseDB,
		cfg:          cfg,
		upgrades:     upgrades,
		genesisBytes: genesisBytes,
		upgradeBytes: upgradeBytes,
		configBytes:  configBytes,
	}
	sut.bootVM(t, cfg.clockTime)

	// The cleanup reads the fields at exit, so it closes the latest boot.
	t.Cleanup(func() {
		if sut.shutdownVM != nil {
			require.NoError(t, sut.shutdownVM())
		}
		if sut.apiServer != nil {
			sut.apiServer.Close()
		}
	})
	return sut
}

// bootVM starts a VM on [l1SUT.baseDB] and wires its API into s. Each boot
// gets a new snow context, since metrics namespaces would otherwise conflict,
// and a new validator set.
func (s *l1SUT) bootVM(t *testing.T, clockTime *time.Time) {
	t.Helper()

	snowCtx := newSnowCtx(t, s.upgrades)
	vdrs := warptest.NewValidators(t, warptest.WithMinimum(2))
	warptest.SetValidators(t, snowCtx, vdrs)
	validatorState := snowCtx.ValidatorState.(*validatorstest.State)
	// The uptime tracker needs a current validator set to reach NormalOp.
	validatorState.GetCurrentValidatorSetF = func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
		return map[ids.ID]*validators.GetCurrentValidatorOutput{}, 0, nil
	}
	if s.cfg.configureValidatorState != nil {
		s.cfg.configureValidatorState(validatorState)
	}

	vm, appSender, shutdownVM, err := initVM(s.ctx, snowCtx, s.baseDB, clockTime, s.genesisBytes, s.upgradeBytes, s.configBytes)
	require.NoError(t, err)

	require.NoError(t, vm.SetState(s.ctx, snow.NormalOp))

	handlers, err := vm.CreateHandlers(s.ctx)
	require.NoError(t, err)

	apiMux := http.NewServeMux()
	for path, handler := range handlers {
		apiMux.Handle(path, handler)
	}
	apiServer := httptest.NewServer(apiMux)

	c, err := l1sclient.Dial(s.ctx, apiServer.URL+"/rpc")
	require.NoError(t, err)

	// The engine sets the preference before the first BuildBlock.
	lastID, err := vm.LastAccepted(s.ctx)
	require.NoError(t, err)
	require.NoError(t, vm.SetPreference(s.ctx, lastID, nil))

	s.vm = vm
	s.snowCtx = snowCtx
	s.validators = vdrs
	s.client = c
	s.shutdownVM = shutdownVM
	s.apiServer = apiServer
	s.appResponse = appSender.SentAppResponse
	s.appErr = appSender.SentAppError
}

// restart replaces the VM with a new one on the same database, keeping the
// clock.
func (s *l1SUT) restart(t *testing.T) {
	t.Helper()

	prevClock := s.vm.clock.Time()
	require.NoError(t, s.shutdownVM())
	s.apiServer.Close()
	s.shutdownVM = nil
	s.apiServer = nil

	s.bootVM(t, &prevClock)
}

// initVM initializes a VM with its clock at clockTime, if non-nil. The returned
// shutdown is idempotent.
func initVM(
	ctx context.Context,
	snowCtx *snow.Context,
	db avadb.Database,
	clockTime *time.Time,
	genesisBytes, upgradeBytes, configBytes []byte,
) (*VM, *enginetest.SenderStub, func() error, error) {
	clock := &mockable.Clock{}
	if clockTime != nil {
		clock.Set(*clockTime)
	}
	vm := &VM{
		now:   clock.Time,
		clock: clock,
	}
	appSender := &enginetest.SenderStub{
		SentAppResponse: make(chan []byte, 1),
		SentAppError:    make(chan *engcommon.AppError, 1),
	}
	if err := vm.Initialize(ctx, snowCtx, db, genesisBytes, upgradeBytes, configBytes, nil, appSender); err != nil {
		return nil, nil, nil, err
	}
	var (
		once     sync.Once
		closeErr error
	)
	shutdown := func() error {
		once.Do(func() {
			closeErr = vm.Shutdown(context.WithoutCancel(ctx))
		})
		return closeErr
	}
	return vm, appSender, shutdown, nil
}

func newTestGenesis(fork upgrade.Upgrade, keychain *saetest.KeyChain) *core.Genesis {
	chainConfig := legacyparams.Copy(paramstest.ForkToChainConfig[fork])
	return &core.Genesis{
		Config:     &chainConfig,
		Alloc:      saetest.MaxAllocFor(keychain.Addresses()...),
		Timestamp:  saeparams.TauSeconds,
		Difficulty: big.NewInt(0),
		GasLimit:   legacyparams.DefaultFeeConfig.GasLimit.Uint64(),
	}
}

func withFork(fork upgrade.Upgrade) l1SUTOption { //nolint:unparam // Igloo is currently Latest but caller-controlled by design
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.fork = fork
	})
}

func withNumAccounts(numAccounts uint) l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.numAccounts = numAccounts
	})
}

func withGenesisConfig(fn func(*core.Genesis, []common.Address)) l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.configureGenesis = fn
	})
}

// withIglooGenesis starts the genesis at Igloo, which gaspricemanager genesis
// precompiles require.
func withIglooGenesis() l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.iglooGenesis = true
	})
}

func withUpgradeConfig(fn func([]common.Address) []byte) l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.configureUpgrade = fn
	})
}

// withNow pins the VM's clock, which drives block building and uptime
// tracking.
func withNow(now time.Time) l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.clockTime = &now
	})
}

// withValidatorState lets fn modify the validator state before the VM is
// initialized.
func withValidatorState(fn func(*validatorstest.State)) l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.configureValidatorState = fn
	})
}

// withCurrentValidatorSet makes the given validator the only one, for both
// uptime tracking and peer connectivity.
func withCurrentValidatorSet(validationID ids.ID, nodeID ids.NodeID, startTime uint64) l1SUTOption {
	return withValidatorState(func(s *validatorstest.State) {
		s.GetCurrentValidatorSetF = func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*validators.GetCurrentValidatorOutput{
				validationID: {
					ValidationID:  validationID,
					NodeID:        nodeID,
					Weight:        1,
					StartTime:     startTime,
					IsActive:      true,
					IsL1Validator: true,
				},
			}, 0, nil
		}
		s.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return map[ids.NodeID]*validators.GetValidatorOutput{
				nodeID: {NodeID: nodeID, Weight: 1},
			}, nil
		}
	})
}

// withFeeRecipient sets the node's fee recipient. Without it, fees are burned.
func withFeeRecipient(addr common.Address) l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.feeRecipient = &addr
	})
}

// withGasTarget sets the node's preferred gas target, which the header target
// exponent moves toward.
func withGasTarget(target gas.Gas) l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.gasTarget = &target
	})
}

// withAPIs serves only apis.
func withAPIs(apis set.Set[saerpc.API]) l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.apis = apis
	})
}

// withStandaloneDatabase stores chain data in a standalone database in dir,
// rather than in [l1SUT.baseDB].
func withStandaloneDatabase(dir string) l1SUTOption {
	return options.Func[l1SUTConfig](func(c *l1SUTConfig) {
		c.databasePath = dir
	})
}

func (s *l1SUT) buildAndVerifyBlock(t *testing.T, blockCtx *block.Context) *blocks.Block {
	t.Helper()

	msg, err := s.vm.WaitForEvent(s.ctx)
	require.NoError(t, err)
	require.Equal(t, engcommon.PendingTxs, msg)

	built, err := s.vm.BuildBlock(s.ctx, blockCtx)
	require.NoError(t, err)
	require.NoError(t, s.vm.VerifyBlock(s.ctx, blockCtx, built))
	return built
}

func (s *l1SUT) acceptAndExecuteBlock(t *testing.T, built *blocks.Block) {
	t.Helper()

	require.NoError(t, s.vm.SetPreference(s.ctx, built.ID(), nil))
	require.NoError(t, s.vm.AcceptBlock(s.ctx, built))
	require.NoError(t, built.WaitUntilExecuted(s.ctx))
}

func (s *l1SUT) setTime(t *testing.T, now time.Time) {
	t.Helper()
	s.vm.clock.Set(now)
}

func (s *l1SUT) advanceTime(t *testing.T, d time.Duration) {
	t.Helper()
	s.vm.clock.Set(s.vm.clock.Time().Add(d))
}

func (s *l1SUT) sendTransferTx(t *testing.T, from int, to int, value *big.Int) *types.Transaction {
	t.Helper()

	tx := s.signTransferTx(t, from, to, value)
	require.NoError(t, s.client.SendTransaction(s.ctx, tx))
	return tx
}

func (s *l1SUT) signTransferTx(t *testing.T, from int, to int, value *big.Int) *types.Transaction {
	t.Helper()

	addresses := s.ethWallet.Addresses()
	toAddress := addresses[to]
	return s.ethWallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
		To:        &toAddress,
		Gas:       21_000,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(225 * ethparams.GWei),
		Value:     value,
	})
}

func (s *l1SUT) buildAcceptExecuteBlock(t *testing.T) *blocks.Block {
	t.Helper()

	built := s.buildAndVerifyBlock(t, nil)
	s.acceptAndExecuteBlock(t, built)
	return built
}

// minimalDeployInitCode deploys 1 byte of code, so code presence shows whether
// a deploy succeeded.
var minimalDeployInitCode = []byte{
	// mem[0] = 0x01
	byte(vm.PUSH1), 0x01,
	byte(vm.PUSH1), 0x00,
	byte(vm.MSTORE8),
	// return mem[0..1]
	byte(vm.PUSH1), 0x01,
	byte(vm.PUSH1), 0x00,
	byte(vm.RETURN),
}

// signDeployTx signs a deployment of [minimalDeployInitCode].
func (s *l1SUT) signDeployTx(t *testing.T, from int) *types.Transaction {
	t.Helper()

	return s.ethWallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
		To:        nil, // contract creation
		Gas:       100_000,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(225 * ethparams.GWei),
		Data:      minimalDeployInitCode,
	})
}

func (s *l1SUT) sendDeployTx(t *testing.T, from int) *types.Transaction {
	t.Helper()

	tx := s.signDeployTx(t, from)
	require.NoError(t, s.client.SendTransaction(s.ctx, tx))
	return tx
}

func (s *l1SUT) signCallTx(t *testing.T, from int, to common.Address, data []byte, gas uint64) *types.Transaction {
	t.Helper()

	return s.ethWallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
		To:        &to,
		Gas:       gas,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(225 * ethparams.GWei),
		Data:      data,
	})
}

func (s *l1SUT) sendCallTx(t *testing.T, from int, to common.Address, data []byte, gas uint64) *types.Transaction {
	t.Helper()

	tx := s.signCallTx(t, from, to, data, gas)
	require.NoError(t, s.client.SendTransaction(s.ctx, tx))
	return tx
}

func (s *l1SUT) fetchAllowListRole(
	t *testing.T,
	precompile common.Address,
	address common.Address,
	blockNumber rpc.BlockNumber,
) allowlist.Role {
	t.Helper()

	stateDB, _, err := s.vm.GethRPCBackends().StateAndHeaderByNumber(s.ctx, blockNumber)
	require.NoError(t, err)
	return allowlist.GetAllowListStatus(stateDB, precompile, address)
}

// isPrecompileEnabledAtLatest reports whether precompile is enabled at the
// VM's clock time.
func (s *l1SUT) isPrecompileEnabledAtLatest(precompile common.Address) bool {
	chainConfig := s.vm.GethRPCBackends().ChainConfig()
	return legacyparams.GetExtra(chainConfig).IsPrecompileEnabled(precompile, s.vm.clock.Unix())
}

// requireBlockContainsTxs asserts that block contains exactly want, in any
// order.
func requireBlockContainsTxs(t *testing.T, block *blocks.Block, want ...common.Hash) {
	t.Helper()

	got := make(map[common.Hash]struct{}, len(block.Transactions()))
	for _, tx := range block.Transactions() {
		got[tx.Hash()] = struct{}{}
	}
	require.Lenf(t, got, len(want), "block must contain exactly %d txs, got %d", len(want), len(got))
	for _, h := range want {
		_, ok := got[h]
		require.Truef(t, ok, "block must contain tx %#x", h)
	}
}

func (s *l1SUT) requireTxSucceeded(t *testing.T, tx *types.Transaction) *types.Receipt {
	t.Helper()

	receipt, err := s.client.TransactionReceipt(s.ctx, tx.Hash())
	require.NoErrorf(t, err, "TransactionReceipt(%#x)", tx.Hash())
	require.Equalf(t, types.ReceiptStatusSuccessful, receipt.Status,
		"tx %#x must succeed (status=1)", tx.Hash())
	return receipt
}

// requireTxFailed asserts that tx was included but reverted.
func (s *l1SUT) requireTxFailed(t *testing.T, tx *types.Transaction) *types.Receipt {
	t.Helper()

	receipt, err := s.client.TransactionReceipt(s.ctx, tx.Hash())
	require.NoErrorf(t, err, "TransactionReceipt(%#x)", tx.Hash())
	require.Equalf(t, types.ReceiptStatusFailed, receipt.Status,
		"tx %#x must revert (status=0)", tx.Hash())
	return receipt
}

// requireDeploySucceeded asserts that tx deployed code.
func (s *l1SUT) requireDeploySucceeded(t *testing.T, tx *types.Transaction) {
	t.Helper()
	require.Nil(t, tx.To(), "test setup: tx must be a contract creation")

	receipt := s.requireTxSucceeded(t, tx)
	require.NotEqualf(t, (common.Address{}), receipt.ContractAddress,
		"successful deploy must populate ContractAddress")

	code, err := s.client.CodeAt(s.ctx, receipt.ContractAddress, nil)
	require.NoErrorf(t, err, "CodeAt(%s)", receipt.ContractAddress)
	require.NotEmptyf(t, code, "successful deploy at %s must leave code on chain", receipt.ContractAddress)
}

// requireDeployFailed asserts that tx was included but deployed no code.
func (s *l1SUT) requireDeployFailed(t *testing.T, tx *types.Transaction) {
	t.Helper()
	require.Nil(t, tx.To(), "test setup: tx must be a contract creation")

	receipt := s.requireTxFailed(t, tx)
	// Failed deploys still report the derived address.
	code, err := s.client.CodeAt(s.ctx, receipt.ContractAddress, nil)
	require.NoErrorf(t, err, "CodeAt(%s)", receipt.ContractAddress)
	require.Emptyf(t, code, "failed deploy must leave no code at %s", receipt.ContractAddress)
}

// postIglooStartTime returns Tau after Igloo, so Igloo is active in the
// settled state.
func postIglooStartTime(t *testing.T) time.Time {
	t.Helper()

	networkUpgrades := extras.GetNetworkUpgrades(upgradetest.GetConfig(upgrade.Igloo))
	require.NotNil(t, networkUpgrades.IglooTimestamp)
	return time.Unix(int64(*networkUpgrades.IglooTimestamp), 0).Add(saeparams.Tau) // #nosec G115 -- upgrade timestamp is far below MaxInt64
}

// TestStateUpgradeAppliedAtActivationSAE asserts that a state upgrade MUST
// apply in the first block at or after its timestamp.
func TestStateUpgradeAppliedAtActivationSAE(t *testing.T) {
	const (
		fromIdx = 0
		toIdx   = 1
	)

	var (
		now            = postIglooStartTime(t)
		activationTime = now.Add(saeparams.Tau)
		activationTS   = uint64(activationTime.Unix()) // #nosec G115 -- known positive test timestamp

		// target is unfunded, so the upgrade's effects are unambiguous.
		target       = common.HexToAddress("0x00000000000000000000000000000000DeadBeef")
		balanceBump  = big.NewInt(123_456_789)
		storageSlot  = common.HexToHash("0x01")
		storageValue = common.HexToHash("0xcafe")
	)

	sut := newL1SUT(
		t,
		withFork(upgrade.Igloo),
		withNumAccounts(2),
		withNow(now),
		withUpgradeConfig(func(_ []common.Address) []byte {
			return marshalJSON(t, &extras.UpgradeConfig{
				StateUpgrades: []stateupgrade.Block{
					{
						Timestamp: &activationTS,
						Accounts: map[common.Address]stateupgrade.Account{
							target: {
								BalanceChange: (*math.HexOrDecimal256)(balanceBump),
								Storage:       map[common.Hash]common.Hash{storageSlot: storageValue},
							},
						},
					},
				},
			})
		}),
	)

	preBalance, err := sut.client.BalanceAt(sut.ctx, target, nil)
	require.NoError(t, err)
	require.Zero(t, preBalance.Sign(), "target balance must be 0 before activation")

	preStorage, err := sut.client.StorageAt(sut.ctx, target, storageSlot, nil)
	require.NoError(t, err)
	require.Equal(t, common.Hash{}.Bytes(), preStorage, "storage slot must be empty before activation")

	// Blocks are only built for pending txs.
	sut.setTime(t, activationTime)
	_ = sut.sendTransferTx(t, fromIdx, toIdx, common.Big1)
	_ = sut.buildAcceptExecuteBlock(t)

	postBalance, err := sut.client.BalanceAt(sut.ctx, target, nil)
	require.NoError(t, err)
	require.Zerof(t, postBalance.Cmp(balanceBump),
		"target balance must reflect StateUpgrade balance bump (want=%s got=%s)", balanceBump, postBalance)

	postStorage, err := sut.client.StorageAt(sut.ctx, target, storageSlot, nil)
	require.NoError(t, err)
	require.Equal(t, storageValue.Bytes(), postStorage, "storage slot must reflect the StateUpgrade")
}

func marshalJSON(t *testing.T, v interface{}) []byte {
	t.Helper()

	bytes, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes
}

func newSnowCtx(t *testing.T, upgrades upgrade.Config) *snow.Context {
	t.Helper()

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.NetworkUpgrades = upgrades
	return snowCtx
}

func TestRestartRejectsActivatedNetworkUpgradeChange(t *testing.T) {
	initialIgloo := uint64(upgrade.InitiallyActiveTime.Unix() + 2) // #nosec G115 -- known positive test timestamp
	upgradeBytes := func(timestamp uint64) []byte {
		return marshalJSON(t, extras.UpgradeConfig{
			NetworkUpgradeOverrides: &extras.NetworkUpgrades{
				IglooTimestamp: new(timestamp),
			},
		})
	}

	sut := newL1SUT(
		t,
		withFork(upgrade.Igloo),
		withNumAccounts(2),
		withNow(time.Unix(int64(initialIgloo+100), 0)), // #nosec G115 -- test timestamp is far below MaxInt64
		withUpgradeConfig(func([]common.Address) []byte {
			return upgradeBytes(initialIgloo)
		}),
	)
	sut.sendTransferTx(t, 0, 1, big.NewInt(1))
	sut.buildAcceptExecuteBlock(t)

	clockTime := sut.vm.clock.Time()
	require.NoError(t, sut.shutdownVM(), "VM.Shutdown()")
	sut.apiServer.Close()
	sut.shutdownVM = nil
	sut.apiServer = nil

	snowCtx := newSnowCtx(t, sut.upgrades)
	snowCtx.Log = logging.NoLog{}
	warptest.SetValidators(t, snowCtx, warptest.NewValidators(t, warptest.WithMinimum(2)))
	validatorState := snowCtx.ValidatorState.(*validatorstest.State)
	validatorState.GetCurrentValidatorSetF = func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
		return map[ids.ID]*validators.GetCurrentValidatorOutput{}, 0, nil
	}
	previousLog := log.Root()
	log.SetDefault(log.NewLogger(log.DiscardHandler()))
	t.Cleanup(func() {
		log.SetDefault(previousLog)
	})

	vm, _, shutdown, err := initVM(
		sut.ctx,
		snowCtx,
		sut.baseDB,
		&clockTime,
		sut.genesisBytes,
		upgradeBytes(initialIgloo+1),
		sut.configBytes,
	)
	if shutdown != nil {
		t.Cleanup(func() {
			require.NoError(t, shutdown(), "VM.Shutdown()")
		})
	}
	var compatErr *ethparams.ConfigCompatError
	require.ErrorAs(t, err, &compatErr, "initVM()")
	require.Nil(t, vm, "initVM()")
}
