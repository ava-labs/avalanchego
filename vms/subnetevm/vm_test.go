// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm/ethtest"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rpc"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/subnetevm/api/client"

	engcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

type SUT struct {
	ctx     context.Context
	snowCtx *snow.Context
	vm      *VM

	// client serves both the standard Ethereum surface (via the embedded
	// `*ethclient.Client`) and the subnet-evm-specific methods.
	client *client.Client

	// Wallet for issuing transactions
	ethWallet     *saetest.Wallet
	validatorKeys []*localsigner.LocalSigner

	// See [SUT.verifyWarpMessage]
	appResponse chan []byte
	appErr      chan *engcommon.AppError
}

type (
	sutConfig struct {
		fork        upgradetest.Fork
		numAccounts uint
		// clockTime, if non-nil, pins the VM's mockable clock (and
		// transitively `sae.Config.Now` and the uptime tracker) to a
		// deterministic instant. When nil, the VM falls back to wall
		// time. Use [withNow] to set.
		clockTime        *time.Time
		configureGenesis func(*core.Genesis, []common.Address)
		configureUpgrade func([]common.Address) []byte
		// feeRecipient, if non-nil, is passed through
		// `Config.FeeRecipient` (as a hex string) to the VM. When nil, the
		// VM's default (zero address => effective burn) applies.
		feeRecipient *common.Address
		// configureValidatorState, if non-nil, is invoked AFTER the
		// default `*validatorstest.State` has been built (with the
		// always-empty `GetCurrentValidatorSetF` default) and BEFORE
		// `vm.Initialize` runs. Use it to inject a non-trivial validator
		// set for tests that exercise the uptime tracker / validators API.
		configureValidatorState func(*validatorstest.State)
	}
	sutOption = options.Option[sutConfig]
)

func newSUT(t *testing.T, opts ...sutOption) *SUT {
	t.Helper()

	cfg := options.ApplyTo(&sutConfig{
		fork:        upgradetest.Durango,
		numAccounts: 1,
	}, opts...)

	// TODO(alarso16): this will need to be parameterizable
	fork := cfg.fork
	upgrades := upgradetest.GetConfig(fork)

	// Test will fail if any error log from libevm, or warn log from SAE, is emitted.
	// Some warn logs from libevm are expected.
	log.SetDefault(log.NewLogger(ethtest.NewTBLogHandler(t, log.LevelError)))
	logger := saetest.NewTBLogger(t, logging.Info)
	ctx := logger.CancelOnError(t.Context())

	baseDB := memdb.New()
	snowCtx, validatorKeys := newSnowCtx(t, upgrades)
	if cfg.configureValidatorState != nil {
		// Safe: `newSnowCtx` always installs a `*validatorstest.State`.
		cfg.configureValidatorState(snowCtx.ValidatorState.(*validatorstest.State))
	}

	mempoolConf := legacypool.DefaultConfig
	mempoolConf.Journal = "/dev/null"

	keychain := saetest.NewUNSAFEKeyChain(t, cfg.numAccounts)
	genesis := newTestGenesis(cfg.fork, keychain)
	if cfg.configureGenesis != nil {
		cfg.configureGenesis(genesis, keychain.Addresses())
	}
	genesisBytes, err := json.Marshal(genesis)
	require.NoError(t, err)
	var upgradeBytes []byte
	if cfg.configureUpgrade != nil {
		upgradeBytes = cfg.configureUpgrade(keychain.Addresses())
	}

	var configBytes []byte
	if cfg.feeRecipient != nil {
		configBytes = mustMarshalJSON(t, &Config{FeeRecipient: cfg.feeRecipient.Hex()})
	}

	saeConfig := sae.Config{
		MempoolConfig: mempoolConf,
		DBConfig: saedb.Config{
			TrieDBConfig: triedb.HashDefaults,
		},
	}
	vm := New(saeConfig)

	// Pin the VM's mockable clock BEFORE `Initialize` so that
	// `sae.Config.Now` (defaulted to `vm.clock.Time` in `New`) and the
	// validator uptime tracker both observe a deterministic instant
	// from their very first read. Subsequent test-side advances via
	// `sut.setTime`/`advanceTime` flow through the same clock.
	if cfg.clockTime != nil {
		vm.clock.Set(*cfg.clockTime)
	}

	// allow receiving responses via [SUT.verifyWarpMessage]
	appResponseCh := make(chan []byte, 1)
	appErrCh := make(chan *engcommon.AppError, 1)
	appSender := &enginetest.SenderStub{
		SentAppResponse: appResponseCh,
		SentAppError:    appErrCh,
	}

	require.NoError(t, vm.Initialize(
		ctx,
		snowCtx,
		baseDB,
		genesisBytes,
		upgradeBytes,
		configBytes,
		nil,
		appSender,
	))
	t.Cleanup(func() {
		require.NoError(t, vm.Shutdown(context.WithoutCancel(ctx)))
	})

	require.NoError(t, vm.SetState(ctx, snow.NormalOp))

	handlers, err := vm.CreateHandlers(ctx)
	require.NoError(t, err)

	// Mount every handler returned by `CreateHandlers` on a single shared
	// mux so typed JSON-RPC clients can target a single base URL (matching
	// how AvalancheGo serves these in production).
	apiMux := http.NewServeMux()
	for path, handler := range handlers {
		apiMux.Handle(path, handler)
	}
	apiServer := httptest.NewServer(apiMux)
	t.Cleanup(apiServer.Close)

	c, err := client.NewClientWithURL(apiServer.URL)
	require.NoError(t, err)
	t.Cleanup(c.Close)

	// TODO(alarso16): delete this - it should be on the VM
	lastID, err := vm.LastAccepted(ctx)
	require.NoError(t, err)
	require.NoError(t, vm.SetPreference(ctx, lastID, nil))

	return &SUT{
		ctx:     ctx,
		snowCtx: snowCtx,
		vm:      vm,
		client:  c,
		ethWallet: saetest.NewWalletWithKeyChain(
			keychain,
			types.LatestSigner(genesis.Config),
		),
		validatorKeys: validatorKeys,
		appResponse:   appResponseCh,
		appErr:        appErrCh,
	}
}

