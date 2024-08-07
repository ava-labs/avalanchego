// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/caminoconfig"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	defaultCaminoValidatorWeight = 2 * units.KiloAvax
	defaultCaminoBalance         = 100 * defaultCaminoValidatorWeight
	localStakingPath             = "../../../../staking/local/"
)

var (
	caminoPreFundedKeys                             = secp256k1.TestKeys()
	caminoPreFundedNodeKeys, caminoPreFundedNodeIDs = nodeid.LoadLocalCaminoNodeKeysAndIDs(localStakingPath)
	testCaminoSubnet1ControlKeys                    = caminoPreFundedKeys[0:3]
)

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

func (e *caminoEnvironment) SetState(blkID ids.ID, chainState state.Chain) {
	e.states[blkID] = chainState
}

func newCaminoEnvironment(postBanff, addSubnet bool, caminoGenesisConf api.Camino) *caminoEnvironment {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	config := defaultCaminoConfig(postBanff)
	clk := defaultClock(postBanff)

	baseDBManager := manager.NewMemDB(version.CurrentDatabase)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	ctx, msm := defaultCtx(baseDB)

	fx := defaultFx(&clk, ctx.Log, isBootstrapped.Get())

	rewards := reward.NewCalculator(config.RewardConfig)

	baseState := defaultCaminoState(config, ctx, baseDB, rewards, caminoGenesisConf)

	atomicUTXOs := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	uptimes := uptime.NewManager(baseState)
	utxoHandler := utxo.NewCaminoHandler(ctx, &clk, fx, true)

	txBuilder := builder.NewCamino(
		ctx,
		config,
		&clk,
		fx,
		baseState,
		atomicUTXOs,
		utxoHandler,
	)

	backend := Backend{
		Config:       config,
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
		config:         config,
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

	if addSubnet {
		addCaminoSubnet(env, txBuilder)
	}

	return env
}

func addCaminoSubnet(
	env *caminoEnvironment,
	txBuilder builder.Builder,
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

	executor := CaminoStandardTxExecutor{
		StandardTxExecutor{
			Backend: &env.backend,
			State:   stateDiff,
			Tx:      testSubnet1,
		},
	}
	err = testSubnet1.Unsigned.Visit(&executor)
	if err != nil {
		panic(err)
	}

	stateDiff.AddTx(testSubnet1, status.Committed)
	if err := stateDiff.Apply(env.state); err != nil {
		panic(err)
	}
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
	lastAcceptedID = state.GetLastAccepted()
	return state
}

func defaultCaminoConfig(postBanff bool) *config.Config {
	config := defaultConfig(postBanff, true)
	config.MinValidatorStake = defaultCaminoValidatorWeight
	config.MaxValidatorStake = defaultCaminoValidatorWeight
	config.BerlinPhaseTime = defaultValidateStartTime.Add(-2 * time.Second)
	config.CaminoConfig = caminoconfig.Config{
		DACProposalBondAmount: 100 * units.Avax,
	}
	return &config
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
		Camino:        &caminoGenesisConf,
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

func generateTestUTXO(txID ids.ID, assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID) *avax.UTXO {
	return generateTestUTXOWithIndex(txID, 0, assetID, amount, outputOwners, depositTxID, bondTxID, true)
}

func generateTestUTXOWithIndex(txID ids.ID, outIndex uint32, assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID, init bool) *avax.UTXO { //nolint:unparam
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
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: outIndex,
		},
		Asset: avax.Asset{ID: assetID},
		Out:   out,
	}
	if init {
		testUTXO.InputID()
	}
	return testUTXO
}

func generateTestOutFromUTXO(utxo *avax.UTXO, depositTxID, bondTxID ids.ID) *avax.TransferableOutput {
	out := utxo.Out
	if lockedOut, ok := out.(*locked.Out); ok {
		out = lockedOut.TransferableOut
	}
	secpOut, ok := out.(*secp256k1fx.TransferOutput)
	if !ok {
		panic("not secp out")
	}
	var innerOut avax.TransferableOut = &secp256k1fx.TransferOutput{
		Amt:          secpOut.Amt,
		OutputOwners: secpOut.OutputOwners,
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		innerOut = &locked.Out{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableOut: innerOut,
		}
	}
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: utxo.AssetID()},
		Out:   innerOut,
	}
}

