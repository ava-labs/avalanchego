// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/caminoconfig"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	testNetworkID                = 10 // To be used in tests
	defaultCaminoValidatorWeight = 2 * units.KiloAvax
	defaultMinStakingDuration    = 24 * time.Hour
	defaultMaxStakingDuration    = 365 * 24 * time.Hour
	defaultCaminoBalance         = 100 * defaultCaminoValidatorWeight
	defaultTxFee                 = uint64(100)
	localStakingPath             = "../../../../staking/local/"
)

var (
	defaultGenesisTime           = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultValidateStartTime     = defaultGenesisTime
	defaultValidateEndTime       = defaultValidateStartTime.Add(10 * defaultMinStakingDuration)
	avaxAssetID                  = ids.ID{'y', 'e', 'e', 't'}
	xChainID                     = ids.Empty.Prefix(0)
	cChainID                     = ids.Empty.Prefix(1)
	caminoPreFundedKeys          = secp256k1.TestKeys()
	_, caminoPreFundedNodeIDs    = nodeid.LoadLocalCaminoNodeKeysAndIDs(localStakingPath)
	testCaminoSubnet1ControlKeys = caminoPreFundedKeys[0:3]
	lastAcceptedID               = ids.GenerateTestID()
	testSubnet1                  *txs.Tx
	testKeyfactory               secp256k1.Factory
	errMissing                   = errors.New("missing")
)

type mutableSharedMemory struct {
	atomic.SharedMemory
}

type caminoEnvironment struct {
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
	txBuilder      CaminoBuilder
	backend        executor.Backend
}

func (e *caminoEnvironment) GetState(blkID ids.ID) (state.Chain, bool) {
	if blkID == lastAcceptedID {
		return e.state, true
	}
	chainState, ok := e.states[blkID]
	return chainState, ok
}

func (e *caminoEnvironment) SetState(blkID ids.ID, chainState state.Chain) {
	e.states[blkID] = chainState
}

func newCaminoEnvironment(postBanff bool, caminoGenesisConf api.Camino) *caminoEnvironment {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	config := defaultCaminoConfig(postBanff)
	clk := defaultClock(postBanff)

	baseDBManager := manager.NewMemDB(version.CurrentDatabase)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	ctx, msm := defaultCtx(baseDB)

	fx := defaultFx(&clk, ctx.Log, isBootstrapped.Get())

	rewards := reward.NewCalculator(config.RewardConfig)
	baseState := defaultCaminoState(&config, ctx, baseDB, rewards, caminoGenesisConf)

	atomicUTXOs := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	uptimes := uptime.NewManager(baseState)
	utxoHandler := utxo.NewCaminoHandler(ctx, &clk, fx, true)

	txBuilder := NewCamino(
		ctx,
		&config,
		&clk,
		fx,
		baseState,
		atomicUTXOs,
		utxoHandler,
	)

	backend := executor.Backend{
		Config:       &config,
		Ctx:          ctx,
		Clk:          &clk,
		Bootstrapped: &isBootstrapped,
		Fx:           fx,
		FlowChecker:  utxoHandler,
		Uptimes:      uptimes,
		Rewards:      rewards,
	}

	env := &caminoEnvironment{
		isBootstrapped: &isBootstrapped,
		config:         &config,
		clk:            &clk,
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

	addCaminoSubnet(env, txBuilder)

	return env
}

func newCaminoBuilder(postBanff bool, state state.State) (*caminoBuilder, *versiondb.Database) {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	config := defaultCaminoConfig(postBanff)
	clk := defaultClock(postBanff)

	baseDBManager := manager.NewMemDB(version.CurrentDatabase)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	ctx, _ := defaultCtx(baseDB)

	fx := defaultFx(&clk, ctx.Log, isBootstrapped.Get())

	atomicUTXOs := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	utxoHandler := utxo.NewHandler(ctx, &clk, fx)

	txBuilder := NewCamino(
		ctx,
		&config,
		&clk,
		fx,
		state,
		atomicUTXOs,
		utxoHandler,
	)

	caminoBuilder, ok := txBuilder.(*caminoBuilder)
	if !ok {
		panic("not camino builder")
	}

	return caminoBuilder, baseDB
}

func addCaminoSubnet(
	env *caminoEnvironment,
	txBuilder Builder,
) {
	// Create a subnet
	var err error
	testSubnet1, err = txBuilder.NewCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		[]ids.ShortID{ // control keys
			caminoPreFundedKeys[0].PublicKey().Address(),
			caminoPreFundedKeys[1].PublicKey().Address(),
			caminoPreFundedKeys[2].PublicKey().Address(),
		},
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
		caminoPreFundedKeys[0].PublicKey().Address(),
	)
	if err != nil {
		panic(err)
	}

	// store it
	stateDiff, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		panic(err)
	}

	caminoExecutor := executor.CaminoStandardTxExecutor{
		StandardTxExecutor: executor.StandardTxExecutor{
			Backend: &env.backend,
			State:   stateDiff,
			Tx:      testSubnet1,
		},
	}
	err = testSubnet1.Unsigned.Visit(&caminoExecutor)
	if err != nil {
		panic(err)
	}

	stateDiff.AddTx(testSubnet1, status.Committed)
	stateDiff.Apply(env.state)
}