func newTestGenesis(fork upgradetest.Fork, keychain *saetest.KeyChain) *core.Genesis {
	chainConfig := subnetevmparams.Copy(paramstest.ForkToChainConfig[fork])
	return &core.Genesis{
		Config:     &chainConfig,
		Alloc:      saetest.MaxAllocFor(keychain.Addresses()...),
		Timestamp:  saeparams.TauSeconds,
		Difficulty: big.NewInt(0),
	}
}

func withFork(fork upgradetest.Fork) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.fork = fork
	})
}

func withNumAccounts(numAccounts uint) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.numAccounts = numAccounts
	})
}

func withGenesisConfig(fn func(*core.Genesis, []common.Address)) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.configureGenesis = fn
	})
}

func withUpgradeConfig(fn func([]common.Address) []byte) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.configureUpgrade = fn
	})
}

// withNow pins the VM's mockable clock to `now` before `Initialize`
// runs. The same clock backs `sae.Config.Now` (used by the block
// builder) and the validator uptime tracker, so a single set is enough
// to drive both deterministically.
func withNow(now time.Time) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.clockTime = &now
	})
}

// withValidatorState lets a test mutate the default
// `*validatorstest.State` (e.g. install a `GetCurrentValidatorSetF`
// that returns a non-empty set) before `vm.Initialize` runs.
func withValidatorState(fn func(*validatorstest.State)) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.configureValidatorState = fn
	})
}

// withCurrentValidatorSet seeds the validator state with a single active
// validator pinned to (`validationID`, `nodeID`, `startTime`). Both
// `GetCurrentValidatorSetF` (consumed by `uptimetracker.UptimeTracker`)
// and `GetValidatorSetF` (consumed by `*p2p.Validators` to drive
// `IsConnected`) are populated so that the validator round-trips through
// the entire validators-API surface.
func withCurrentValidatorSet(validationID ids.ID, nodeID ids.NodeID, startTime uint64) sutOption {
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

// withFeeRecipient sets the validator's preferred fee recipient on the VM
// (`Config.FeeRecipient`). Required for any test that exercises a path
// where fees end up at a configurable address — i.e. the
// `AllowFeeRecipients=true` arms (genesis-flag or rewardmanager
// `allowFeeRecipients()`). The address is serialised to hex; the VM's
// [Config.ParsedFeeRecipient] handles the round-trip back to
// [common.Address].
func withFeeRecipient(addr common.Address) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.feeRecipient = &addr
	})
}

func (s *SUT) buildAndVerifyBlock(t *testing.T, blockCtx *block.Context) *blocks.Block {
	t.Helper()

	msg, err := s.vm.WaitForEvent(s.ctx)
	require.NoError(t, err)
	require.Equal(t, engcommon.PendingTxs, msg)

	built, err := s.vm.BuildBlock(s.ctx, blockCtx)
	require.NoError(t, err)
	require.NoError(t, s.vm.VerifyBlock(s.ctx, blockCtx, built))
	return built
}

