// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txheap"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	defaultWeight = 5 * units.MilliAvax
	trackChecksum = false
)

var (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour
	defaultGenesisTime        = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultValidateStartTime  = defaultGenesisTime
	defaultValidateEndTime    = defaultValidateStartTime.Add(20 * defaultMinStakingDuration)
	defaultMinValidatorStake  = 5 * units.MilliAvax
	defaultBalance            = 100 * defaultMinValidatorStake
	preFundedKeys             = secp256k1.TestKeys()
	avaxAssetID               = ids.ID{'y', 'e', 'e', 't'}
	defaultTxFee              = uint64(100)
	xChainID                  = ids.Empty.Prefix(0)
	cChainID                  = ids.Empty.Prefix(1)
	lastAcceptedID            = ids.GenerateTestID()

	testSubnet1            *txs.Tx
	testSubnet1ControlKeys = preFundedKeys[0:3]

	errMissing = errors.New("missing")
)

type mutableSharedMemory struct {
	atomic.SharedMemory
}

type environment struct {
	isBootstrapped *utils.Atomic[bool]
	config         *config.Config
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *snow.Context
	msm            *mutableSharedMemory
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

func newEnvironment(t *testing.T, postBanff, postCortina bool) *environment {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	config := defaultConfig(postBanff, postCortina)
	clk := defaultClock(postBanff || postCortina)

	baseDB := versiondb.New(memdb.New())
	ctx, msm := defaultCtx(baseDB)

	fx := defaultFx(clk, ctx.Log, isBootstrapped.Get())

	rewards := reward.NewCalculator(config.RewardConfig)
	baseState := defaultState(&config, ctx, baseDB, rewards)

	atomicUTXOs := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	uptimes := uptime.NewManager(baseState, clk)
	utxoHandler := utxo.NewHandler(ctx, clk, fx)

	txBuilder := builder.New(
		ctx,
		&config,
		clk,
		fx,
		baseState,
		atomicUTXOs,
		utxoHandler,
	)

	backend := Backend{
		Config:       &config,
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
		config:         &config,
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
			preFundedKeys[0].PublicKey().Address(),
			preFundedKeys[1].PublicKey().Address(),
			preFundedKeys[2].PublicKey().Address(),
		},
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		preFundedKeys[0].PublicKey().Address(),
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
}

func defaultState(
	cfg *config.Config,
	ctx *snow.Context,
	db database.Database,
	rewards reward.Calculator,
) state.State {
	genesis := buildGenesisTest(ctx)
	execCfg, _ := config.GetExecutionConfig(nil)
	state, err := state.New(
		db,
		genesis,
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

func defaultCtx(db database.Database) (*snow.Context, *mutableSharedMemory) {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = 10
	ctx.XChainID = xChainID
	ctx.CChainID = cChainID
	ctx.AVAXAssetID = avaxAssetID

	atomicDB := prefixdb.New([]byte{1}, db)
	m := atomic.NewMemory(atomicDB)

	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	ctx.ValidatorState = &validators.TestState{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constants.PlatformChainID: constants.PrimaryNetworkID,
				xChainID:                  constants.PrimaryNetworkID,
				cChainID:                  constants.PrimaryNetworkID,
			}[chainID]
			if !ok {
				return ids.Empty, errMissing
			}
			return subnetID, nil
		},
	}

	return ctx, msm
}

func defaultConfig(postBanff, postCortina bool) config.Config {
	banffTime := mockable.MaxTime
	if postBanff {
		banffTime = defaultValidateEndTime.Add(-2 * time.Second)
	}
	cortinaTime := mockable.MaxTime
	if postCortina {
		cortinaTime = defaultValidateStartTime.Add(-2 * time.Second)
	}

	return config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		Validators:             validators.NewManager(),
		TxFee:                  defaultTxFee,
		CreateSubnetTxFee:      100 * defaultTxFee,
		CreateBlockchainTxFee:  100 * defaultTxFee,
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
		ApricotPhase3Time: defaultValidateEndTime,
		ApricotPhase5Time: defaultValidateEndTime,
		BanffTime:         banffTime,
		CortinaTime:       cortinaTime,
	}
}

func defaultClock(postFork bool) *mockable.Clock {
	now := defaultGenesisTime
	if postFork {
		// 1 second after Banff fork
		now = defaultValidateEndTime.Add(-2 * time.Second)
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

func buildGenesisTest(ctx *snow.Context) *genesis.Genesis {
	genesisUtxos := make([]*genesis.UTXO, len(preFundedKeys))
	for i, key := range preFundedKeys {
		addr := key.PublicKey().Address()
		genesisUtxos[i] = &genesis.UTXO{
			UTXO: avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        ids.Empty,
					OutputIndex: uint32(i),
				},
				Asset: avax.Asset{ID: ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: defaultBalance,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				},
			},
			Message: nil,
		}
	}

	vdrs := txheap.NewByEndTime()
	for _, key := range preFundedKeys {
		addr := key.PublicKey().Address()
		nodeID := ids.NodeID(key.PublicKey().Address())

		utxo := &avax.TransferableOutput{
			Asset: avax.Asset{ID: ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: defaultWeight,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
		}

		owner := &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		}

		tx := &txs.Tx{Unsigned: &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: constants.PlatformChainID,
			}},
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(defaultValidateStartTime.Unix()),
				End:    uint64(defaultValidateEndTime.Unix()),
				Wght:   utxo.Output().Amount(),
			},
			StakeOuts:        []*avax.TransferableOutput{utxo},
			RewardsOwner:     owner,
			DelegationShares: reward.PercentDenominator,
		}}

		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			panic(err)
		}
		vdrs.Add(tx)
	}

	return &genesis.Genesis{
		GenesisID:     hashing.ComputeHash256Array(ids.Empty[:]),
		UTXOs:         genesisUtxos,
		Validators:    vdrs.List(),
		Chains:        nil,
		Timestamp:     uint64(defaultGenesisTime.Unix()),
		InitialSupply: 360 * units.MegaAvax,
	}
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
