// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/prometheus/client_golang/prometheus"

	p_validator "github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

var (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour
	defaultGenesisTime        = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultValidateStartTime  = defaultGenesisTime
	defaultValidateEndTime    = defaultValidateStartTime.Add(10 * defaultMinStakingDuration)
	defaultMinValidatorStake  = 5 * units.MilliAvax
	defaultBalance            = 100 * defaultMinValidatorStake
	preFundedKeys             []*crypto.PrivateKeySECP256K1R
	avaxAssetID               = ids.ID{'y', 'e', 'e', 't'}
	defaultTxFee              = uint64(100)
	xChainID                  = ids.Empty.Prefix(0)
	cChainID                  = ids.Empty.Prefix(1)
)

type testHelpersCollection struct {
	isBootstrapped *utils.AtomicBool
	cfg            *config.Config
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *snow.Context
	fx             fx.Fx
	tState         state.State
	atomicUtxosMan avax.AtomicUTXOManager
	utxosMan       utxos.SpendHandler
	txBuilder      builder.TxBuilder
}

// TODO ABENEGIA: snLookup currently duplicated in vm_test.go
type snLookup struct {
	chainsToSubnet map[ids.ID]ids.ID
}

func (sn *snLookup) SubnetID(chainID ids.ID) (ids.ID, error) {
	subnetID, ok := sn.chainsToSubnet[chainID]
	if !ok {
		return ids.ID{}, errors.New("missing subnet associated with requested chainID")
	}
	return subnetID, nil
}

func init() {
	preFundedKeys = defaultKeys()
}

func newTestHelpersCollection() *testHelpersCollection {
	var isBootstrapped utils.AtomicBool
	isBootstrapped.SetValue(true)

	cfg := defaultCfg()
	clk := defaultClock()

	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	ctx := defaultCtx(baseDB)

	fx := defaultFx(&clk, ctx.Log, isBootstrapped.GetValue())

	rewardsCalc := reward.NewCalculator(cfg.RewardConfig)
	tState := defaultState(&cfg, ctx, baseDB, rewardsCalc)

	atomicUtxosMan := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	utxosMan := utxos.NewHandler(ctx, clk, tState, fx)

	txBuilder := builder.NewTxBuilder(
		ctx, cfg, clk, fx,
		tState, atomicUtxosMan,
		utxosMan, rewardsCalc)

	return &testHelpersCollection{
		isBootstrapped: &isBootstrapped,
		cfg:            &cfg,
		clk:            &clk,
		baseDB:         baseDB,
		ctx:            ctx,
		fx:             fx,
		tState:         tState,
		atomicUtxosMan: atomicUtxosMan,
		utxosMan:       utxosMan,
		txBuilder:      txBuilder,
	}
}

func defaultState(
	cfg *config.Config,
	ctx *snow.Context,
	baseDB *versiondb.Database,
	rewardsCalc reward.Calculator,
) state.State {
	dummyLocalStake := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "uts",
		Name:      "local_staked",
		Help:      "Total amount of AVAX on this node staked",
	})
	dummyTotalStake := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "uts",
		Name:      "total_staked",
		Help:      "Total amount of AVAX staked",
	})

	tState := state.New(baseDB, cfg, ctx, dummyLocalStake, dummyTotalStake, rewardsCalc)

	// setup initial data as if we are storing genesis
	initializeState(tState, ctx)

	// persist and reload to init a bunch of in-memory stuff
	if err := tState.Write(); err != nil {
		panic(err)
	}
	if err := tState.Load(); err != nil {
		panic(err)
	}
	return tState
}

func defaultCtx(baseDB *versiondb.Database) *snow.Context {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = 10
	ctx.XChainID = xChainID
	ctx.AVAXAssetID = avaxAssetID

	atomicDB := prefixdb.New([]byte{1}, baseDB)
	m := &atomic.Memory{}
	err := m.Initialize(logging.NoLog{}, atomicDB)
	if err != nil {
		panic(err)
	}

	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	ctx.SNLookup = &snLookup{
		chainsToSubnet: map[ids.ID]ids.ID{
			constants.PlatformChainID: constants.PrimaryNetworkID,
			xChainID:                  constants.PrimaryNetworkID,
			cChainID:                  constants.PrimaryNetworkID,
		},
	}

	return ctx
}