func generateTestOut(assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID) *avax.TransferableOutput {
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
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: assetID},
		Out:   out,
	}
}

func generateCrossOut(assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, recipient ids.ShortID) *avax.TransferableOutput {
	var out avax.TransferableOut = &secp256k1fx.CrossTransferOutput{
		TransferOutput: secp256k1fx.TransferOutput{
			Amt:          amount,
			OutputOwners: outputOwners,
		},
		Recipient: recipient,
	}
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: assetID},
		Out:   out,
	}
}

func generateTestIn(assetID ids.ID, amount uint64, depositTxID, bondTxID ids.ID, sigIndices []uint32) *avax.TransferableInput {
	var in avax.TransferableIn = &secp256k1fx.TransferInput{
		Amt: amount,
		Input: secp256k1fx.Input{
			SigIndices: sigIndices,
		},
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		in = &locked.In{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableIn: in,
		}
	}
	return &avax.TransferableInput{
		Asset: avax.Asset{ID: assetID},
		In:    in,
	}
}

func generateTestStakeableOut(assetID ids.ID, amount, locktime uint64, outputOwners secp256k1fx.OutputOwners) *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: assetID},
		Out: &stakeable.LockOut{
			Locktime: locktime,
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: outputOwners,
			},
		},
	}
}

func generateTestStakeableIn(assetID ids.ID, amount, locktime uint64, sigIndices []uint32) *avax.TransferableInput {
	return &avax.TransferableInput{
		Asset: avax.Asset{ID: assetID},
		In: &stakeable.LockIn{
			Locktime: locktime,
			TransferableIn: &secp256k1fx.TransferInput{
				Amt: amount,
				Input: secp256k1fx.Input{
					SigIndices: sigIndices,
				},
			},
		},
	}
}

func generateTestInFromUTXO(utxo *avax.UTXO, sigIndices []uint32) *avax.TransferableInput {
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

	// to be sure that utxoid.id is set in both entities
	utxo.InputID()
	return &avax.TransferableInput{
		UTXOID: utxo.UTXOID,
		Asset:  utxo.Asset,
		In:     in,
	}
}

func generateInsFromUTXOs(utxos []*avax.UTXO) []*avax.TransferableInput {
	return generateInsFromUTXOsWithSigIndices(utxos, []uint32{0})
}

func generateInsFromUTXOsWithSigIndices(utxos []*avax.UTXO, sigIndices []uint32) []*avax.TransferableInput {
	ins := make([]*avax.TransferableInput, len(utxos))
	for i := range utxos {
		ins[i] = generateTestInFromUTXO(utxos[i], sigIndices)
	}
	return ins
}

func generateKeyAndOwner(t *testing.T) (*secp256k1.PrivateKey, ids.ShortID, secp256k1fx.OutputOwners) {
	key, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)
	addr := key.Address()
	return key, addr, secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}
}

// msgOwnersWithKeys is created in order to be able to sort both keys and owners by address
type msgOwnersWithKeys struct {
	Owners *secp256k1fx.OutputOwners
	Keys   []*secp256k1.PrivateKey
}

func (mo msgOwnersWithKeys) Len() int {
	return len(mo.Keys)
}

func (mo msgOwnersWithKeys) Swap(i, j int) {
	mo.Owners.Addrs[i], mo.Owners.Addrs[j] = mo.Owners.Addrs[j], mo.Owners.Addrs[i]
	mo.Keys[i], mo.Keys[j] = mo.Keys[j], mo.Keys[i]
}

func (mo msgOwnersWithKeys) Less(i, j int) bool {
	return bytes.Compare(mo.Owners.Addrs[i].Bytes(), mo.Owners.Addrs[j].Bytes()) < 0
}