func (s *SUT) acceptAndExecuteBlock(t *testing.T, built *blocks.Block) {
	t.Helper()

	require.NoError(t, s.vm.SetPreference(s.ctx, built.ID(), nil))
	require.NoError(t, s.vm.AcceptBlock(s.ctx, built))
	require.NoError(t, built.WaitUntilExecuted(s.ctx))
}

// setTime pins the VM's mockable clock (used by both the SAE block
// builder and the validator uptime tracker) to `now`.
func (s *SUT) setTime(t *testing.T, now time.Time) {
	t.Helper()
	s.vm.clock.Set(now)
}

// advanceTime moves the VM's mockable clock forward by `d`.
func (s *SUT) advanceTime(t *testing.T, d time.Duration) {
	t.Helper()
	s.vm.clock.Set(s.vm.clock.Time().Add(d))
}

func (s *SUT) sendTransferTx(t *testing.T, from int, to int, value *big.Int) *types.Transaction {
	t.Helper()

	tx := s.signTransferTx(t, from, to, value)
	require.NoError(t, s.client.SendTransaction(s.ctx, tx))
	return tx
}

func (s *SUT) signTransferTx(t *testing.T, from int, to int, value *big.Int) *types.Transaction {
	t.Helper()

	addresses := s.ethWallet.Addresses()
	toAddress := addresses[to]
	return s.ethWallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
		To:        &toAddress,
		Gas:       21_000,
		GasFeeCap: big.NewInt(225 * subnetevmparams.GWei),
		Value:     value,
	})
}

func (s *SUT) buildAndAcceptBlock(t *testing.T) *blocks.Block {
	t.Helper()

	built := s.buildAndVerifyBlock(t, nil)
	s.acceptAndExecuteBlock(t, built)
	return built
}

// minimalDeployInitCode returns initcode that deploys exactly 1 byte of
// runtime code (`0x01`), so that presence/absence of code at the deploy
// address cleanly distinguishes a successful deploy from a reverted (or
// never-executed) one.
//
//	PUSH1 1; PUSH1 0; MSTORE8  // mem[0] = 0x01
//	PUSH1 1; PUSH1 0; RETURN   // return mem[0..1]
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

// signDeployTx signs (without broadcasting) a contract creation tx from
// account `from` whose initcode deploys [minimalDeployInitCode].
func (s *SUT) signDeployTx(t *testing.T, from int) *types.Transaction {
	t.Helper()

	return s.ethWallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
		To:        nil, // contract creation
		Gas:       100_000,
		GasFeeCap: big.NewInt(225 * subnetevmparams.GWei),
		Data:      minimalDeployInitCode,
	})
}

// sendDeployTx is signDeployTx + broadcast.
func (s *SUT) sendDeployTx(t *testing.T, from int) *types.Transaction {
	t.Helper()

	tx := s.signDeployTx(t, from)
	require.NoError(t, s.client.SendTransaction(s.ctx, tx))
	return tx
}

// signCallTx signs (without broadcasting) a contract call from account `from`
// to `to` with the given calldata and gas limit. Use this for any precompile
// invocation (nativeminter, rewardmanager, allowlist setters, ...): the
// per-precompile sign helper is just `signCallTx(t, from, addr, packedData,
// gasCap)`.
func (s *SUT) signCallTx(t *testing.T, from int, to common.Address, data []byte, gas uint64) *types.Transaction {
	t.Helper()

	return s.ethWallet.SetNonceAndSign(t, from, &types.DynamicFeeTx{
		To:        &to,
		Gas:       gas,
		GasFeeCap: big.NewInt(225 * subnetevmparams.GWei),
		Data:      data,
	})
}

// sendCallTx is signCallTx + broadcast.
func (s *SUT) sendCallTx(t *testing.T, from int, to common.Address, data []byte, gas uint64) *types.Transaction {
	t.Helper()

	tx := s.signCallTx(t, from, to, data, gas)
	require.NoError(t, s.client.SendTransaction(s.ctx, tx))
	return tx
}

