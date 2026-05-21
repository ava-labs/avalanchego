// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/MetalBlockchain/metalgo/chains"
	"github.com/MetalBlockchain/metalgo/chains/atomic"
	"github.com/MetalBlockchain/metalgo/codec"
	"github.com/MetalBlockchain/metalgo/codec/linearcodec"
	"github.com/MetalBlockchain/metalgo/database/memdb"
	"github.com/MetalBlockchain/metalgo/database/versiondb"
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/snow/snowtest"
	"github.com/MetalBlockchain/metalgo/snow/uptime"
	"github.com/MetalBlockchain/metalgo/snow/validators"
	"github.com/MetalBlockchain/metalgo/upgrade/upgradetest"
	"github.com/MetalBlockchain/metalgo/utils"
	"github.com/MetalBlockchain/metalgo/utils/constants"
	"github.com/MetalBlockchain/metalgo/utils/crypto/secp256k1"
	"github.com/MetalBlockchain/metalgo/utils/logging"
	"github.com/MetalBlockchain/metalgo/utils/timer/mockable"
	"github.com/MetalBlockchain/metalgo/utils/units"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/config"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/fx"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/genesis/genesistest"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/reward"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/state"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/state/statetest"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/status"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/txs"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/txs/txstest"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/utxo"
	"github.com/MetalBlockchain/metalgo/vms/secp256k1fx"
	"github.com/MetalBlockchain/metalgo/wallet/chain/p/wallet"
)

const (
	defaultMinValidatorStake = 5 * units.MilliAvax

	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour

	defaultTxFee = 100 * units.NanoAvax
)

var (
	lastAcceptedID = ids.GenerateTestID()

	testSubnet1 *txs.Tx
)

type mutableSharedMemory struct {
	atomic.SharedMemory
}

type environment struct {
	isBootstrapped *utils.Atomic[bool]
	config         *config.Internal
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *snow.Context
	msm            *mutableSharedMemory
	state          *state.State
	states         map[ids.ID]state.Chain
	uptimes        uptime.Manager
	backend        Backend
}

func (e *environment) GetState(blkID ids.ID) (state.Chain, bool) {
	if blkID == lastAcceptedID {
		return e.state, true
	}
	chainState, ok := e.states[blkID]
	return chainState, ok
}

func (e *environment) SetState(blkID ids.ID, chainState state.Chain) {
	e.states[blkID] = chainState
}