func generateMsigAliasAndKeys(t *testing.T, threshold, addrsCount uint32, sorted bool) ([]*secp256k1.PrivateKey, *multisig.AliasWithNonce, *secp256k1fx.OutputOwners, *secp256k1fx.OutputOwners) {
	msgOwners := msgOwnersWithKeys{
		Owners: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     make([]ids.ShortID, addrsCount),
		},
		Keys: make([]*secp256k1.PrivateKey, addrsCount),
	}

	for i := uint32(0); i < addrsCount; i++ {
		key, err := testKeyfactory.NewPrivateKey()
		require.NoError(t, err)
		msgOwners.Owners.Addrs[i] = key.Address()
		msgOwners.Keys[i] = key
	}

	if sorted {
		sort.Sort(msgOwners)
	} else {
		sort.Sort(sort.Reverse(msgOwners))
	}

	alias := &multisig.AliasWithNonce{Alias: multisig.Alias{
		ID:     ids.GenerateTestShortID(),
		Owners: msgOwners.Owners,
	}}

	return msgOwners.Keys, alias, msgOwners.Owners, &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{alias.ID},
	}
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

func newCaminoEnvironmentWithMocks(
	caminoGenesisConf api.Camino,
	sharedMemory atomic.SharedMemory,
) *caminoEnvironment {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	vmConfig := defaultCaminoConfig(true)

	clk := defaultClock(true)

	baseDBManager := manager.NewMemDB(version.CurrentDatabase)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	ctx, msm := defaultCtx(baseDB)

	fx := defaultFx(&clk, ctx.Log, isBootstrapped.Get())

	rewards := reward.NewCalculator(vmConfig.RewardConfig)

	defaultState := defaultCaminoState(vmConfig, ctx, baseDB, rewards, caminoGenesisConf)

	if sharedMemory != nil {
		msm = &mutableSharedMemory{
			SharedMemory: sharedMemory,
		}
		ctx.SharedMemory = msm
	}

	atomicUTXOs := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	uptimes := uptime.NewManager(defaultState)
	utxoHandler := utxo.NewCaminoHandler(ctx, &clk, fx, true)

	return &caminoEnvironment{
		isBootstrapped: &isBootstrapped,
		config:         vmConfig,
		clk:            &clk,
		baseDB:         baseDB,
		ctx:            ctx,
		msm:            msm,
		fx:             fx,
		state:          defaultState,
		states:         make(map[ids.ID]state.Chain),
		atomicUTXOs:    atomicUTXOs,
		uptimes:        uptimes,
		utxosHandler:   utxoHandler,
		txBuilder: builder.NewCamino(
			ctx,
			vmConfig,
			&clk,
			fx,
			defaultState,
			atomicUTXOs,
			utxoHandler,
		),
		backend: Backend{
			Config:       vmConfig,
			Ctx:          ctx,
			Clk:          &clk,
			Bootstrapped: &isBootstrapped,
			Fx:           fx,
			FlowChecker:  utxoHandler,
			Uptimes:      uptimes,
			Rewards:      rewards,
		},
	}
}

func expectVerifyMultisigPermission(t *testing.T, s *state.MockDiff, addrs []ids.ShortID, aliases []*multisig.AliasWithNonce) {
	t.Helper()
	expectGetMultisigAliases(t, s, addrs, aliases)
}

func expectGetMultisigAliases(t *testing.T, s *state.MockDiff, addrs []ids.ShortID, aliases []*multisig.AliasWithNonce) {
	t.Helper()
	for i := range addrs {
		var alias *multisig.AliasWithNonce
		if i < len(aliases) {
			alias = aliases[i]
		}
		if alias == nil {
			s.EXPECT().GetMultisigAlias(addrs[i]).Return(nil, database.ErrNotFound)
		} else {
			s.EXPECT().GetMultisigAlias(addrs[i]).Return(alias, nil)
		}
	}
}

func expectVerifyLock(
	t *testing.T,
	s *state.MockDiff,
	ins []*avax.TransferableInput,
	utxos []*avax.UTXO,
	addrs []ids.ShortID,
	aliases []*multisig.AliasWithNonce,
) {
	t.Helper()
	expectGetUTXOsFromInputs(t, s, ins, utxos)
	expectGetMultisigAliases(t, s, addrs, aliases)
}

