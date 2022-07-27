// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"errors"
	"fmt"
	"testing"
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
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/prometheus/client_golang/prometheus"

	tx_builder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
)

var (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxValidatorStake  = 500 * units.MilliAvax
	defaultMaxStakingDuration = 365 * 24 * time.Hour
	defaultGenesisTime        = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultValidateStartTime  = defaultGenesisTime
	defaultValidateEndTime    = defaultValidateStartTime.Add(10 * defaultMinStakingDuration)
	defaultMinValidatorStake  = 5 * units.MilliAvax
	defaultBalance            = 100 * defaultMinValidatorStake
	preFundedKeys             = crypto.BuildTestKeys()
	avaxAssetID               = ids.ID{'y', 'e', 'e', 't'}
	defaultTxFee              = uint64(100)
	xChainID                  = ids.Empty.Prefix(0)
	cChainID                  = ids.Empty.Prefix(1)

	testSubnet1            *txs.Tx
	testSubnet1ControlKeys = preFundedKeys[0:3]
	testKeyFactory         = crypto.FactorySECP256K1R{}
)

const (
	testNetworkID                 = 10 // To be used in tests
	defaultWeight                 = 10000
	maxRecentlyAcceptedWindowSize = 256
	recentlyAcceptedWindowTTL     = 5 * time.Minute
)

type mutableSharedMemory struct {
	atomic.SharedMemory
}

type dummyBlkTimer struct{}

func (*dummyBlkTimer) ResetBlockTimer() {}

type testHelpersCollection struct {
	BlockBuilder
	blkManager stateful.Manager
	mempool    mempool.Mempool
	sender     *common.SenderTest

	isBootstrapped *utils.AtomicBool
	cfg            *config.Config
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *snow.Context
	msm            *mutableSharedMemory
	fx             fx.Fx
	fullState      state.State
	atomicUtxosMan avax.AtomicUTXOManager
	uptimeMan      uptime.Manager
	utxosMan       utxo.Handler
	txBuilder      tx_builder.Builder
	txExecBackend  executor.Backend
	stateVersions  state.Versions
}

// TODO snLookup currently duplicated in vm_test.go. Consider removing duplication
type snLookup struct {
	chainsToSubnet map[ids.ID]ids.ID
}

func (sn *snLookup) SubnetID(chainID ids.ID) (ids.ID, error) {
	subnetID, ok := sn.chainsToSubnet[chainID]
	if !ok {
		return ids.ID{}, errors.New("")
	}
	return subnetID, nil
}

