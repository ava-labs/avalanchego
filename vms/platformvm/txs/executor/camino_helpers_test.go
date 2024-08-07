// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	testState "github.com/ava-labs/avalanchego/vms/platformvm/state/test"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/test"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
)

var (
	testCaminoSubnet1ControlKeys = test.FundedKeys[0:3]

	_ state.Versions = (*caminoEnvironment)(nil)
)

func newCaminoEnvironment(t *testing.T, phase test.Phase, caminoGenesisConf api.Camino) *caminoEnvironment {
	t.Helper()

	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	baseDB := versiondb.New(memdb.New())
	config := test.Config(t, phase)
	clk := test.Clock()
	ctx := test.ContextWithSharedMemory(t, baseDB)
	fx := test.Fx(t, clk, ctx.Log, isBootstrapped.Get())
	rewards := reward.NewCalculator(config.RewardConfig)

	genesisBytes := test.Genesis(t, ctx.AVAXAssetID, caminoGenesisConf, nil)
	baseState := testState.State(t, config, ctx, baseDB, rewards, genesisBytes)

	atomicUTXOs := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	uptimes := uptime.NewManager(baseState)
	utxoHandler := utxo.NewCaminoHandler(ctx, clk, fx, true)

	txBuilder := builder.NewCamino(
		ctx,
		config,
		clk,
		fx,
		baseState,
		atomicUTXOs,
		utxoHandler,
	)

	backend := Backend{
		Config:       config,
		Ctx:          ctx,
		Clk:          clk,
		Bootstrapped: &isBootstrapped,
		Fx:           fx,
		FlowChecker:  utxoHandler,
		Uptimes:      uptimes,
		Rewards:      rewards,
	}

	env := &caminoEnvironment{
		isBootstrapped: &isBootstrapped,
		config:         config,
		clk:            clk,
		baseDB:         baseDB,
		ctx:            ctx,
		fx:             fx,
		state:          baseState,
		states:         make(map[ids.ID]state.Chain),
		atomicUTXOs:    atomicUTXOs,
		uptimes:        uptimes,
		utxosHandler:   utxoHandler,
		txBuilder:      txBuilder,
		backend:        backend,
	}

	t.Cleanup(func() {
		if env.isBootstrapped.Get() {
			primaryValidatorSet, exist := env.config.Validators.Get(constants.PrimaryNetworkID)
			if !exist {
				require.FailNow(t, errMissingPrimaryValidators.Error())
			}
			primaryValidators := primaryValidatorSet.List()

			validatorIDs := make([]ids.NodeID, len(primaryValidators))
			for i, vdr := range primaryValidators {
				validatorIDs[i] = vdr.NodeID
			}
			require.NoError(t, env.uptimes.StopTracking(validatorIDs, constants.PrimaryNetworkID))

			for subnetID := range env.config.TrackedSubnets {
				vdrs, exist := env.config.Validators.Get(subnetID)
				if !exist {
					return
				}
				validators := vdrs.List()

				validatorIDs := make([]ids.NodeID, len(validators))
				for i, vdr := range validators {
					validatorIDs[i] = vdr.NodeID
				}
				require.NoError(t, env.uptimes.StopTracking(validatorIDs, subnetID))
			}
			env.state.SetHeight( /*height*/ math.MaxUint64)
			require.NoError(t, env.state.Commit())
		}

		require.NoError(t, env.state.Close())
		require.NoError(t, env.baseDB.Close())
	})

	return env
}

type caminoEnvironment struct {
	isBootstrapped *utils.Atomic[bool]
	config         *config.Config
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *snow.Context
	fx             fx.Fx
	state          state.State
	states         map[ids.ID]state.Chain
	atomicUTXOs    avax.AtomicUTXOManager
	uptimes        uptime.Manager
	utxosHandler   utxo.Handler
	txBuilder      builder.CaminoBuilder
	backend        Backend
}