func expectVerifyUnlockDeposit(
	t *testing.T,
	s *state.MockDiff,
	ins []*avax.TransferableInput,
	utxos []*avax.UTXO,
	addrs []ids.ShortID,
	aliases []*multisig.AliasWithNonce, //nolint:unparam
) {
	t.Helper()
	expectGetUTXOsFromInputs(t, s, ins, utxos)
	expectGetMultisigAliases(t, s, addrs, aliases)
}

// TODO @evlekht seems, that [addrs] actually not affecting anything and could be omitted
func expectUnlock(
	t *testing.T,
	s *state.MockDiff,
	lockTxIDs []ids.ID,
	addrs []ids.ShortID,
	utxos []*avax.UTXO,
	removedLockState locked.State, //nolint:unparam
) {
	t.Helper()
	lockTxIDsSet := set.NewSet[ids.ID](len(lockTxIDs))
	addrsSet := set.NewSet[ids.ShortID](len(addrs))
	lockTxIDsSet.Add(lockTxIDs...)
	addrsSet.Add(addrs...)
	for _, txID := range lockTxIDs {
		s.EXPECT().GetTx(txID).Return(&txs.Tx{
			Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
				Outs: []*avax.TransferableOutput{{
					Out: &locked.Out{
						IDs: locked.IDsEmpty.Lock(removedLockState),
						TransferableOut: &secp256k1fx.TransferOutput{
							OutputOwners: secp256k1fx.OutputOwners{Addrs: addrs},
						},
					},
				}},
			}},
		}, status.Committed, nil)
	}
	s.EXPECT().LockedUTXOs(lockTxIDsSet, addrsSet, removedLockState).Return(utxos, nil)
}

func expectGetUTXOsFromInputs(t *testing.T, s *state.MockDiff, ins []*avax.TransferableInput, utxos []*avax.UTXO) {
	t.Helper()
	for i := range ins {
		if utxos[i] == nil {
			s.EXPECT().GetUTXO(ins[i].InputID()).Return(nil, database.ErrNotFound)
		} else {
			s.EXPECT().GetUTXO(ins[i].InputID()).Return(utxos[i], nil)
		}
	}
}

func expectConsumeUTXOs(t *testing.T, s *state.MockDiff, ins []*avax.TransferableInput) {
	t.Helper()
	for _, in := range ins {
		s.EXPECT().DeleteUTXO(in.InputID())
	}
}

func expectProduceUTXOs(t *testing.T, s *state.MockDiff, outs []*avax.TransferableOutput, txID ids.ID, baseOutIndex int) { //nolint:unparam
	t.Helper()
	for i := range outs {
		s.EXPECT().AddUTXO(&avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(baseOutIndex + i),
			},
			Asset: outs[i].Asset,
			Out:   outs[i].Out,
		})
	}
}

func expectProduceNewlyLockedUTXOs(t *testing.T, s *state.MockDiff, outs []*avax.TransferableOutput, txID ids.ID, baseOutIndex int, lockState locked.State) { //nolint:unparam
	t.Helper()
	for i := range outs {
		out := outs[i].Out
		if lockedOut, ok := out.(*locked.Out); ok {
			utxoLockedOut := *lockedOut
			utxoLockedOut.FixLockID(txID, lockState)
			out = &utxoLockedOut
		}
		s.EXPECT().AddUTXO(&avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(baseOutIndex + i),
			},
			Asset: outs[i].Asset,
			Out:   out,
		})
	}
}

type phase int

const (
	sunrisePhase phase = 0
	athensPhase  phase = 1
	berlinPhase  phase = 2
	firstPhase   phase = sunrisePhase
	lastPhase    phase = berlinPhase
)

func phaseTime(t *testing.T, phase phase, cfg *config.Config) time.Time {
	switch phase {
	case sunrisePhase: // SunrisePhase
		return cfg.AthensPhaseTime.Add(-time.Second)
	case athensPhase:
		return cfg.AthensPhaseTime
	case berlinPhase:
		return cfg.BerlinPhaseTime
	}
	require.FailNow(t, "unknown phase")
	return time.Time{}
}

func phaseName(t *testing.T, phase phase) string {
	switch phase {
	case sunrisePhase:
		return "SunrisePhase"
	case athensPhase:
		return "AthensPhase"
	case berlinPhase:
		return "BerlinPhase"
	}
	require.FailNow(t, "unknown phase")
	return ""
}