func defaultCtx(db database.Database) (*snow.Context, *mutableSharedMemory) {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = 10
	ctx.XChainID = xChainID
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

func defaultClock(postBanff bool) mockable.Clock {
	now := defaultGenesisTime
	if postBanff {
		// 1 second after Banff fork
		now = defaultValidateEndTime.Add(-2 * time.Second)
	}
	clk := mockable.Clock{}
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

func defaultCaminoState(
	cfg *config.Config,
	ctx *snow.Context,
	db database.Database,
	rewards reward.Calculator,
	caminoGenesisConf api.Camino,
) state.State {
	genesisBytes := buildCaminoGenesisTest(ctx, caminoGenesisConf)
	state, err := state.New(
		db,
		genesisBytes,
		prometheus.NewRegistry(),
		cfg,
		ctx,
		metrics.Noop,
		rewards,
		&utils.Atomic[bool]{},
	)
	if err != nil {
		panic(err)
	}

	// persist and reload to init a bunch of in-memory stuff
	state.SetHeight(0)
	if err := state.Commit(); err != nil {
		panic(err)
	}
	state.SetHeight( /*height*/ 0)
	if err := state.Commit(); err != nil {
		panic(err)
	}
	state.GetLastAccepted()
	return state
}

func defaultCaminoConfig(postBanff bool) config.Config {
	banffTime := mockable.MaxTime
	if postBanff {
		banffTime = defaultValidateEndTime.Add(-2 * time.Second)
	}

	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)

	return config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		Validators:             vdrs,
		TxFee:                  defaultTxFee,
		CreateSubnetTxFee:      100 * defaultTxFee,
		CreateBlockchainTxFee:  100 * defaultTxFee,
		MinValidatorStake:      defaultCaminoValidatorWeight,
		MaxValidatorStake:      defaultCaminoValidatorWeight,
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
		CaminoConfig: caminoconfig.Config{
			DaoProposalBondAmount: 100 * units.Avax,
		},
	}
}

func buildCaminoGenesisTest(ctx *snow.Context, caminoGenesisConf api.Camino) []byte {
	genesisUTXOs := make([]api.UTXO, len(caminoPreFundedKeys))
	hrp := constants.NetworkIDToHRP[testNetworkID]
	for i, key := range caminoPreFundedKeys {
		addr, err := address.FormatBech32(hrp, key.PublicKey().Address().Bytes())
		if err != nil {
			panic(err)
		}
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultCaminoBalance),
			Address: addr,
		}
	}

	caminoGenesisConf.UTXODeposits = make([]api.UTXODeposit, len(genesisUTXOs))
	caminoGenesisConf.ValidatorDeposits = make([][]api.UTXODeposit, len(caminoPreFundedKeys))
	caminoGenesisConf.ValidatorConsortiumMembers = make([]ids.ShortID, len(caminoPreFundedKeys))

	genesisValidators := make([]api.PermissionlessValidator, len(caminoPreFundedKeys))
	for i, key := range caminoPreFundedKeys {
		addr, err := address.FormatBech32(hrp, key.PublicKey().Address().Bytes())
		if err != nil {
			panic(err)
		}
		genesisValidators[i] = api.PermissionlessValidator{
			Staker: api.Staker{
				StartTime: json.Uint64(defaultValidateStartTime.Unix()),
				EndTime:   json.Uint64(defaultValidateEndTime.Unix()),
				NodeID:    caminoPreFundedNodeIDs[i],
			},
			RewardOwner: &api.Owner{
				Threshold: 1,
				Addresses: []string{addr},
			},
			Staked: []api.UTXO{{
				Amount:  json.Uint64(defaultCaminoValidatorWeight),
				Address: addr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
		caminoGenesisConf.ValidatorDeposits[i] = make([]api.UTXODeposit, 1)
		caminoGenesisConf.ValidatorConsortiumMembers[i] = key.Address()
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		NetworkID:     json.Uint32(testNetworkID),
		AvaxAssetID:   ctx.AVAXAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		Camino:        caminoGenesisConf,
		InitialSupply: json.Uint64(360 * units.MegaAvax),
		Encoding:      formatting.Hex,
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		panic(fmt.Errorf("problem while building platform chain's genesis state: %w", err))
	}

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	if err != nil {
		panic(err)
	}

	return genesisBytes
}

func shutdownCaminoEnvironment(env *caminoEnvironment) error {
	if env.isBootstrapped.Get() {
		primaryValidatorSet, exist := env.config.Validators.Get(constants.PrimaryNetworkID)
		if !exist {
			return errors.New("no default subnet validators")
		}
		primaryValidators := primaryValidatorSet.List()

		validatorIDs := make([]ids.NodeID, len(primaryValidators))
		for i, vdr := range primaryValidators {
			validatorIDs[i] = vdr.NodeID
		}

		if err := env.uptimes.StopTracking(validatorIDs, constants.PrimaryNetworkID); err != nil {
			return err
		}
		env.state.SetHeight( /*height*/ math.MaxUint64)
		if err := env.state.Commit(); err != nil {
			return err
		}
	}

	errs := wrappers.Errs{}
	errs.Add(
		env.state.Close(),
		env.baseDB.Close(),
	)
	return errs.Err
}

func generateTestUTXO(txID ids.ID, assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID) *avax.UTXO {
	var out avax.TransferableOut = &secp256k1fx.TransferOutput{
		Amt:          amount,
		OutputOwners: outputOwners,
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		out = &locked.Out{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableOut: out,
		}
	}
	testUTXO := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: txID},
		Asset:  avax.Asset{ID: assetID},
		Out:    out,
	}
	testUTXO.InputID()
	return testUTXO
}