func defaultCfg() config.Config {
	return config.Config{
		Chains:                 chains.MockManager{},
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
		ApricotPhase4Time: defaultValidateEndTime,
		ApricotPhase5Time: defaultValidateEndTime,
	}
}

func defaultClock() mockable.Clock {
	clk := mockable.Clock{}
	clk.Set(defaultGenesisTime)
	return clk
}

type fxVMInt struct {
	registry codec.Registry
	clk      *mockable.Clock
	log      logging.Logger
}

func (fvi *fxVMInt) CodecRegistry() codec.Registry { return fvi.registry }
func (fvi *fxVMInt) Clock() *mockable.Clock        { return fvi.clk }
func (fvi *fxVMInt) Logger() logging.Logger        { return fvi.log }

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

func defaultKeys() []*crypto.PrivateKeySECP256K1R {
	dummyCtx := snow.DefaultContextTest()
	res := make([]*crypto.PrivateKeySECP256K1R, 0)
	factory := crypto.FactorySECP256K1R{}
	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
		"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
		"2RWLv6YVEXDiWLpaCbXhhqxtLbnFaKQsWPSSMSPhpWo47uJAeV",
	} {
		privKeyBytes, err := formatting.Decode(formatting.CB58, key)
		dummyCtx.Log.AssertNoError(err)
		pk, err := factory.ToPrivateKey(privKeyBytes)
		dummyCtx.Log.AssertNoError(err)
		res = append(res, pk.(*crypto.PrivateKeySECP256K1R))
	}
	return res
}

func initializeState(tState state.State, ctx *snow.Context) {
	utxos := make([]*avax.UTXO, len(preFundedKeys))
	for i, key := range preFundedKeys {
		addr := key.PublicKey().Address()
		utxos[i] = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: defaultBalance,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
		}
	}

	timestamp := uint64(defaultGenesisTime.Unix())
	initialSupply := 360 * units.MegaAvax
	validators := make([]*txs.Tx, len(preFundedKeys))
	for i, key := range preFundedKeys {
		addrID := key.PublicKey().Address()
		utx := &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    ctx.NetworkID,
				BlockchainID: ctx.ChainID,
				Ins:          nil,
				Outs:         nil,
			}},
			Validator: p_validator.Validator{
				Start: uint64(defaultValidateStartTime.Unix()),
				End:   uint64(defaultValidateEndTime.Unix()),
				Wght:  defaultBalance,
			},
			Stake: nil,
			RewardsOwner: &secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{addrID},
			},
			Shares: uint32(defaultTxFee),
		}
		tx := &txs.Tx{Unsigned: utx}
		if err := tx.Sign(txs.Codec, nil); err != nil {
			panic(err)
		}
		validators[i] = tx
	}

	chains := make([]*txs.Tx, 0)
	dummyGenID := ids.ID{'g', 'e', 'n', 'I', 'D'}
	if err := tState.SyncGenesis(
		dummyGenID,
		timestamp,
		initialSupply,
		utxos,
		validators,
		chains,
	); err != nil {
		panic(err)
	}
}

func internalStateShutdown(t *testHelpersCollection) error {
	if t.isBootstrapped.GetValue() {
		primaryValidatorSet, exist := t.cfg.Validators.GetValidators(constants.PrimaryNetworkID)
		if !exist {
			return errors.New("no default subnet validators")
		}
		primaryValidators := primaryValidatorSet.List()

		validatorIDs := make([]ids.NodeID, len(primaryValidators))
		for i, vdr := range primaryValidators {
			validatorIDs[i] = vdr.ID()
		}

		if err := t.tState.Write(); err != nil {
			return err
		}
	}

	errs := wrappers.Errs{}
	errs.Add(
		t.tState.Close(),
		t.baseDB.Close(),
	)
	return errs.Err
}
