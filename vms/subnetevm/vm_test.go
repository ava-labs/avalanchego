// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
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

	engcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

type SUT struct {
	ctx     context.Context
	snowCtx *snow.Context
	vm      *VM
	client  *ethclient.Client

	// Wallet for issuing transactions
	ethWallet     *saetest.Wallet
	validatorKeys []*localsigner.LocalSigner

	// See [SUT.verifyWarpMessage]
	appResponse chan []byte
	appErr      chan *engcommon.AppError

	now *time.Time
}

type (
	sutConfig struct {
		fork             upgradetest.Fork
		numAccounts      uint
		now              *time.Time
		configureGenesis func(*core.Genesis, []common.Address)
		configureUpgrade func([]common.Address) []byte
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

	saeConfig := sae.Config{
		MempoolConfig: mempoolConf,
		DBConfig: saedb.Config{
			TrieDBConfig: triedb.HashDefaults,
		},
	}
	if cfg.now != nil {
		saeConfig.Now = func() time.Time {
			return *cfg.now
		}
	}
	vm := New(saeConfig)

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
		nil,
		nil,
		appSender,
	))
	t.Cleanup(func() {
		require.NoError(t, vm.Shutdown(context.WithoutCancel(ctx)))
	})

	require.NoError(t, vm.SetState(ctx, snow.NormalOp))

	handlers, err := vm.CreateHandlers(ctx)
	require.NoError(t, err)
	server := httptest.NewServer(handlers["/ws"])
	t.Cleanup(server.Close)

	uri := server.Listener.Addr().String()
	rpcClient, err := rpc.Dial("ws://" + uri)
	require.NoError(t, err)
	t.Cleanup(rpcClient.Close)

	client := ethclient.NewClient(rpcClient)

	// TODO(alarso16): delete this - it should be on the VM
	lastID, err := vm.LastAccepted(ctx)
	require.NoError(t, err)
	require.NoError(t, vm.SetPreference(ctx, lastID, nil))

	return &SUT{
		ctx:     ctx,
		snowCtx: snowCtx,
		vm:      vm,
		client:  client,
		ethWallet: saetest.NewWalletWithKeyChain(
			keychain,
			types.LatestSigner(genesis.Config),
		),
		validatorKeys: validatorKeys,
		appResponse:   appResponseCh,
		appErr:        appErrCh,
		now:           cfg.now,
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

func withNow(now *time.Time) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.now = now
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

func (s *SUT) setTime(t *testing.T, now time.Time) {
	t.Helper()
	require.NotNil(t, s.now)
	*s.now = now
}

func (s *SUT) advanceTime(t *testing.T, d time.Duration) {
	t.Helper()
	require.NotNil(t, s.now)
	*s.now = s.now.Add(d)
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

func (s *SUT) fetchTxAllowListRole(t *testing.T, address common.Address, blockNumber rpc.BlockNumber) allowlist.Role {
	t.Helper()

	stateDB, _, err := s.vm.GethRPCBackends().StateAndHeaderByNumber(s.ctx, blockNumber)
	require.NoError(t, err)
	return txallowlist.GetTxAllowListStatus(stateDB, address)
}

func (s *SUT) isPrecompileEnabledAtLatest(precompile common.Address) bool {
	chainConfig := s.vm.GethRPCBackends().ChainConfig()
	timestamp := uint64(s.now.Unix())
	return subnetevmparams.GetExtra(chainConfig).IsPrecompileEnabled(precompile, timestamp)
}

// TestTxAllowListPrecompileUpgradesSAE exercises mid-chain `PrecompileUpgrades`
// for the `txallowlist` precompile under SAE end-to-end through a single
// timeline: genesis-enabled (admin only) -> disable -> re-enable (admin +
// manager). At each step we assert all three properties of interest:
//  1. The precompile is correctly enabled / disabled at the scheduled
//     timestamp (via `BeforeExecutingBlock` -> `core.ApplyUpgrades`).
//  2. The configuration is correctly applied to state (admin / manager
//     roles observable via `txallowlist.GetTxAllowListStatus`).
//  3. Worst-case admission correctly blocks transfers from non-enabled
//     roles against the last-settled state, and unblocks them once the
//     allowlist is removed.
func TestTxAllowListPrecompileUpgradesSAE(t *testing.T) {
	const (
		adminIdx    = 0
		nonAdminIdx = 1
	)

	now := txAllowListTestStartTime(t)
	disableTime := now.Add(saeparams.Tau)
	// Re-enable after the disable activation has had time to settle, so the
	// timeline only ever moves forward during the test.
	reenableTime := disableTime.Add(2 * saeparams.Tau)

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(&now),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			subnetevmparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				txallowlist.ConfigKey: txallowlist.NewConfig(
					utils.PointerTo[uint64](0),
					[]common.Address{addresses[adminIdx]},
					nil,
					nil,
				),
			}
		}),
		withUpgradeConfig(func(addresses []common.Address) []byte {
			return mustMarshalJSON(t, &extras.UpgradeConfig{
				PrecompileUpgrades: []extras.PrecompileUpgrade{
					{
						Config: txallowlist.NewDisableConfig(utils.PointerTo(uint64(disableTime.Unix()))),
					},
					{
						// Re-enable promotes `nonAdminIdx` to manager so we
						// can observe the new config land in state.
						Config: txallowlist.NewConfig(
							utils.PointerTo(uint64(reenableTime.Unix())),
							[]common.Address{addresses[adminIdx]},
							nil,
							[]common.Address{addresses[nonAdminIdx]},
						),
					},
				},
			})
		}),
	)

	addresses := sut.ethWallet.Addresses()
	admin := addresses[adminIdx]
	nonAdmin := addresses[nonAdminIdx]
	fundValue := new(big.Int).Mul(big.NewInt(subnetevmparams.Ether), big.NewInt(1000))

	// Step 0: at genesis, the allowlist is enabled with `admin` as the only
	// allow-listed sender. Latest and finalized agree (no upgrades yet).
	// (1) Precompile is enabled per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(txallowlist.ContractAddress), "txallowlist must be enabled at genesis")
	// (2) Genesis config is applied to state in both latest & finalized.
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Mempool ingress (last-executed) rejects the non-admin sender at
	// RPC; worst-case admission (last-settled) excludes it from blocks.
	// Fund `nonAdmin` so it can pay fees later.
	fundNonAdmin := sut.sendTransferTx(t, adminIdx, nonAdminIdx, fundValue)
	droppedTx := sut.signTransferTx(t, nonAdminIdx, adminIdx, common.Big1)
	// JSON-RPC stringifies the error, so the sentinel chain is lost; match
	// on its message instead. See e.g. txallowlist/simulated_test.go.
	require.ErrorContains(t, //nolint:forbidigo // upstream error wrapped as string
		sut.client.SendTransaction(sut.ctx, droppedTx),
		vmerrors.ErrSenderAddressNotAllowListed.Error(),
		"non-admin tx must be rejected at mempool ingress while allowlist is enabled",
	)
	block := sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1, "non-admin tx must be excluded while allowlist is enabled")
	require.Equal(t, fundNonAdmin.Hash(), block.Transactions()[0].Hash())

	// Step 1: advance to `disableTime` and produce the activation block.
	sut.setTime(t, disableTime)
	disableActivationTx := sut.sendTransferTx(t, adminIdx, nonAdminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, disableActivationTx.Hash(), block.Transactions()[0].Hash())

	// (1) Disable upgrade fires inside `BeforeExecutingBlock`; precompile is
	// no longer enabled per chain config at the current timestamp.
	require.False(t, sut.isPrecompileEnabledAtLatest(txallowlist.ContractAddress), "txallowlist must be disabled after activation")
	// (2) State diverges between latest and finalized: latest reflects the
	// disable just applied, but finalized still observes the pre-disable
	// admin role because settlement lags by Tau.
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized state must still show admin role until disable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) Ingress (last-executed) accepts `droppedTx` immediately on
	// re-submit; worst-case admission (last-settled) only includes it after
	// the disable activation settles.
	require.NoError(t, sut.client.SendTransaction(sut.ctx, droppedTx),
		"previously-blocked tx must be admitted at mempool ingress once disable executes")
	sut.advanceTime(t, saeparams.Tau+time.Second)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1, "previously-blocked tx must be included once disable settles")
	require.Equal(t, droppedTx.Hash(), block.Transactions()[0].Hash())

	// (2) After settlement, finalized catches up with latest: both observe
	// the disabled allowlist (no roles).
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect disable once it has settled")
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

	// Step 2: advance to `reenableTime` and produce the re-enable activation
	// block.
	sut.setTime(t, reenableTime)
	reenableActivationTx := sut.sendTransferTx(t, adminIdx, nonAdminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, reenableActivationTx.Hash(), block.Transactions()[0].Hash())

	// (1) Re-enable upgrade fires inside `BeforeExecutingBlock`; precompile
	// is enabled again per chain config at the current timestamp.
	require.True(t, sut.isPrecompileEnabledAtLatest(txallowlist.ContractAddress), "txallowlist must be re-enabled after activation")
	// (2) Latest reflects the re-enable (admin + manager promotion); the
	// finalized snapshot is still pre-re-enable (no roles).
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.ManagerRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.LatestBlockNumber))
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized state must still reflect disabled allowlist until re-enable settles")
	require.Equal(t, allowlist.NoRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))

	// (3) `nonAdmin` is now a manager and must be admissible
	sut.advanceTime(t, saeparams.Tau+time.Second)
	managerTx := sut.sendTransferTx(t, nonAdminIdx, adminIdx, common.Big1)
	block = sut.buildAndAcceptBlock(t)
	require.Len(t, block.Transactions(), 1, "manager tx must be admitted after re-enable settles")
	require.Equal(t, managerTx.Hash(), block.Transactions()[0].Hash())

	// (2) Finalized has caught up with latest after re-enable settled.
	require.Equal(t, allowlist.AdminRole, sut.fetchTxAllowListRole(t, admin, rpc.FinalizedBlockNumber),
		"finalized must reflect re-enable once it has settled")
	require.Equal(t, allowlist.ManagerRole, sut.fetchTxAllowListRole(t, nonAdmin, rpc.FinalizedBlockNumber))
}

func txAllowListTestStartTime(t *testing.T) time.Time {
	t.Helper()

	networkUpgrades := extras.GetNetworkUpgrades(upgradetest.GetConfig(upgradetest.Helicon))
	require.NotNil(t, networkUpgrades.HeliconTimestamp)
	return time.Unix(int64(*networkUpgrades.HeliconTimestamp), 0).Add(saeparams.Tau)
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