func (e *caminoEnvironment) GetState(blkID ids.ID) (state.Chain, bool) {
	if blkID == lastAcceptedID {
		return e.state, true
	}
	chainState, ok := e.states[blkID]
	return chainState, ok
}

func (e *caminoEnvironment) addCaminoSubnet(t *testing.T) {
	t.Helper()

	// Create a subnet
	var err error
	testSubnet1, err = e.txBuilder.NewCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		[]ids.ShortID{ // control keys
			test.FundedKeys[0].Address(),
			test.FundedKeys[1].Address(),
			test.FundedKeys[2].Address(),
		},
		[]*secp256k1.PrivateKey{test.FundedKeys[0]},
		test.FundedKeys[0].Address(),
	)
	require.NoError(t, err)

	// store it
	stateDiff, err := state.NewDiff(lastAcceptedID, e)
	require.NoError(t, err)

	executor := CaminoStandardTxExecutor{
		StandardTxExecutor{
			Backend: &e.backend,
			State:   stateDiff,
			Tx:      testSubnet1,
		},
	}
	require.NoError(t, testSubnet1.Unsigned.Visit(&executor))
	stateDiff.AddTx(testSubnet1, status.Committed)
	require.NoError(t, stateDiff.Apply(e.state))
}

func newExecutorBackend(
	t *testing.T,
	caminoGenesisConf api.Camino,
	phase test.Phase,
	sharedMemory atomic.SharedMemory,
) *Backend {
	t.Helper()

	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	baseDB := versiondb.New(memdb.New())
	config := test.Config(t, phase)
	clk := test.ClockWithTime(test.PhaseTime(t, phase, config))
	ctx := test.ContextWithSharedMemory(t, baseDB)
	fx := test.Fx(t, clk, ctx.Log, isBootstrapped.Get())
	rewards := reward.NewCalculator(config.RewardConfig)

	genesisBytes := test.Genesis(t, ctx.AVAXAssetID, caminoGenesisConf, nil)
	state := testState.State(t, config, ctx, baseDB, rewards, genesisBytes)

	if sharedMemory != nil {
		ctx.SharedMemory = &mutableSharedMemory{
			SharedMemory: sharedMemory,
		}
	}

	uptimes := uptime.NewManager(state)
	utxoHandler := utxo.NewCaminoHandler(ctx, clk, fx, true)

	backend := Backend{
		Config:       config,
		Ctx:          ctx,
		Clk:          clk,
		Bootstrapped: &isBootstrapped,
		Fx:           fx,
		FlowChecker:  utxoHandler,
		Uptimes:      uptimes,
		Rewards:      rewards,
	}

	t.Cleanup(func() {
		if backend.Bootstrapped.Get() {
			primaryValidatorSet, exist := backend.Config.Validators.Get(constants.PrimaryNetworkID)
			if !exist {
				require.FailNow(t, errMissingPrimaryValidators.Error())
			}
			primaryValidators := primaryValidatorSet.List()

			validatorIDs := make([]ids.NodeID, len(primaryValidators))
			for i, vdr := range primaryValidators {
				validatorIDs[i] = vdr.NodeID
			}
			require.NoError(t, backend.Uptimes.StopTracking(validatorIDs, constants.PrimaryNetworkID))

			for subnetID := range backend.Config.TrackedSubnets {
				vdrs, exist := backend.Config.Validators.Get(subnetID)
				if !exist {
					return
				}
				validators := vdrs.List()

				validatorIDs := make([]ids.NodeID, len(validators))
				for i, vdr := range validators {
					validatorIDs[i] = vdr.NodeID
				}
				require.NoError(t, backend.Uptimes.StopTracking(validatorIDs, subnetID))
			}
			state.SetHeight( /*height*/ math.MaxUint64)
			require.NoError(t, state.Commit())
		}

		require.NoError(t, state.Close())
		require.NoError(t, baseDB.Close())
	})

	return &backend
}