func newTestHelpersCollection(t *testing.T, mockResetBlockTimer bool) *testHelpersCollection {
	var (
		res = &testHelpersCollection{}
		err error
	)

	res.isBootstrapped = &utils.AtomicBool{}
	res.isBootstrapped.SetValue(true)

	res.cfg = defaultCfg()
	res.clk = defaultClock()

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	res.baseDB = versiondb.New(baseDBManager.Current().Database)
	res.ctx, res.msm = defaultCtx(res.baseDB)
	res.fx = defaultFx(res.clk, res.ctx.Log, res.isBootstrapped.GetValue())

	rewardsCalc := reward.NewCalculator(res.cfg.RewardConfig)
	res.fullState = defaultState(res.cfg, res.ctx, res.baseDB, rewardsCalc)

	res.atomicUtxosMan = avax.NewAtomicUTXOManager(res.ctx.SharedMemory, txs.Codec)
	res.uptimeMan = uptime.NewManager(res.fullState)
	res.utxosMan = utxo.NewHandler(res.ctx, res.clk, res.fullState, res.fx)

	res.txBuilder = tx_builder.New(
		res.ctx,
		*res.cfg,
		res.clk,
		res.fx,
		res.fullState,
		res.atomicUtxosMan,
		res.utxosMan,
	)

	genesisID := res.fullState.GetLastAccepted()
	res.stateVersions = state.NewVersions(genesisID, res.fullState)
	res.txExecBackend = executor.Backend{
		Config:        res.cfg,
		Ctx:           res.ctx,
		Clk:           res.clk,
		Bootstrapped:  res.isBootstrapped,
		Fx:            res.fx,
		FlowChecker:   res.utxosMan,
		Uptimes:       res.uptimeMan,
		Rewards:       rewardsCalc,
		StateVersions: res.stateVersions,
	}

	registerer := prometheus.NewRegistry()
	window := window.New(
		window.Config{
			Clock:   res.clk,
			MaxSize: maxRecentlyAcceptedWindowSize,
			TTL:     recentlyAcceptedWindowTTL,
		},
	)
	res.sender = &common.SenderTest{T: t}

	metrics, err := metrics.New("", registerer, res.cfg.WhitelistedSubnets)
	if err != nil {
		panic(fmt.Errorf("failed to create metrics: %w", err))
	}

	if mockResetBlockTimer {
		dummy := &dummyBlkTimer{}
		res.mempool, err = mempool.NewMempool("mempool", registerer, dummy)
	} else {
		res.mempool, err = mempool.NewMempool("mempool", registerer, res)
	}

	if err != nil {
		panic(fmt.Errorf("failed to create mempool: %w", err))
	}
	res.blkManager = stateful.NewManager(
		res.mempool,
		metrics,
		res.fullState,
		res.txExecBackend,
		window,
	)

	res.BlockBuilder = NewBlockBuilder(
		res.mempool,
		res.txBuilder,
		res.txExecBackend,
		res.blkManager,
		nil, // toEngine,
		res.sender,
	)

	if err := res.BlockBuilder.SetPreference(genesisID); err != nil {
		panic(fmt.Errorf("failed setting last accepted block: %w", err))
	}

	addSubnet(res.fullState, res.txBuilder, res.txExecBackend)

	return res
}

func addSubnet(
	baseState state.State,
	txBuilder tx_builder.Builder,
	backend executor.Backend,
) {
	// Create a subnet
	var err error
	testSubnet1, err = txBuilder.NewCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		[]ids.ShortID{ // control keys
			preFundedKeys[0].PublicKey().Address(),
			preFundedKeys[1].PublicKey().Address(),
			preFundedKeys[2].PublicKey().Address(),
		},
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
		preFundedKeys[0].PublicKey().Address(),
	)
	if err != nil {
		panic(err)
	}

	// store it
	genesisID := baseState.GetLastAccepted()
	stateDiff, err := state.NewDiff(genesisID, backend.StateVersions)
	if err != nil {
		panic(err)
	}

	executor := executor.StandardTxExecutor{
		Backend: &backend,
		State:   stateDiff,
		Tx:      testSubnet1,
	}
	err = testSubnet1.Unsigned.Visit(&executor)
	if err != nil {
		panic(err)
	}

	stateDiff.AddTx(testSubnet1, status.Committed)
	stateDiff.Apply(baseState)
	backend.StateVersions.SetState(genesisID, baseState)
}

func defaultState(
	cfg *config.Config,
	ctx *snow.Context,
	baseDB *versiondb.Database,
	rewardsCalc reward.Calculator,
) state.State {
	genesisBytes := buildGenesisTest(ctx)
	tState, err := state.New(
		baseDB,
		genesisBytes,
		prometheus.NewRegistry(),
		cfg,
		ctx,
		metrics.NewNoopMetrics(),
		rewardsCalc,
	)
	if err != nil {
		panic(err)
	}
	return tState
}

func defaultCtx(baseDB *versiondb.Database) (*snow.Context, *mutableSharedMemory) {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = 10
	ctx.XChainID = xChainID
	ctx.AVAXAssetID = avaxAssetID

	atomicDB := prefixdb.New([]byte{1}, baseDB)
	m := atomic.NewMemory(atomicDB)

	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	ctx.SNLookup = &snLookup{
		chainsToSubnet: map[ids.ID]ids.ID{
			constants.PlatformChainID: constants.PrimaryNetworkID,
			xChainID:                  constants.PrimaryNetworkID,
			cChainID:                  constants.PrimaryNetworkID,
		},
	}

	return ctx, msm
}

