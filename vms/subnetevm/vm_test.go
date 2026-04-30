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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/paramstest"
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
