// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txstest"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators/validatorstest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/wallet"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

const (
	pending stakerStatus = iota
	current

	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour

	defaultTxFee = 100 * units.NanoAvax
)

var testSubnet1 *txs.Tx

type stakerStatus uint

type staker struct {
	nodeID             ids.NodeID
	rewardAddress      ids.ShortID
	startTime, endTime time.Time
}

type test struct {
	description           string
	stakers               []staker
	subnetStakers         []staker
	advanceTimeTo         []time.Time
	expectedStakers       map[ids.NodeID]stakerStatus
	expectedSubnetStakers map[ids.NodeID]stakerStatus
}

type environment struct {
	blkManager Manager
	mempool    txmempool.Mempool[*txs.Tx]

	isBootstrapped *utils.Atomic[bool]
	config         *config.Internal
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *snow.Context
	fx             fx.Fx
	state          state.State
	mockedState    *state.MockState
	uptimes        uptime.Manager
	utxosVerifier  utxo.Verifier
	backend        *executor.Backend
}

func newEnvironment(t *testing.T, ctrl *gomock.Controller, f upgradetest.Fork) *environment {
	res := &environment{
		isBootstrapped: &utils.Atomic[bool]{},
		config:         defaultConfig(f),
		clk:            defaultClock(),
	}
	res.isBootstrapped.Set(true)

	res.baseDB = versiondb.New(memdb.New())
	atomicDB := prefixdb.New([]byte{1}, res.baseDB)
	m := atomic.NewMemory(atomicDB)

	res.ctx = snowtest.Context(t, snowtest.PChainID)
	res.ctx.SharedMemory = m.NewSharedMemory(res.ctx.ChainID)

	res.fx = defaultFx(res.clk, res.ctx.Log, res.isBootstrapped.Get())

	rewardsCalc := reward.NewCalculator(res.config.RewardConfig)

	if ctrl == nil {
		res.state = statetest.New(t, statetest.Config{
			DB:         res.baseDB,
			Genesis:    genesistest.NewBytes(t, genesistest.Config{}),
			Validators: res.config.Validators,
			Context:    res.ctx,
			Rewards:    rewardsCalc,
		})

		res.uptimes = uptime.NewManager(res.state, res.clk)
		res.utxosVerifier = utxo.NewVerifier(res.ctx, res.clk, res.fx)
	} else {
		res.mockedState = state.NewMockState(ctrl)
		res.uptimes = uptime.NewManager(res.mockedState, res.clk)
		res.utxosVerifier = utxo.NewVerifier(res.ctx, res.clk, res.fx)

		// setup expectations strictly needed for environment creation
		res.mockedState.EXPECT().GetLastAccepted().Return(ids.GenerateTestID()).Times(1)
	}

	res.backend = &executor.Backend{
		Config:       res.config,
		Ctx:          res.ctx,
		Clk:          res.clk,
		Bootstrapped: res.isBootstrapped,
		Fx:           res.fx,
		FlowChecker:  res.utxosVerifier,
		Uptimes:      res.uptimes,
		Rewards:      rewardsCalc,
	}

	registerer := prometheus.NewRegistry()

	metrics := metrics.Noop

	var err error
	res.mempool, err = mempool.New("mempool", registerer)
	if err != nil {
		panic(fmt.Errorf("failed to create mempool: %w", err))
	}

	if ctrl == nil {
		res.blkManager = NewManager(
			res.mempool,
			metrics,
			res.state,
			res.backend,
			validatorstest.Manager,
		)
		addSubnet(t, res)
	} else {
		res.blkManager = NewManager(
			res.mempool,
			metrics,
			res.mockedState,
			res.backend,
			validatorstest.Manager,
		)
		// we do not add any subnet to state, since we can mock
		// whatever we need
	}

	t.Cleanup(func() {
		res.ctx.Lock.Lock()
		defer res.ctx.Lock.Unlock()

		if res.mockedState != nil {
			// state is mocked, nothing to do here
			return
		}

		require := require.New(t)

		if res.uptimes.StartedTracking() {
			validatorIDs := res.config.Validators.GetValidatorIDs(constants.PrimaryNetworkID)

			require.NoError(res.uptimes.StopTracking(validatorIDs))
			require.NoError(res.state.Commit())
		}

		if res.state != nil {
			require.NoError(res.state.Close())
		}

		require.NoError(res.baseDB.Close())
	})

	return res
}

type walletConfig struct {
	keys      []*secp256k1.PrivateKey
	subnetIDs []ids.ID
}

func newWallet(t testing.TB, e *environment, c walletConfig) wallet.Wallet {
	if len(c.keys) == 0 {
		c.keys = genesistest.DefaultFundedKeys
	}
	return txstest.NewWallet(
		t,
		e.ctx,
		e.config,
		e.state,
		secp256k1fx.NewKeychain(c.keys...),
		c.subnetIDs,
		nil, // validationIDs
		[]ids.ID{e.ctx.CChainID, e.ctx.XChainID},
	)
}

func addSubnet(t testing.TB, env *environment) {
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

	genesisID := env.state.GetLastAccepted()
	stateDiff, err := state.NewDiff(genesisID, env.blkManager)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
	_, _, _, err = executor.StandardTx(
		env.backend,
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
	upgrades := upgradetest.GetConfigWithUpgradeTime(f, time.Time{})
	// This package neglects fork ordering
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

func defaultClock() *mockable.Clock {
	clk := &mockable.Clock{}
	clk.Set(genesistest.DefaultValidatorStartTime)
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

func addPendingValidator(
	t testing.TB,
	env *environment,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	keys []*secp256k1.PrivateKey,
) *txs.Tx {
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: keys,
	})

	addValidatorTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   env.config.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	staker, err := state.NewPendingStaker(
		addValidatorTx.ID(),
		addValidatorTx.Unsigned.(*txs.AddValidatorTx),
	)
	require.NoError(err)

	require.NoError(env.state.PutPendingValidator(staker))
	env.state.AddTx(addValidatorTx, status.Committed)
	env.state.SetHeight(1)
	require.NoError(env.state.Commit())
	return addValidatorTx
}