func defaultCfg() *config.Config {
	return &config.Config{
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
		BlueberryTime:     mockable.MaxTime,
	}
}

func defaultClock() *mockable.Clock {
	clk := mockable.Clock{}
	clk.Set(defaultGenesisTime)
	return &clk
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

func buildGenesisTest(ctx *snow.Context) []byte {
	genesisUTXOs := make([]api.UTXO, len(preFundedKeys))
	hrp := constants.NetworkIDToHRP[testNetworkID]
	for i, key := range preFundedKeys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(hrp, id.Bytes())
		if err != nil {
			panic(err)
		}
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]api.PrimaryValidator, len(preFundedKeys))
	for i, key := range preFundedKeys {
		nodeID := ids.NodeID(key.PublicKey().Address())
		addr, err := address.FormatBech32(hrp, nodeID.Bytes())
		if err != nil {
			panic(err)
		}
		genesisValidators[i] = api.PrimaryValidator{
			Staker: api.Staker{
				StartTime: json.Uint64(defaultValidateStartTime.Unix()),
				EndTime:   json.Uint64(defaultValidateEndTime.Unix()),
				NodeID:    nodeID,
			},
			RewardOwner: &api.Owner{
				Threshold: 1,
				Addresses: []string{addr},
			},
			Staked: []api.UTXO{{
				Amount:  json.Uint64(defaultWeight),
				Address: addr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		NetworkID:     json.Uint32(testNetworkID),
		AvaxAssetID:   ctx.AVAXAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
		Encoding:      formatting.Hex,
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		panic(fmt.Errorf("problem while building platform chain's genesis state: %v", err))
	}

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	if err != nil {
		panic(err)
	}

	return genesisBytes
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

		if err := t.uptimeMan.Shutdown(validatorIDs); err != nil {
			return err
		}
		if err := t.fullState.Commit(); err != nil {
			return err
		}
	}

	errs := wrappers.Errs{}
	errs.Add(
		t.fullState.Close(),
		t.baseDB.Close(),
	)
	return errs.Err
}

func createTestDecisionTxes(count int) ([]*txs.Tx, error) {
	res := make([]*txs.Tx, 0, count)
	for i := uint32(0); i < uint32(count); i++ {
		utx := &txs.CreateChainTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    10,
				BlockchainID: ids.Empty.Prefix(uint64(i)),
				Ins: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID:        ids.ID{'t', 'x', 'I', 'D'},
						OutputIndex: i,
					},
					Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
					In: &secp256k1fx.TransferInput{
						Amt:   uint64(5678),
						Input: secp256k1fx.Input{SigIndices: []uint32{i}},
					},
				}},
				Outs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
					Out: &secp256k1fx.TransferOutput{
						Amt: uint64(1234),
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
						},
					},
				}},
			}},
			SubnetID:    ids.GenerateTestID(),
			ChainName:   "chainName",
			VMID:        ids.GenerateTestID(),
			FxIDs:       []ids.ID{ids.GenerateTestID()},
			GenesisData: []byte{'g', 'e', 'n', 'D', 'a', 't', 'a'},
			SubnetAuth:  &secp256k1fx.Input{SigIndices: []uint32{1}},
		}

		tx, err := txs.NewSigned(utx, txs.Codec, nil)
		if err != nil {
			return nil, err
		}
		res = append(res, tx)
	}
	return res, nil
}

func createTestValidatorTx(h *testHelpersCollection, startTime, endTime time.Time) (*txs.Tx, error) {
	return h.txBuilder.NewAddValidatorTx(
		h.cfg.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		h.ctx.NodeID,  // node ID
		ids.ShortID{}, // reward address
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
		preFundedKeys[0].PublicKey().Address(), // change addr
	)
}
