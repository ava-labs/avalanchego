// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/config/configtest"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	lastAcceptedID = ids.GenerateTestID()

	testSubnet1 *txs.Tx
)

type environment struct {
	isBootstrapped *utils.Atomic[bool]
	config         *config.Config
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *snow.Context
	msm            *configtest.MutableSharedMemory
	fx             fx.Fx
	state          state.State
	states         map[ids.ID]state.Chain
	atomicUTXOs    avax.AtomicUTXOManager
	uptimes        uptime.Manager
	utxosHandler   utxo.Handler
	txBuilder      builder.Builder
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

func newEnvironment(t *testing.T, postBanff, postCortina, postDurango bool) *environment {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	var (
		fork     configtest.ActiveFork
		forkTime time.Time
	)
	switch {
	case postDurango:
		fork = configtest.DurangoFork
		forkTime = genesistest.ValidateStartTime.Add(-2 * time.Second).Add(-2 * time.Second)
	case postCortina:
		fork = configtest.CortinaFork
		forkTime = genesistest.ValidateStartTime.Add(-2 * time.Second).Add(-2 * time.Second)
	case postBanff:
		fork = configtest.BanffFork
		forkTime = genesistest.ValidateEndTime.Add(-2 * time.Second)
	default:
		fork = configtest.ApricotPhase5Fork
		forkTime = genesistest.GenesisTime
	}

	config := configtest.Config(fork, forkTime)
	clk := defaultClock(forkTime)

	baseDB := versiondb.New(memdb.New())
	ctx, msm := configtest.Context(t, baseDB)

	fx := defaultFx(clk, ctx.Log, isBootstrapped.Get())

	rewards := reward.NewCalculator(config.RewardConfig)
	baseState := defaultState(t, config, ctx, baseDB, rewards)

	atomicUTXOs := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	uptimes := uptime.NewManager(baseState, clk)
	utxoHandler := utxo.NewHandler(ctx, clk, fx)

	txBuilder := builder.New(
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

	env := &environment{
		isBootstrapped: &isBootstrapped,
		config:         config,
		clk:            clk,
		baseDB:         baseDB,
		ctx:            ctx,
		msm:            msm,
		fx:             fx,
		state:          baseState,
		states:         make(map[ids.ID]state.Chain),
		atomicUTXOs:    atomicUTXOs,
		uptimes:        uptimes,
		utxosHandler:   utxoHandler,
		txBuilder:      txBuilder,
		backend:        backend,
	}

	addSubnet(t, env, txBuilder)

	return env
}

func addSubnet(
	t *testing.T,
	env *environment,
	txBuilder builder.Builder,
) {
	require := require.New(t)

	// Create a subnet
	var err error
	testSubnet1, err = txBuilder.NewCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		[]ids.ShortID{ // control keys
			genesistest.SubnetControlKeys[0].PublicKey().Address(),
			genesistest.SubnetControlKeys[1].PublicKey().Address(),
			genesistest.SubnetControlKeys[2].PublicKey().Address(),
		},
		[]*secp256k1.PrivateKey{genesistest.Keys[4]},
		genesistest.Keys[0].PublicKey().Address(),
	)
	require.NoError(err)

	// store it
	stateDiff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := StandardTxExecutor{
		Backend: &env.backend,
		State:   stateDiff,
		Tx:      testSubnet1,
	}
	require.NoError(testSubnet1.Unsigned.Visit(&executor))

	stateDiff.AddTx(testSubnet1, status.Committed)
	require.NoError(stateDiff.Apply(env.state))
	require.NoError(env.state.Commit())
}

func defaultState(
	t *testing.T,
	cfg *config.Config,
	ctx *snow.Context,
	db database.Database,
	rewards reward.Calculator,
) state.State {
	_, genesisBytes := genesistest.Genesis(t, ctx)

	execCfg, _ := config.GetExecutionConfig(nil)
	state, err := state.New(
		db,
		genesisBytes,
		prometheus.NewRegistry(),
		cfg,
		execCfg,
		ctx,
		metrics.Noop,
		rewards,
	)
	if err != nil {
		panic(err)
	}

	// persist and reload to init a bunch of in-memory stuff
	state.SetHeight(0)
	if err := state.Commit(); err != nil {
		panic(err)
	}
	lastAcceptedID = state.GetLastAccepted()
	return state
}

func defaultClock(forkTime time.Time) *mockable.Clock {
	clk := &mockable.Clock{}
	clk.Set(forkTime)
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
		registry: linearcodec.NewDefault(time.Time{}),
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

func shutdownEnvironment(env *environment) error {
	if env.isBootstrapped.Get() {
		validatorIDs := env.config.Validators.GetValidatorIDs(constants.PrimaryNetworkID)

		if err := env.uptimes.StopTracking(validatorIDs, constants.PrimaryNetworkID); err != nil {
			return err
		}

		for subnetID := range env.config.TrackedSubnets {
			validatorIDs := env.config.Validators.GetValidatorIDs(subnetID)

			if err := env.uptimes.StopTracking(validatorIDs, subnetID); err != nil {
				return err
			}
		}
		env.state.SetHeight( /*height*/ math.MaxUint64)
		if err := env.state.Commit(); err != nil {
			return err
		}
	}

	return utils.Err(
		env.state.Close(),
		env.baseDB.Close(),
	)
}
