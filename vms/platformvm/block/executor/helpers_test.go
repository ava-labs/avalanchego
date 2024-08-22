// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
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
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txstest"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	pvalidators "github.com/ava-labs/avalanchego/vms/platformvm/validators"
	walletsigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	walletcommon "github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
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
	mempool    mempool.Mempool
	sender     *enginetest.Sender

	isBootstrapped *utils.Atomic[bool]
	config         *config.Config
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *snow.Context
	fx             fx.Fx
	state          state.State
	mockedState    *state.MockState
	uptimes        uptime.Manager
	utxosVerifier  utxo.Verifier
	factory        *txstest.WalletFactory
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
		res.factory = txstest.NewWalletFactory(
			res.ctx,
			res.config,
			res.state,
		)
	} else {
		res.mockedState = state.NewMockState(ctrl)
		res.uptimes = uptime.NewManager(res.mockedState, res.clk)
		res.utxosVerifier = utxo.NewVerifier(res.ctx, res.clk, res.fx)
		res.factory = txstest.NewWalletFactory(
			res.ctx,
			res.config,
			res.mockedState,
		)

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
	res.sender = &enginetest.Sender{T: t}

	metrics := metrics.Noop

	var err error
	res.mempool, err = mempool.New("mempool", registerer, nil)
	if err != nil {
		panic(fmt.Errorf("failed to create mempool: %w", err))
	}

	if ctrl == nil {
		res.blkManager = NewManager(
			res.mempool,
			metrics,
			res.state,
			res.backend,
			pvalidators.TestManager,
		)
		addSubnet(res)
	} else {
		res.blkManager = NewManager(
			res.mempool,
			metrics,
			res.mockedState,
			res.backend,
			pvalidators.TestManager,
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

		if res.isBootstrapped.Get() {
			validatorIDs := res.config.Validators.GetValidatorIDs(constants.PrimaryNetworkID)

			require.NoError(res.uptimes.StopTracking(validatorIDs, constants.PrimaryNetworkID))
			require.NoError(res.state.Commit())
		}

		if res.state != nil {
			require.NoError(res.state.Close())
		}

		require.NoError(res.baseDB.Close())
	})

	return res
}

func addSubnet(env *environment) {
	builder, signer := env.factory.NewWallet(genesistest.DefaultFundedKeys[0])
	utx, err := builder.NewCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 2,
			Addrs: []ids.ShortID{
				genesistest.DefaultFundedKeys[0].Address(),
				genesistest.DefaultFundedKeys[1].Address(),
				genesistest.DefaultFundedKeys[2].Address(),
			},
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
		}),
	)
	if err != nil {
		panic(err)
	}
	testSubnet1, err = walletsigner.SignUnsigned(context.Background(), signer, utx)
	if err != nil {
		panic(err)
	}

	genesisID := env.state.GetLastAccepted()
	stateDiff, err := state.NewDiff(genesisID, env.blkManager)
	if err != nil {
		panic(err)
	}
	feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
	executor := executor.StandardTxExecutor{
		Backend:       env.backend,
		State:         stateDiff,
		FeeCalculator: feeCalculator,
		Tx:            testSubnet1,
	}
	err = testSubnet1.Unsigned.Visit(&executor)
	if err != nil {
		panic(err)
	}

	stateDiff.AddTx(testSubnet1, status.Committed)
	if err := stateDiff.Apply(env.state); err != nil {
		panic(err)
	}
	if err := env.state.Commit(); err != nil {
		panic(err)
	}
}

func defaultConfig(f upgradetest.Fork) *config.Config {
	upgrades := upgradetest.GetConfigWithUpgradeTime(f, time.Time{})
	// This package neglects fork ordering
	upgradetest.SetTimesTo(
		&upgrades,
		min(f, upgradetest.ApricotPhase5),
		genesistest.DefaultValidatorEndTime,
	)

	return &config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		Validators:             validators.NewManager(),
		StaticFeeConfig: fee.StaticConfig{
			TxFee:                 defaultTxFee,
			CreateSubnetTxFee:     100 * defaultTxFee,
			CreateBlockchainTxFee: 100 * defaultTxFee,
		},
		MinValidatorStake: 5 * units.MilliAvax,
		MaxValidatorStake: 500 * units.MilliAvax,
		MinDelegatorStake: 1 * units.MilliAvax,
		MinStakeDuration:  defaultMinStakingDuration,
		MaxStakeDuration:  defaultMaxStakingDuration,
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
	clk.Set(genesistest.DefaultTime)
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
	env *environment,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	keys []*secp256k1.PrivateKey,
) (*txs.Tx, error) {
	builder, signer := env.factory.NewWallet(keys...)
	utx, err := builder.NewAddValidatorTx(
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
	if err != nil {
		return nil, err
	}
	addPendingValidatorTx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
	if err != nil {
		return nil, err
	}

	staker, err := state.NewPendingStaker(
		addPendingValidatorTx.ID(),
		addPendingValidatorTx.Unsigned.(*txs.AddValidatorTx),
	)
	if err != nil {
		return nil, err
	}

	env.state.PutPendingValidator(staker)
	env.state.AddTx(addPendingValidatorTx, status.Committed)
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	if err := env.state.Commit(); err != nil {
		return nil, err
	}
	return addPendingValidatorTx, nil
}