func newEnvironment(t *testing.T, f upgradetest.Fork) *environment {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	config := defaultConfig(f)
	clk := defaultClock(f)

	baseDB := versiondb.New(memdb.New())
	ctx := snowtest.Context(t, snowtest.PChainID)
	m := atomic.NewMemory(baseDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	fx := defaultFx(clk, ctx.Log, isBootstrapped.Get())

	rewards := reward.NewCalculator(config.RewardConfig)
	baseState := statetest.New(t, statetest.Config{
		DB:         baseDB,
		Genesis:    genesistest.NewBytes(t, genesistest.Config{}),
		Validators: config.Validators,
		Upgrades:   config.UpgradeConfig,
		Context:    ctx,
		Rewards:    rewards,
	})
	lastAcceptedID = baseState.GetLastAccepted()

	uptimes := uptime.NewManager(baseState, clk)
	utxosVerifier := utxo.NewVerifier(ctx, clk, fx)

	backend := Backend{
		Config:       config,
		Ctx:          ctx,
		Clk:          clk,
		Bootstrapped: &isBootstrapped,
		Fx:           fx,
		FlowChecker:  utxosVerifier,
		Uptimes:      uptimes,
		Rewards:      rewards,
	}

	env := &environment{
		isBootstrapped: &isBootstrapped,
		config:         config,
		clk:            clk,
		baseDB:         baseDB,
		ctx:            ctx,
		msm:            msm,
		state:          baseState,
		states:         make(map[ids.ID]state.Chain),
		uptimes:        uptimes,
		backend:        backend,
	}

	addSubnet(t, env)

	t.Cleanup(func() {
		env.ctx.Lock.Lock()
		defer env.ctx.Lock.Unlock()

		require := require.New(t)

		if env.isBootstrapped.Get() {
			if env.uptimes.StartedTracking() {
				validatorIDs := env.config.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
				require.NoError(env.uptimes.StopTracking(validatorIDs))
			}

			env.state.SetHeight(math.MaxUint64)
			require.NoError(env.state.Commit())
		}

		require.NoError(env.state.Close())
		require.NoError(env.baseDB.Close())
	})

	return env
}

type walletConfig struct {
	config    *config.Internal
	keys      []*secp256k1.PrivateKey
	subnetIDs []ids.ID
	chainIDs  []ids.ID
}

func newWallet(t testing.TB, e *environment, c walletConfig) wallet.Wallet {
	if c.config == nil {
		c.config = e.config
	}
	if len(c.keys) == 0 {
		c.keys = genesistest.DefaultFundedKeys
	}
	return txstest.NewWallet(
		t,
		e.ctx,
		c.config,
		e.state,
		secp256k1fx.NewKeychain(c.keys...),
		c.subnetIDs,
		nil, // validationIDs
		c.chainIDs,
	)
}

func addSubnet(t *testing.T, env *environment) {
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: genesistest.DefaultFundedKeys[:1],
	})

	var err error
	testSubnet1, err = wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 2,
			Addrs: []ids.ShortID{
				genesistest.DefaultFundedKeys[0].Address(),
				genesistest.DefaultFundedKeys[1].Address(),
				genesistest.DefaultFundedKeys[2].Address(),
			},
		},
	)
	require.NoError(err)

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		testSubnet1,
		stateDiff,
	)
	require.NoError(err)

	stateDiff.AddTx(testSubnet1, status.Committed)
	require.NoError(stateDiff.Apply(env.state))
	require.NoError(env.state.Commit())
}

func defaultConfig(f upgradetest.Fork) *config.Internal {
	upgrades := upgradetest.GetConfigWithUpgradeTime(
		f,
		genesistest.DefaultValidatorStartTime.Add(-2*time.Second),
	)
	upgradetest.SetTimesTo(
		&upgrades,
		min(f, upgradetest.ApricotPhase5),
		genesistest.DefaultValidatorEndTime,
	)

	return &config.Internal{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		Validators:             validators.NewManager(),
		MinValidatorStake:      5 * units.MilliAvax,
		MaxValidatorStake:      500 * units.MilliAvax,
		MinDelegatorStake:      1 * units.MilliAvax,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig: reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .10 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		},
		UpgradeConfig: upgrades,
	}
}

func defaultClock(f upgradetest.Fork) *mockable.Clock {
	now := genesistest.DefaultValidatorStartTime
	if f >= upgradetest.Banff {
		// 1 second after active fork
		now = genesistest.DefaultValidatorEndTime.Add(-2 * time.Second)
	}
	clk := &mockable.Clock{}
	clk.Set(now)
	return clk
}

type fxVMInt struct {
	registry codec.Registry
	clk      *mockable.Clock
	log      logging.Logger
}

func (fvi *fxVMInt) CodecRegistry() codec.Registry {
	return fvi.registry
}

func (fvi *fxVMInt) Clock() *mockable.Clock {
	return fvi.clk
}

func (fvi *fxVMInt) Logger() logging.Logger {
	return fvi.log
}

func defaultFx(clk *mockable.Clock, log logging.Logger, isBootstrapped bool) fx.Fx {
	fxVMInt := &fxVMInt{
		registry: linearcodec.NewDefault(),
		clk:      clk,
		log:      log,
	}
	res := &secp256k1fx.Fx{}
	if err := res.Initialize(fxVMInt); err != nil {
		panic(err)
	}
	if isBootstrapped {
		if err := res.Bootstrapped(); err != nil {
			panic(err)
		}
	}
	return res
}