// fetchAllowListRole fetches the allowlist role for `address` at `blockNumber`.
func (s *SUT) fetchAllowListRole(
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

// isPrecompileEnabledAtLatest returns whether `precompile` is enabled per the
// chain config at the SUT's current (latest) timestamp.
func (s *SUT) isPrecompileEnabledAtLatest(precompile common.Address) bool {
	chainConfig := s.vm.GethRPCBackends().ChainConfig()
	return subnetevmparams.GetExtra(chainConfig).IsPrecompileEnabled(precompile, s.vm.clock.Unix())
}

// requireBlockContainsTxs asserts that `block` contains exactly the supplied
// tx hashes, ignoring order.
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

// requireTxSucceeded fetches `tx`'s receipt and asserts a successful status
// (status=1).
func (s *SUT) requireTxSucceeded(t *testing.T, tx *types.Transaction) *types.Receipt {
	t.Helper()

	receipt, err := s.client.TransactionReceipt(s.ctx, tx.Hash())
	require.NoErrorf(t, err, "TransactionReceipt(%#x)", tx.Hash())
	require.Equalf(t, types.ReceiptStatusSuccessful, receipt.Status,
		"tx %#x must succeed (status=1)", tx.Hash())
	return receipt
}

// requireTxFailed fetches `tx`'s receipt and asserts a failed status
// (status=0). Use for txs that revert inside the EVM (precompile errors,
// frame-local CanCreateContract rejection, ...): the tx is included in the
// block but its state changes are rolled back.
func (s *SUT) requireTxFailed(t *testing.T, tx *types.Transaction) *types.Receipt {
	t.Helper()

	receipt, err := s.client.TransactionReceipt(s.ctx, tx.Hash())
	require.NoErrorf(t, err, "TransactionReceipt(%#x)", tx.Hash())
	require.Equalf(t, types.ReceiptStatusFailed, receipt.Status,
		"tx %#x must revert (status=0)", tx.Hash())
	return receipt
}

// requireDeploySucceeded fetches `tx`'s receipt and asserts a successful
// contract creation (status=1; deployed code present at receipt's contract
// address).
func (s *SUT) requireDeploySucceeded(t *testing.T, tx *types.Transaction) {
	t.Helper()
	require.Nil(t, tx.To(), "test setup: tx must be a contract creation")

	receipt := s.requireTxSucceeded(t, tx)
	require.NotEqualf(t, (common.Address{}), receipt.ContractAddress,
		"successful deploy must populate ContractAddress")

	code, err := s.client.CodeAt(s.ctx, receipt.ContractAddress, nil)
	require.NoErrorf(t, err, "CodeAt(%s)", receipt.ContractAddress)
	require.NotEmptyf(t, code, "successful deploy at %s must leave code on chain", receipt.ContractAddress)
}

// requireDeployFailed fetches `tx`'s receipt and asserts a failed contract
// creation (status=0; no code at the derived deploy address).
//
// `CanCreateContract` returns its error as `vmerr` inside the EVM, which
// surfaces as a failed receipt rather than excluding the tx from the block.
// See the design note on [hook.Points.CanExecuteTransaction].
func (s *SUT) requireDeployFailed(t *testing.T, tx *types.Transaction) {
	t.Helper()
	require.Nil(t, tx.To(), "test setup: tx must be a contract creation")

	receipt := s.requireTxFailed(t, tx)
	// receipt.ContractAddress is still derived from (sender, nonce) even
	// for failed deploys; assert the address has no code.
	code, err := s.client.CodeAt(s.ctx, receipt.ContractAddress, nil)
	require.NoErrorf(t, err, "CodeAt(%s)", receipt.ContractAddress)
	require.Emptyf(t, code, "failed deploy must leave no code at %s", receipt.ContractAddress)
}

// postHeliconStartTime returns a deterministic clock anchor at
// `HeliconTimestamp + Tau`, suitable for tests that need (a) Helicon and
// all preceding upgrades to be active in the worst-case state and (b) at
// least one settlement window of headroom for subsequent
// `sut.advanceTime` arithmetic.
func postHeliconStartTime(t *testing.T) time.Time {
	t.Helper()

	networkUpgrades := extras.GetNetworkUpgrades(upgradetest.GetConfig(upgradetest.Helicon))
	require.NotNil(t, networkUpgrades.HeliconTimestamp)
	return time.Unix(int64(*networkUpgrades.HeliconTimestamp), 0).Add(saeparams.Tau)
}

// TestStateUpgradeAppliedAtActivationSAE exercises the `StateUpgrades` arm of
// `BeforeExecutingBlock` -> `subnetevmcore.ApplyUpgrades`.
//
// It schedules a single `extras.StateUpgrade` at `now + Tau` that:
//   - bumps a known account's balance, and
//   - writes a deterministic value to one of its storage slots.
//
// The deterministic clock is then advanced past activation; the next block
// fires `BeforeExecutingBlock`, applies the upgrade, and we assert both
// effects via `client.BalanceAt` / `client.StorageAt` at `LatestBlockNumber`.
func TestStateUpgradeAppliedAtActivationSAE(t *testing.T) {
	const (
		fromIdx = 0
		toIdx   = 1
	)

	var (
		now            = time.Unix(saeparams.TauSeconds, 0).Add(saeparams.Tau)
		activationTime = now.Add(saeparams.Tau)
		activationTS   = uint64(activationTime.Unix())

		// `target` is intentionally NOT one of the funded keychain accounts;
		// it starts with zero balance and empty storage, so the upgrade's
		// effects are unambiguous (no overflow risk, no interference from
		// the trigger tx).
		target       = common.HexToAddress("0x00000000000000000000000000000000DeadBeef")
		balanceBump  = big.NewInt(123_456_789)
		storageSlot  = common.HexToHash("0x01")
		storageValue = common.HexToHash("0xcafe")
	)

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(now),
		withUpgradeConfig(func(_ []common.Address) []byte {
			return mustMarshalJSON(t, &extras.UpgradeConfig{
				StateUpgrades: []extras.StateUpgrade{
					{
						BlockTimestamp: &activationTS,
						StateUpgradeAccounts: map[common.Address]extras.StateUpgradeAccount{
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

	// Pre-activation: target is empty (no balance, no storage).
	preBalance, err := sut.client.BalanceAt(sut.ctx, target, nil)
	require.NoError(t, err)
	require.Zero(t, preBalance.Sign(), "target balance must be 0 before activation")

	preStorage, err := sut.client.StorageAt(sut.ctx, target, storageSlot, nil)
	require.NoError(t, err)
	require.Equal(t, common.Hash{}.Bytes(), preStorage, "storage slot must be empty before activation")

	// Activation block: SAE block builders only fire on `PendingTxs`, so we
	// piggy-back the activation on a trivial keychain-internal transfer.
	// `BeforeExecutingBlock` runs the StateUpgrade BEFORE the tx executes,
	// but `target` is not the tx recipient, so the upgrade's effects are
	// observable in isolation.
	sut.setTime(t, activationTime)
	_ = sut.sendTransferTx(t, fromIdx, toIdx, common.Big1)
	_ = sut.buildAndAcceptBlock(t)

	// Post-activation: balance + storage reflect the StateUpgrade exactly.
	postBalance, err := sut.client.BalanceAt(sut.ctx, target, nil)
	require.NoError(t, err)
	require.Zerof(t, postBalance.Cmp(balanceBump),
		"target balance must reflect StateUpgrade balance bump (want=%s got=%s)", balanceBump, postBalance)

	postStorage, err := sut.client.StorageAt(sut.ctx, target, storageSlot, nil)
	require.NoError(t, err)
	require.Equal(t, storageValue.Bytes(), postStorage, "storage slot must reflect the StateUpgrade")
}

func mustMarshalJSON(t *testing.T, v interface{}) []byte {
	t.Helper()

	bytes, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes
}

func newSnowCtx(t *testing.T, upgrades upgrade.Config) (*snow.Context, []*localsigner.LocalSigner) {
	t.Helper()

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.NetworkUpgrades = upgrades
	validatorState, validatorKeys := newValidatorState(snowCtx.SubnetID)
	snowCtx.ValidatorState = validatorState
	return snowCtx, validatorKeys
}

func newValidatorState(subnetID ids.ID) (*validatorstest.State, []*localsigner.LocalSigner) {
	const (
		numValidators      = 2
		weightPerValidator = 50
	)

	secretKeys := make([]*localsigner.LocalSigner, numValidators)
	nodeIDs := make([]ids.NodeID, numValidators)
	for i := range numValidators {
		key, _ := localsigner.New() // Uses rand, never returns error
		secretKeys[i] = key
		nodeIDs[i] = ids.GenerateTestNodeID()
	}

	return &validatorstest.State{
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		},
		GetMinimumHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
			return subnetID, nil
		},
		// Default to an empty current validator set so that tests which
		// don't care about uptime tracking still drive `vm.SetState(NormalOp)`
		// successfully (the uptime tracker calls
		// `GetCurrentValidatorSet` during its first `Sync`). The
		// uptime-specific tests overwrite this via [withCurrentValidatorSet].
		GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*validators.GetCurrentValidatorOutput{}, 0, nil
		},
		GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
			warpValidators := make([]*validators.Warp, numValidators)
			for i := range numValidators {
				warpValidators[i] = &validators.Warp{
					PublicKey:      secretKeys[i].PublicKey(),
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(secretKeys[i].PublicKey()),
					Weight:         50,
					NodeIDs:        []ids.NodeID{nodeIDs[i]},
				}
			}
			validatorSet := validators.WarpSet{
				Validators:  warpValidators,
				TotalWeight: weightPerValidator * numValidators,
			}
			utils.Sort(validatorSet.Validators)

			return map[ids.ID]validators.WarpSet{
				subnetID: validatorSet,
			}, nil
		},
	}, secretKeys
}