func generateKeyAndOwner() (*secp256k1.PrivateKey, ids.ShortID, secp256k1fx.OutputOwners) {
	key, err := testKeyfactory.NewPrivateKey()
	if err != nil {
		panic(err)
	}
	addr := key.Address()

	return key, addr, secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}
}

func generateTestInFromUTXO(utxo *avax.UTXO, sigIndices []uint32, init bool) *avax.TransferableInput {
	var in avax.TransferableIn
	switch out := utxo.Out.(type) {
	case *secp256k1fx.TransferOutput:
		in = &secp256k1fx.TransferInput{
			Amt:   out.Amount(),
			Input: secp256k1fx.Input{SigIndices: sigIndices},
		}
	case *locked.Out:
		in = &locked.In{
			IDs: out.IDs,
			TransferableIn: &secp256k1fx.TransferInput{
				Amt:   out.Amount(),
				Input: secp256k1fx.Input{SigIndices: sigIndices},
			},
		}
	default:
		panic("unknown utxo.Out type")
	}

	input := &avax.TransferableInput{
		UTXOID: avax.UTXOID{TxID: utxo.UTXOID.TxID, OutputIndex: utxo.UTXOID.OutputIndex},
		Asset:  utxo.Asset,
		In:     in,
	}

	if init {
		input.InputID()
	}

	return input
}

func newCaminoBuilderWithMocks(postBanff bool, state state.State, sharedMemory atomic.SharedMemory) (*caminoBuilder, *versiondb.Database) {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	config := defaultCaminoConfig(postBanff)
	clk := defaultClock(postBanff)

	baseDBManager := manager.NewMemDB(version.CurrentDatabase)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	ctx, _ := defaultCtx(baseDB)

	if sharedMemory != nil {
		ctx.SharedMemory = sharedMemory
	}

	fx := defaultFx(&clk, ctx.Log, isBootstrapped.Get())

	atomicUTXOs := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	utxoHandler := utxo.NewHandler(ctx, &clk, fx)

	txBuilder := NewCamino(
		ctx,
		&config,
		&clk,
		fx,
		state,
		atomicUTXOs,
		utxoHandler,
	)

	caminoBuilder, ok := txBuilder.(*caminoBuilder)
	if !ok {
		panic("not camino builder")
	}

	return caminoBuilder, baseDB
}

func expectLock(s *state.MockState, allUTXOs map[ids.ShortID][]*avax.UTXO) {
	for addr, utxos := range allUTXOs {
		utxoids := make([]ids.ID, len(utxos))
		for i := range utxos {
			utxoids[i] = utxos[i].InputID()
		}
		s.EXPECT().UTXOIDs(addr.Bytes(), ids.Empty, math.MaxInt).Return(utxoids, nil)
		for _, utxo := range utxos {
			s.EXPECT().GetUTXO(utxo.InputID()).Return(utxo, nil)
			s.EXPECT().GetMultisigAlias(addr).Return(nil, database.ErrNotFound).Times(2)
		}
	}
}
