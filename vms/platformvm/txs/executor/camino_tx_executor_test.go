// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	deposits "github.com/ava-labs/avalanchego/vms/platformvm/deposit"

	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestCaminoEnv(t *testing.T) {
	caminoGenesisConf := genesis.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment( /*postBanff*/ false, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		err := shutdownCaminoEnvironment(env)
		require.NoError(t, err)
	}()
	env.config.BanffTime = env.state.GetTimestamp()
}

func TestCaminoStandardTxExecutorAddValidatorTx(t *testing.T) {
	caminoGenesisConf := genesis.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment( /*postBanff*/ true, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownCaminoEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BanffTime = env.state.GetTimestamp()
	nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()
	type args struct {
		stakeAmount   uint64
		startTime     uint64
		endTime       uint64
		nodeID        ids.NodeID
		rewardAddress ids.ShortID
		shares        uint32
		keys          []*crypto.PrivateKeySECP256K1R
		changeAddr    ids.ShortID
	}
	tests := map[string]struct {
		generateArgs func() args
		preExecute   func(*testing.T, *txs.Tx)
		expectedErr  error
	}{
		"Happy path": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Unix()) + 1,
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        nodeID,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], nodeKey},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute:  func(t *testing.T, tx *txs.Tx) {},
			expectedErr: nil,
		},
		"Validator's start time too early": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Unix()) - 1,
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        nodeID,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], nodeKey},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute:  func(t *testing.T, tx *txs.Tx) {},
			expectedErr: errTimestampNotBeforeStartTime,
		},
		"Validator's start time too far in the future": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Add(MaxFutureStartTime).Unix() + 1),
					endTime:       uint64(defaultValidateEndTime.Add(MaxFutureStartTime).Add(defaultMinStakingDuration).Unix() + 1),
					nodeID:        nodeID,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], nodeKey},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute:  func(t *testing.T, tx *txs.Tx) {},
			expectedErr: errFutureStakeTime,
		},
		"Validator already validating primary network": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Unix() + 1),
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        caminoPreFundedNodeIDs[0],
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], caminoPreFundedNodeKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute:  func(t *testing.T, tx *txs.Tx) {},
			expectedErr: errValidatorExists,
		},
		"Validator in pending validator set of primary network": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultGenesisTime.Add(1 * time.Second).Unix()),
					endTime:       uint64(defaultGenesisTime.Add(1 * time.Second).Add(defaultMinStakingDuration).Unix()),
					nodeID:        caminoPreFundedNodeIDs[0],
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], caminoPreFundedNodeKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				staker, err := state.NewCurrentStaker(
					tx.ID(),
					tx.Unsigned.(*txs.CaminoAddValidatorTx),
					0,
				)
				require.NoError(t, err)
				env.state.PutCurrentValidator(staker)
				env.state.AddTx(tx, status.Committed)
				dummyHeight := uint64(1)
				env.state.SetHeight(dummyHeight)
				err = env.state.Commit()
				require.ErrorContains(t, err, errDuplicateValidator.Error())
			},
			expectedErr: errValidatorExists,
		},
		"AddValidatorTx flow check failed": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Unix() + 1),
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        nodeID,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[1], nodeKey},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				utxoIDs, err := env.state.UTXOIDs(caminoPreFundedKeys[1].PublicKey().Address().Bytes(), ids.Empty, math.MaxInt32)
				require.NoError(t, err)
				for _, utxoID := range utxoIDs {
					env.state.DeleteUTXO(utxoID)
				}
			},
			expectedErr: errFlowCheckFailed,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			addValidatorArgs := tt.generateArgs()
			tx, err := env.txBuilder.NewAddValidatorTx(
				addValidatorArgs.stakeAmount,
				addValidatorArgs.startTime,
				addValidatorArgs.endTime,
				addValidatorArgs.nodeID,
				addValidatorArgs.rewardAddress,
				addValidatorArgs.shares,
				addValidatorArgs.keys,
				addValidatorArgs.changeAddr,
			)
			require.NoError(t, err)

			tt.preExecute(t, tx)

			onAcceptState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(t, err)

			executor := CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      tx,
				},
			}
			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorAddSubnetValidatorTx(t *testing.T) {
	caminoGenesisConf := genesis.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment( /*postBanff*/ true, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownCaminoEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BanffTime = env.state.GetTimestamp()
	nodeKey, nodeID := caminoPreFundedNodeKeys[0], caminoPreFundedNodeIDs[0]
	tempNodeKey, tempNodeID := nodeid.GenerateCaminoNodeKeyAndID()

	pendingDSValidatorKey, pendingDSValidatorID := nodeid.GenerateCaminoNodeKeyAndID()
	dsStartTime := defaultGenesisTime.Add(10 * time.Second)
	dsEndTime := dsStartTime.Add(5 * defaultMinStakingDuration)

	// Add `pendingDSValidatorID` as validator to pending set
	addDSTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake,
		uint64(dsStartTime.Unix()),
		uint64(dsEndTime.Unix()),
		pendingDSValidatorID,
		ids.ShortEmpty,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], pendingDSValidatorKey},
		ids.ShortEmpty,
	)
	require.NoError(t, err)
	staker, err := state.NewCurrentStaker(
		addDSTx.ID(),
		addDSTx.Unsigned.(*txs.CaminoAddValidatorTx),
		0,
	)
	require.NoError(t, err)
	env.state.PutCurrentValidator(staker)
	env.state.AddTx(addDSTx, status.Committed)
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	err = env.state.Commit()
	require.NoError(t, err)

	// Add `caminoPreFundedNodeIDs[1]` as subnet validator
	subnetTx, err := env.txBuilder.NewAddSubnetValidatorTx(
		env.config.MinValidatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		caminoPreFundedNodeIDs[1],
		testSubnet1.ID(),
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], caminoPreFundedNodeKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(t, err)
	staker, err = state.NewCurrentStaker(
		subnetTx.ID(),
		subnetTx.Unsigned.(*txs.AddSubnetValidatorTx),
		0,
	)
	require.NoError(t, err)
	env.state.PutCurrentValidator(staker)
	env.state.AddTx(subnetTx, status.Committed)
	env.state.SetHeight(dummyHeight)

	err = env.state.Commit()
	require.NoError(t, err)

	type args struct {
		weight     uint64
		startTime  uint64
		endTime    uint64
		nodeID     ids.NodeID
		subnetID   ids.ID
		keys       []*crypto.PrivateKeySECP256K1R
		changeAddr ids.ShortID
	}
	tests := map[string]struct {
		generateArgs        func() args
		preExecute          func(*testing.T, *txs.Tx)
		expectedSpecificErr error
		// In some checks, avalanche implementation is not returning a specific error or this error is private
		// So in order not to change avalanche files we should just assert that we have some error
		expectedGeneralErr bool
	}{
		"Happy path validator in current set of primary network": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(defaultValidateStartTime.Unix()) + 1,
					endTime:    uint64(defaultValidateEndTime.Unix()),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute:          func(t *testing.T, tx *txs.Tx) {},
			expectedSpecificErr: nil,
		},
		"Validator stops validating subnet after stops validating primary network": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(defaultValidateStartTime.Unix()) + 1,
					endTime:    uint64(defaultValidateEndTime.Unix() + 1),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute:          func(t *testing.T, tx *txs.Tx) {},
			expectedSpecificErr: errValidatorSubset,
		},
		"Validator not in pending or current validator set": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(defaultValidateStartTime.Unix()) + 1,
					endTime:    uint64(defaultValidateEndTime.Unix()),
					nodeID:     tempNodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], tempNodeKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute:          func(t *testing.T, tx *txs.Tx) {},
			expectedSpecificErr: database.ErrNotFound,
		},
		"Validator in pending set but starts validating before primary network": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(dsStartTime.Unix()) - 1,
					endTime:    uint64(dsEndTime.Unix()),
					nodeID:     pendingDSValidatorID,
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], pendingDSValidatorKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute:          func(t *testing.T, tx *txs.Tx) {},
			expectedSpecificErr: errValidatorSubset,
		},
		"Validator in pending set but stops after primary network": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(dsStartTime.Unix()),
					endTime:    uint64(dsEndTime.Unix()) + 1,
					nodeID:     pendingDSValidatorID,
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], pendingDSValidatorKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute:          func(t *testing.T, tx *txs.Tx) {},
			expectedSpecificErr: errValidatorSubset,
		},
		"Happy path validator in pending set": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(dsStartTime.Unix()),
					endTime:    uint64(dsEndTime.Unix()),
					nodeID:     pendingDSValidatorID,
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], pendingDSValidatorKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute:          func(t *testing.T, tx *txs.Tx) {},
			expectedSpecificErr: nil,
		},
		"Validator starts at current timestamp": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(defaultValidateStartTime.Unix()),
					endTime:    uint64(defaultValidateEndTime.Unix()),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute:          func(t *testing.T, tx *txs.Tx) {},
			expectedSpecificErr: errTimestampNotBeforeStartTime,
		},
		"Validator is already a subnet validator": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(defaultValidateStartTime.Unix() + 1),
					endTime:    uint64(defaultValidateEndTime.Unix()),
					nodeID:     caminoPreFundedNodeIDs[1],
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], caminoPreFundedNodeKeys[1]},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute:         func(t *testing.T, tx *txs.Tx) {},
			expectedGeneralErr: true,
		},
		"Too few signatures": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(defaultValidateStartTime.Unix() + 1),
					endTime:    uint64(defaultValidateEndTime.Unix()),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				addSubnetValidatorTx := tx.Unsigned.(*txs.AddSubnetValidatorTx)
				input := addSubnetValidatorTx.SubnetAuth.(*secp256k1fx.Input)
				input.SigIndices = input.SigIndices[1:]
			},
			expectedSpecificErr: errUnauthorizedSubnetModification,
		},
		"Control signature from invalid key": {
			generateArgs: func() args {
				return args{
					weight:     env.config.MinValidatorStake,
					startTime:  uint64(defaultValidateStartTime.Unix() + 1),
					endTime:    uint64(defaultValidateEndTime.Unix()),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				// Replace a valid signature with one from keys[3]
				sig, err := caminoPreFundedKeys[3].SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
				require.NoError(t, err)
				copy(tx.Creds[0].(*secp256k1fx.Credential).Sigs[0][:], sig)
			},
			expectedSpecificErr: errFlowCheckFailed,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			addSubnetValidatorArgs := tt.generateArgs()
			tx, err := env.txBuilder.NewAddSubnetValidatorTx(
				addSubnetValidatorArgs.weight,
				addSubnetValidatorArgs.startTime,
				addSubnetValidatorArgs.endTime,
				addSubnetValidatorArgs.nodeID,
				addSubnetValidatorArgs.subnetID,
				addSubnetValidatorArgs.keys,
				addSubnetValidatorArgs.changeAddr,
			)
			require.NoError(t, err)

			tt.preExecute(t, tx)

			onAcceptState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(t, err)

			executor := CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      tx,
				},
			}
			err = tx.Unsigned.Visit(&executor)
			if tt.expectedGeneralErr {
				require.Error(t, err)
			} else {
				require.ErrorIs(t, err, tt.expectedSpecificErr)
			}
		})
	}
}

func TestCaminoStandardTxExecutorAddValidatorTxBody(t *testing.T) {
	caminoGenesisConf := genesis.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment( /*postBanff*/ true, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownCaminoEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	existingTxID := ids.GenerateTestID()
	env.config.BanffTime = env.state.GetTimestamp()
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}
	inputSigners := []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]}

	tests := map[string]struct {
		utxos       []*avax.UTXO
		outs        []*avax.TransferableOutput
		expectedErr error
	}{
		"Happy path bonding": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, locked.ThisTxID),
			},
			expectedErr: nil,
		},
		"Happy path bonding deposited": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, existingTxID, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, existingTxID, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, existingTxID, locked.ThisTxID),
			},
			expectedErr: nil,
		},
		"Happy path bonding deposited and unlocked": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight/2, outputOwners, existingTxID, ids.Empty),
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight/2-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight/2, outputOwners, ids.Empty, locked.ThisTxID),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight/2, outputOwners, existingTxID, locked.ThisTxID),
			},
			expectedErr: nil,
		},
		"Bonding bonded UTXO": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, existingTxID),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, locked.ThisTxID),
			},
			expectedErr: errFlowCheckFailed,
		},
		"Fee burning bonded UTXO": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, existingTxID),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, locked.ThisTxID),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, existingTxID),
			},
			expectedErr: errFlowCheckFailed,
		},
		"Fee burning deposited UTXO": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultCaminoValidatorWeight, outputOwners, existingTxID, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, existingTxID, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, existingTxID, locked.ThisTxID),
			},
			expectedErr: errFlowCheckFailed,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()

			ins := make([]*avax.TransferableInput, len(tt.utxos))
			signers := make([][]*crypto.PrivateKeySECP256K1R, len(tt.utxos)+1)
			for i, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
				ins[i] = generateTestInFromUTXO(utxo, sigIndices)
				signers[i] = inputSigners
			}
			signers[len(signers)-1] = []*crypto.PrivateKeySECP256K1R{nodeKey}

			avax.SortTransferableInputsWithSigners(ins, signers)
			avax.SortTransferableOutputs(tt.outs, txs.Codec)

			utx := &txs.CaminoAddValidatorTx{
				AddValidatorTx: txs.AddValidatorTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    env.ctx.NetworkID,
						BlockchainID: env.ctx.ChainID,
						Ins:          ins,
						Outs:         tt.outs,
					}},
					Validator: validator.Validator{
						NodeID: nodeID,
						Start:  uint64(defaultValidateStartTime.Unix()) + 1,
						End:    uint64(defaultValidateEndTime.Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					RewardsOwner: &secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.ShortEmpty},
					},
					DelegationShares: reward.PercentDenominator,
				},
			}

			tx, err := txs.NewSigned(utx, txs.Codec, signers)
			require.NoError(t, err)

			onAcceptState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(t, err)

			executor := CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      tx,
				},
			}

			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoAddValidatorTxNodeSig(t *testing.T) {
	nodeKey1, nodeID1 := nodeid.GenerateCaminoNodeKeyAndID()
	nodeKey2, _ := nodeid.GenerateCaminoNodeKeyAndID()

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}
	inputSigners := []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]}

	tests := map[string]struct {
		caminoConfig genesis.Camino
		nodeID       ids.NodeID
		nodeKey      *crypto.PrivateKeySECP256K1R
		utxos        []*avax.UTXO
		outs         []*avax.TransferableOutput
		stakedOuts   []*avax.TransferableOutput
		expectedErr  error
	}{
		"Happy path, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey1,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			stakedOuts: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), outputOwners),
			},
			expectedErr: nil,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey2,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			stakedOuts: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, 1, uint64(defaultMinStakingDuration), outputOwners),
			},
			expectedErr: errNodeSignatureMissing,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit true, VerifyNodeSignature true": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey2,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, locked.ThisTxID),
			},
			expectedErr: errNodeSignatureMissing,
		},
		"Inputs and credentials mismatch, LockModeBondDeposit true, VerifyNodeSignature false": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: false,
				LockModeBondDeposit: true,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey2,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, locked.ThisTxID),
			},
			expectedErr: errFlowCheckFailed,
		},
		"Inputs and credentials mismatch, LockModeBondDeposit false, VerifyNodeSignature false": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: false,
				LockModeBondDeposit: false,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey1,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			stakedOuts: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), outputOwners),
			},
			expectedErr: errFlowCheckFailed,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				if err := shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			env.config.BanffTime = env.state.GetTimestamp()

			ins := make([]*avax.TransferableInput, len(tt.utxos))
			signers := make([][]*crypto.PrivateKeySECP256K1R, len(tt.utxos)+1)
			for i, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
				ins[i] = generateTestInFromUTXO(utxo, sigIndices)
				signers[i] = inputSigners
			}
			signers[len(signers)-1] = []*crypto.PrivateKeySECP256K1R{tt.nodeKey}

			avax.SortTransferableInputsWithSigners(ins, signers)
			avax.SortTransferableOutputs(tt.outs, txs.Codec)

			addValidatorTx := &txs.AddValidatorTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          ins,
					Outs:         tt.outs,
				}},
				Validator: validator.Validator{
					NodeID: tt.nodeID,
					Start:  uint64(defaultValidateStartTime.Unix()) + 1,
					End:    uint64(defaultValidateEndTime.Unix()),
					Wght:   env.config.MinValidatorStake,
				},
				StakeOuts: tt.stakedOuts,
				RewardsOwner: &secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.ShortEmpty},
				},
				DelegationShares: reward.PercentDenominator,
			}

			var utx txs.UnsignedTx = addValidatorTx
			if tt.caminoConfig.LockModeBondDeposit {
				utx = &txs.CaminoAddValidatorTx{AddValidatorTx: *addValidatorTx}
			}

			tx, _ := txs.NewSigned(utx, txs.Codec, signers)
			onAcceptState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(t, err)

			executor := CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      tx,
				},
			}

			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoLockedInsOrLockedOuts(t *testing.T) {
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}

	nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()

	signers := [][]*crypto.PrivateKeySECP256K1R{{caminoPreFundedKeys[0]}}
	signers[len(signers)-1] = []*crypto.PrivateKeySECP256K1R{nodeKey}

	tests := map[string]struct {
		outs         []*avax.TransferableOutput
		ins          []*avax.TransferableInput
		expectedErr  error
		caminoConfig genesis.Camino
	}{
		"Locked out - LockModeBondDeposit: true": {
			outs: []*avax.TransferableOutput{
				generateTestOut(ids.ID{}, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.GenerateTestID()),
			},
			ins:         []*avax.TransferableInput{},
			expectedErr: locked.ErrWrongOutType,
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
		},
		"Locked in - LockModeBondDeposit: true": {
			outs: []*avax.TransferableOutput{},
			ins: []*avax.TransferableInput{
				generateTestIn(ids.ID{}, defaultCaminoValidatorWeight, ids.GenerateTestID(), ids.Empty, sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
		},
		"Locked out - LockModeBondDeposit: false": {
			outs: []*avax.TransferableOutput{
				generateTestOut(ids.ID{}, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.GenerateTestID()),
			},
			ins:         []*avax.TransferableInput{},
			expectedErr: locked.ErrWrongOutType,
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
		},
		"Locked in - LockModeBondDeposit: false": {
			outs: []*avax.TransferableOutput{},
			ins: []*avax.TransferableInput{
				generateTestIn(ids.ID{}, defaultCaminoValidatorWeight, ids.GenerateTestID(), ids.Empty, sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
		},
		"Stakeable out - LockModeBondDeposit: true": {
			outs: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), outputOwners),
			},
			ins:         []*avax.TransferableInput{},
			expectedErr: locked.ErrWrongOutType,
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
		},
		"Stakeable in - LockModeBondDeposit: true": {
			outs: []*avax.TransferableOutput{},
			ins: []*avax.TransferableInput{
				generateTestStakeableIn(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
		},
		"Stakeable out - LockModeBondDeposit: false": {
			outs: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), outputOwners),
			},
			ins:         []*avax.TransferableInput{},
			expectedErr: locked.ErrWrongOutType,
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
		},
		"Stakeable in - LockModeBondDeposit: false": {
			outs: []*avax.TransferableOutput{},
			ins: []*avax.TransferableInput{
				generateTestStakeableIn(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
		},
	}

	generateExecutor := func(unsidngedTx txs.UnsignedTx, env *caminoEnvironment) CaminoStandardTxExecutor {
		tx, err := txs.NewSigned(unsidngedTx, txs.Codec, signers)
		require.NoError(t, err)

		onAcceptState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(t, err)

		executor := CaminoStandardTxExecutor{
			StandardTxExecutor{
				Backend: &env.backend,
				State:   onAcceptState,
				Tx:      tx,
			},
		}

		return executor
	}

	for name, tt := range tests {
		t.Run("ExportTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			exportTx := &txs.ExportTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				DestinationChain: env.ctx.XChainID,
				ExportedOutputs: []*avax.TransferableOutput{
					generateTestOut(env.ctx.AVAXAssetID, defaultMinValidatorStake-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				},
			}

			executor := generateExecutor(exportTx, env)

			err := executor.ExportTx(exportTx)
			require.ErrorIs(t, err, tt.expectedErr)

			exportedOutputsTx := &txs.ExportTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generateTestIn(ids.ID{}, 10, ids.Empty, ids.Empty, sigIndices),
					},
					Outs: []*avax.TransferableOutput{
						generateTestOut(env.ctx.AVAXAssetID, defaultMinValidatorStake-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
					},
				}},
				DestinationChain: env.ctx.XChainID,
				ExportedOutputs: []*avax.TransferableOutput{
					generateTestOut(env.ctx.AVAXAssetID, defaultMinValidatorStake-defaultTxFee, outputOwners, ids.Empty, ids.GenerateTestID()),
				},
			}

			executor = generateExecutor(exportedOutputsTx, env)

			err = executor.ExportTx(exportedOutputsTx)
			require.ErrorIs(t, err, locked.ErrWrongOutType)
		})

		t.Run("ImportTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			importTx := &txs.ImportTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				SourceChain: env.ctx.XChainID,
				ImportedInputs: []*avax.TransferableInput{
					generateTestIn(env.ctx.AVAXAssetID, 10, ids.GenerateTestID(), ids.Empty, sigIndices),
				},
			}

			executor := generateExecutor(importTx, env)

			err := executor.ImportTx(importTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("AddAddressStateTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			addAddressStateTxLockedTx := &txs.AddAddressStateTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				Address: caminoPreFundedKeys[0].PublicKey().Address(),
				State:   uint8(0),
				Remove:  false,
			}

			executor := generateExecutor(addAddressStateTxLockedTx, env)

			err := executor.AddAddressStateTx(addAddressStateTxLockedTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("CreateChainTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			createChainTx := &txs.CreateChainTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				SubnetID:   env.ctx.SubnetID,
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{1}},
			}

			executor := generateExecutor(createChainTx, env)

			err := executor.CreateChainTx(createChainTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("CreateSubnetTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			createSubnetTx := &txs.CreateSubnetTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				Owner: &secp256k1fx.OutputOwners{},
			}

			executor := generateExecutor(createSubnetTx, env)

			err := executor.CreateSubnetTx(createSubnetTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("TransformSubnetTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			transformSubnetTx := &txs.TransformSubnetTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				Subnet:     env.ctx.SubnetID,
				AssetID:    env.ctx.AVAXAssetID,
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{1}},
			}

			executor := generateExecutor(transformSubnetTx, env)

			err := executor.TransformSubnetTx(transformSubnetTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("AddSubnetValidatorTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			addSubnetValidatorTx := &txs.AddSubnetValidatorTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				Validator: validator.SubnetValidator{
					Validator: validator.Validator{
						NodeID: nodeID,
						Start:  uint64(time.Now().Unix()),
						End:    uint64(time.Now().Add(time.Hour).Unix()),
						Wght:   uint64(2022),
					},
					Subnet: env.ctx.SubnetID,
				},
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{1}},
			}

			executor := generateExecutor(addSubnetValidatorTx, env)

			err := executor.AddSubnetValidatorTx(addSubnetValidatorTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("RemoveSubnetValidatorTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			removeSubnetValidatorTx := &txs.RemoveSubnetValidatorTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				Subnet:     env.ctx.SubnetID,
				NodeID:     nodeID,
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{1}},
			}

			executor := generateExecutor(removeSubnetValidatorTx, env)

			err := executor.RemoveSubnetValidatorTx(removeSubnetValidatorTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoAddSubnetValidatorTxNodeSig(t *testing.T) {
	nodeKey1, nodeID1 := caminoPreFundedNodeKeys[0], caminoPreFundedNodeIDs[0]
	nodeKey2 := caminoPreFundedNodeKeys[1]

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}
	inputSigners := []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]}

	tests := map[string]struct {
		caminoConfig genesis.Camino
		nodeID       ids.NodeID
		nodeKey      *crypto.PrivateKeySECP256K1R
		utxos        []*avax.UTXO
		outs         []*avax.TransferableOutput
		stakedOuts   []*avax.TransferableOutput
		expectedErr  error
	}{
		"Happy path, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey1,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			stakedOuts: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), outputOwners),
			},
			expectedErr: nil,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey2,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			stakedOuts: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), outputOwners),
			},
			expectedErr: errNodeSignatureMissing,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit true, VerifyNodeSignature true": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey2,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			expectedErr: errNodeSignatureMissing,
		},
		"Inputs and credentials mismatch, LockModeBondDeposit true, VerifyNodeSignature false": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: false,
				LockModeBondDeposit: true,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey2,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			expectedErr: errUnauthorizedSubnetModification,
		},
		"Inputs and credentials mismatch, LockModeBondDeposit false, VerifyNodeSignature false": {
			caminoConfig: genesis.Camino{
				VerifyNodeSignature: false,
				LockModeBondDeposit: false,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey1,
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight*2, outputOwners, ids.Empty, ids.Empty),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			stakedOuts: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), outputOwners),
			},
			expectedErr: errUnauthorizedSubnetModification,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				if err := shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			env.config.BanffTime = env.state.GetTimestamp()

			ins := make([]*avax.TransferableInput, len(tt.utxos))
			var signers [][]*crypto.PrivateKeySECP256K1R
			for i, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
				ins[i] = generateTestInFromUTXO(utxo, sigIndices)
				signers = append(signers, inputSigners)
			}

			avax.SortTransferableInputsWithSigners(ins, signers)
			avax.SortTransferableOutputs(tt.outs, txs.Codec)

			subnetAuth, subnetSigners, err := env.utxosHandler.Authorize(env.state, testSubnet1.ID(), testCaminoSubnet1ControlKeys)
			require.NoError(t, err)
			signers = append(signers, subnetSigners)
			signers = append(signers, []*crypto.PrivateKeySECP256K1R{tt.nodeKey})

			addSubentValidatorTx := &txs.AddSubnetValidatorTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          ins,
					Outs:         tt.outs,
				}},
				Validator: validator.SubnetValidator{
					Validator: validator.Validator{
						NodeID: tt.nodeID,
						Start:  uint64(defaultValidateStartTime.Unix()) + 1,
						End:    uint64(defaultValidateEndTime.Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					Subnet: testSubnet1.ID(),
				},
				SubnetAuth: subnetAuth,
			}

			var utx txs.UnsignedTx = addSubentValidatorTx
			tx, _ := txs.NewSigned(utx, txs.Codec, signers)
			onAcceptState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(t, err)

			executor := CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      tx,
				},
			}

			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoRewardValidatorTx(t *testing.T) {
	caminoGenesisConf := genesis.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}

	env := newCaminoEnvironment( /*postBanff*/ true, caminoGenesisConf)
	env.ctx.Lock.Lock()
	env.config.BanffTime = env.state.GetTimestamp()

	currentStakerIterator, err := env.state.GetCurrentStakerIterator()
	require.NoError(t, err)
	require.True(t, currentStakerIterator.Next())
	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	stakerToRemoveTxIntf, _, err := env.state.GetTx(stakerToRemove.TxID)
	require.NoError(t, err)
	stakerToRemoveTx := stakerToRemoveTxIntf.Unsigned.(*txs.CaminoAddValidatorTx)
	ins, outs, err := env.utxosHandler.Unlock(env.state, []ids.ID{stakerToRemove.TxID}, locked.StateBonded)
	require.NoError(t, err)

	// UTXOs before reward
	innerOut := stakerToRemoveTx.Outs[0].Out.(*locked.Out)
	stakeOwners := innerOut.TransferableOut.(*secp256k1fx.TransferOutput).AddressesSet()
	utxosBeforeReward, err := avax.GetAllUTXOs(env.state, stakeOwners)
	require.NoError(t, err)

	type test struct {
		ins                      []*avax.TransferableInput
		outs                     []*avax.TransferableOutput
		preExecute               func(*testing.T, *txs.Tx)
		generateUTXOsAfterReward func(ids.ID) []*avax.UTXO
		expectedErr              error
	}

	tests := map[string]test{
		"Reward before end time": {
			ins:        ins,
			outs:       outs,
			preExecute: func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO {
				return utxosBeforeReward
			},
			expectedErr: errRemoveValidatorToEarly,
		},
		"Wrong validator": {
			ins:  ins,
			outs: outs,
			preExecute: func(t *testing.T, tx *txs.Tx) {
				rewValTx := tx.Unsigned.(*txs.CaminoRewardValidatorTx)
				rewValTx.RewardValidatorTx.TxID = ids.GenerateTestID()
			},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO {
				return utxosBeforeReward
			},
			expectedErr: database.ErrNotFound,
		},
		"No zero credentials": {
			ins:  ins,
			outs: outs,
			preExecute: func(t *testing.T, tx *txs.Tx) {
				tx.Creds = append(tx.Creds, &secp256k1fx.Credential{})
			},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO {
				return utxosBeforeReward
			},
			expectedErr: errWrongNumberOfCredentials,
		},
		"Invalid inputs (one excess)": {
			ins:        append(ins, &avax.TransferableInput{In: &secp256k1fx.TransferInput{}}),
			outs:       outs,
			preExecute: func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO {
				return utxosBeforeReward
			},
			expectedErr: errInvalidSystemTxBody,
		},
		"Invalid inputs (wrong amount)": {
			ins: func() []*avax.TransferableInput {
				tempIns := make([]*avax.TransferableInput, len(ins))
				inputLockIDs := locked.IDs{}
				if lockedIn, ok := ins[0].In.(*locked.In); ok {
					inputLockIDs = lockedIn.IDs
				}
				tempIns[0] = &avax.TransferableInput{
					UTXOID: ins[0].UTXOID,
					Asset:  ins[0].Asset,
					In: &locked.In{
						IDs: inputLockIDs,
						TransferableIn: &secp256k1fx.TransferInput{
							Amt: ins[0].In.Amount() - 1,
						},
					},
				}
				return tempIns
			}(),
			outs:       outs,
			preExecute: func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO {
				return utxosBeforeReward
			},
			expectedErr: errInvalidSystemTxBody,
		},
		"Invalid outs (one excess)": {
			ins:        ins,
			outs:       append(outs, &avax.TransferableOutput{Out: &secp256k1fx.TransferOutput{}}),
			preExecute: func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO {
				return utxosBeforeReward
			},
			expectedErr: errInvalidSystemTxBody,
		},
		"Invalid outs (wrong amount)": {
			ins: ins,
			outs: func() []*avax.TransferableOutput {
				tempOuts := make([]*avax.TransferableOutput, len(outs))
				copy(tempOuts, outs)
				validOut := tempOuts[0].Out
				if lockedOut, ok := validOut.(*locked.Out); ok {
					validOut = lockedOut.TransferableOut
				}
				secpOut, ok := validOut.(*secp256k1fx.TransferOutput)
				require.True(t, ok)

				var invalidOut avax.TransferableOut = &secp256k1fx.TransferOutput{
					Amt:          secpOut.Amt - 1,
					OutputOwners: secpOut.OutputOwners,
				}
				if lockedOut, ok := validOut.(*locked.Out); ok {
					invalidOut = &locked.Out{
						IDs:             lockedOut.IDs,
						TransferableOut: invalidOut,
					}
				}
				tempOuts[0] = &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out:   invalidOut,
				}
				return tempOuts
			}(),
			preExecute: func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO {
				return utxosBeforeReward
			},
			expectedErr: errInvalidSystemTxBody,
		},
	}

	execute := func(t *testing.T, tt test) (CaminoProposalTxExecutor, *txs.Tx) {
		utx := &txs.CaminoRewardValidatorTx{
			RewardValidatorTx: txs.RewardValidatorTx{TxID: stakerToRemove.TxID},
			Ins:               tt.ins,
			Outs:              tt.outs,
		}

		tx, err := txs.NewSigned(utx, txs.Codec, nil)
		require.NoError(t, err)

		tt.preExecute(t, tx)

		onCommitState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(t, err)

		onAbortState, err := state.NewDiff(lastAcceptedID, env)
		require.NoError(t, err)

		txExecutor := CaminoProposalTxExecutor{
			ProposalTxExecutor{
				OnCommitState: onCommitState,
				OnAbortState:  onAbortState,
				Backend:       &env.backend,
				Tx:            tx,
			},
		}
		err = tx.Unsigned.Visit(&txExecutor)
		require.ErrorIs(t, err, tt.expectedErr)
		return txExecutor, tx
	}

	// Asserting UTXO changes
	assertBalance := func(t *testing.T, tt test, tx *txs.Tx) {
		onCommitUTXOs, err := avax.GetAllUTXOs(env.state, outputOwners.AddressesSet())
		require.NoError(t, err)
		utxosAfterReward := tt.generateUTXOsAfterReward(tx.ID())
		require.Equal(t, onCommitUTXOs, utxosAfterReward)
	}

	// Asserting that staker is removed
	assertNextStaker := func(t *testing.T) {
		nextStakerIterator, err := env.state.GetCurrentStakerIterator()
		require.NoError(t, err)
		require.True(t, nextStakerIterator.Next())
		nextStakerToRemove := nextStakerIterator.Value()
		nextStakerIterator.Release()
		require.NotEqual(t, nextStakerToRemove.TxID, stakerToRemove.TxID)
	}

	for name, tt := range tests {
		t.Run(name+" On abort", func(t *testing.T) {
			txExecutor, tx := execute(t, tt)
			txExecutor.OnAbortState.Apply(env.state)
			env.state.SetHeight(uint64(1))
			err = env.state.Commit()
			require.NoError(t, err)
			assertBalance(t, tt, tx)
		})
		t.Run(name+" On commit", func(t *testing.T) {
			txExecutor, tx := execute(t, tt)
			txExecutor.OnCommitState.Apply(env.state)
			env.state.SetHeight(uint64(1))
			err = env.state.Commit()
			require.NoError(t, err)
			assertBalance(t, tt, tx)
		})
	}

	happyPathTest := test{
		ins:  ins,
		outs: outs,
		preExecute: func(t *testing.T, tx *txs.Tx) {
			env.state.SetTimestamp(stakerToRemove.EndTime)
		},
		generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO {
			return []*avax.UTXO{
				generateTestUTXO(txID, env.ctx.AVAXAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.Empty, env.ctx.AVAXAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			}
		},
		expectedErr: nil,
	}

	t.Run("Happy path on commit", func(t *testing.T) {
		txExecutor, tx := execute(t, happyPathTest)
		txExecutor.OnCommitState.Apply(env.state)
		env.state.SetHeight(uint64(1))
		err = env.state.Commit()
		require.NoError(t, err)
		assertBalance(t, happyPathTest, tx)
		assertNextStaker(t)
	})

	// We need to start again the environment because the staker is already removed from the previous test
	env = newCaminoEnvironment( /*postBanff*/ true, caminoGenesisConf)
	env.ctx.Lock.Lock()
	env.config.BanffTime = env.state.GetTimestamp()

	t.Run("Happy path on abort", func(t *testing.T) {
		txExecutor, tx := execute(t, happyPathTest)
		txExecutor.OnAbortState.Apply(env.state)
		env.state.SetHeight(uint64(1))
		err = env.state.Commit()
		require.NoError(t, err)
		assertBalance(t, happyPathTest, tx)
		assertNextStaker(t)
	})

	// Shut down the environment
	err = shutdownCaminoEnvironment(env)
	require.NoError(t, err)
}

func TestAddAdressStateTxExecutor(t *testing.T) {
	caminoGenesisConf := genesis.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	env := newCaminoEnvironment( /*postBanff*/ true, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		err := shutdownCaminoEnvironment(env)
		require.NoError(t, err)
	}()

	signers := [][]*crypto.PrivateKeySECP256K1R{{caminoPreFundedKeys[0]}}

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}

	tests := map[string]struct {
		stateAddress  ids.ShortID
		targetAddress ids.ShortID
		flag          uint64
		state         uint8
		expectedErr   error
		remove        bool
	}{
		"Flag: AddressStateRoleAdmin": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          uint64(txs.AddressStateRoleAdmin),
			state:         txs.AddressStateRoleAdmin,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		"Flag: AddressStateRoleAdminBit": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateRoleAdminBit,
			state:         txs.AddressStateRoleAdmin,
			expectedErr:   nil,
			remove:        false,
		},
		"Flag: AddressStateRoleKyc Add": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          uint64(txs.AddressStateRoleKyc),
			state:         txs.AddressStateRoleKyc,
			expectedErr:   nil,
			remove:        false,
		},
		"Flag: AddressStateRoleKyc Remove": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          uint64(txs.AddressStateRoleKyc),
			state:         txs.AddressStateRoleKyc,
			expectedErr:   nil,
			remove:        true,
		},
		"Flag: AddressStateRoleKyc Add, Different Target Address": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[2].PublicKey().Address(),
			flag:          uint64(txs.AddressStateRoleKyc),
			state:         txs.AddressStateRoleKyc,
			expectedErr:   nil,
			remove:        false,
		},
		"Flag: AddressStateRoleKycBit Add": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateRoleKycBit,
			state:         txs.AddressStateRoleAdmin,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		"Flag: AddressStateRoleBits Add": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateRoleBits,
			state:         txs.AddressStateRoleAdmin,
			expectedErr:   nil,
			remove:        false,
		},
		"Flag: AddressStateRoleBits Remove": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateRoleBits,
			state:         txs.AddressStateRoleAdmin,
			expectedErr:   nil,
			remove:        true,
		},
		"Flag: AddressStateRoleBits Add, Different Target Address": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[2].PublicKey().Address(),
			flag:          txs.AddressStateRoleBits,
			state:         txs.AddressStateRoleAdmin,
			expectedErr:   nil,
			remove:        false,
		},
		"Flag: AddressStateKycVerified": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[2].PublicKey().Address(),
			flag:          txs.AddressStateKycVerified,
			state:         txs.AddressStateRoleAdmin,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		"Flag: AddressStateKycExpired Add": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateKycExpired,
			state:         txs.AddressStateRoleKyc,
			expectedErr:   nil,
			remove:        false,
		},
		"Flag: AddressStateKycExpired Add, Different Target Address": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[2].PublicKey().Address(),
			flag:          txs.AddressStateKycExpired,
			state:         txs.AddressStateRoleKyc,
			expectedErr:   nil,
			remove:        false,
		},
		"Flag: AddressStateKycExpired Remove": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateKycExpired,
			state:         txs.AddressStateRoleKyc,
			expectedErr:   nil,
			remove:        true,
		},
		"Flag: AddressStateKycBits": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateKycBits,
			state:         txs.AddressStateRoleKyc,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		"Flag: AddressStateKycBits, Different Target Address": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[2].PublicKey().Address(),
			flag:          txs.AddressStateKycBits,
			state:         txs.AddressStateRoleKyc,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		"Flag: AddressStateMax Add": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateMax,
			state:         txs.AddressStateRoleAdmin,
			expectedErr:   nil,
			remove:        false,
		},
		"Flag: AddressStateMax Remove": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateMax,
			state:         txs.AddressStateRoleAdmin,
			expectedErr:   nil,
			remove:        true,
		},
		"Flag: AddressStateValidBits Add": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateValidBits,
			state:         txs.AddressStateRoleValidator,
			expectedErr:   nil,
			remove:        false,
		},
		"Flag: AddressStateValidBits Remove": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          txs.AddressStateValidBits,
			state:         txs.AddressStateRoleValidator,
			expectedErr:   nil,
			remove:        true,
		},
		"Flag: AddressStateRoleAdmin Add, State: AddressStateKycExpired": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          uint64(txs.AddressStateRoleAdmin),
			state:         txs.AddressStateKycExpired,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		"Flag: AddressStateRoleKyc Add, State: AddressStateRoleValidatorBit": {
			stateAddress:  caminoPreFundedKeys[0].PublicKey().Address(),
			targetAddress: caminoPreFundedKeys[0].PublicKey().Address(),
			flag:          uint64(txs.AddressStateRoleKyc),
			state:         uint8(txs.AddressStateRoleValidatorBit),
			expectedErr:   txs.ErrInvalidState,
			remove:        false,
		},
		"Wrong address": {
			stateAddress:  ids.GenerateTestShortID(),
			targetAddress: ids.GenerateTestShortID(),
			flag:          uint64(txs.AddressStateRoleKyc),
			state:         txs.AddressStateRoleKyc,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		"Empty address": {
			stateAddress:  ids.ShortEmpty,
			targetAddress: ids.ShortEmpty,
			flag:          uint64(txs.AddressStateRoleKyc),
			state:         txs.AddressStateRoleKyc,
			expectedErr:   txs.ErrEmptyAddress,
			remove:        false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			addAddressStateTx := &txs.AddAddressStateTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generateTestIn(avaxAssetID, defaultCaminoBalance, ids.Empty, ids.Empty, sigIndices),
					},
					Outs: []*avax.TransferableOutput{
						generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
					},
				}},
				Address: tt.stateAddress,
				State:   tt.state,
				Remove:  tt.remove,
			}

			tx, err := txs.NewSigned(addAddressStateTx, txs.Codec, signers)
			require.NoError(t, err)

			onAcceptState, err := state.NewCaminoDiff(lastAcceptedID, env)
			require.NoError(t, err)

			executor := CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      tx,
				},
			}

			executor.State.SetAddressStates(tt.stateAddress, tt.flag)
			executor.State.SetAddressStates(tt.targetAddress, tt.flag)

			err = addAddressStateTx.Visit(&executor)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorDepositTx(t *testing.T) {
	currentTime := time.Now()

	testDepositOffer := genesis.DepositOffer{
		InterestRateNominator:   0,
		Start:                   uint64(currentTime.Add(-60 * time.Hour).Unix()),
		End:                     uint64(currentTime.Add(+60 * time.Hour).Unix()),
		MinAmount:               1,
		MinDuration:             60,
		MaxDuration:             60,
		UnlockPeriodDuration:    60,
		NoRewardsPeriodDuration: 0,
	}

	testKey, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)
	dummyKey, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)
	dummyOutputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{dummyKey.PublicKey().Address()},
	}

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{testKey.PublicKey().Address()},
	}
	sigIndices := []uint32{0}
	inputSigners := []*crypto.PrivateKeySECP256K1R{testKey.(*crypto.PrivateKeySECP256K1R)}
	existingTxID := ids.GenerateTestID()

	tests := map[string]struct {
		caminoGenesisConf genesis.Camino
		utxos             []*avax.UTXO
		generateIns       func([]*avax.UTXO) []*avax.TransferableInput
		signers           [][]*crypto.PrivateKeySECP256K1R
		outs              []*avax.TransferableOutput
		depositOfferID    func(caminoEnvironment) ids.ID
		expectedErr       error
	}{
		"Wrong lockModeBondDeposit flag": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
				DepositOffers:       []genesis.DepositOffer{testDepositOffer},
			},
			utxos:          []*avax.UTXO{},
			generateIns:    noInputs,
			outs:           []*avax.TransferableOutput{},
			depositOfferID: noOffers,
			expectedErr:    errWrongLockMode,
		},
		"Stakeable ins": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []genesis.DepositOffer{testDepositOffer},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestStakeableIn(avaxAssetID, defaultCaminoBalance, uint64(defaultMinStakingDuration), sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakeable outs": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []genesis.DepositOffer{testDepositOffer},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, defaultCaminoBalance, uint64(defaultMinStakingDuration), outputOwners),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: locked.ErrWrongOutType,
		},
		"Inputs and utxos length mismatch": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []genesis.DepositOffer{testDepositOffer},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errFlowCheckFailed,
		},
		"Inputs and credentials length mismatch": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []genesis.DepositOffer{testDepositOffer},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errFlowCheckFailed,
		},
		"Not existing deposit offer ID": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []genesis.DepositOffer{testDepositOffer},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				return ids.GenerateTestID()
			},
			expectedErr: database.ErrNotFound,
		},
		"Deposit is not active yet": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					{
						InterestRateNominator:   0,
						Start:                   uint64(time.Now().Add(+60 * time.Hour).Unix()),
						End:                     uint64(time.Now().Add(+60 * time.Hour).Unix()),
						MinAmount:               1,
						MinDuration:             60,
						MaxDuration:             60,
						UnlockPeriodDuration:    60,
						NoRewardsPeriodDuration: 0,
					},
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errDepositOfferNotActiveYet,
		},
		"Deposit offer has expired": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					{
						InterestRateNominator:   0,
						Start:                   uint64(time.Now().Add(-60 * time.Hour).Unix()),
						End:                     uint64(time.Now().Add(-60 * time.Hour).Unix()),
						MinAmount:               1,
						MinDuration:             60,
						MaxDuration:             60,
						UnlockPeriodDuration:    60,
						NoRewardsPeriodDuration: 0,
					},
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errDepositOfferInactive,
		},
		"Deposit's duration is too small": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					{
						InterestRateNominator:   0,
						Start:                   uint64(time.Now().Add(-60 * time.Hour).Unix()),
						End:                     uint64(time.Now().Add(+60 * time.Hour).Unix()),
						MinAmount:               1,
						MinDuration:             100,
						MaxDuration:             100,
						UnlockPeriodDuration:    60,
						NoRewardsPeriodDuration: 40,
					},
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errDepositDurationToSmall,
		},
		"Deposit's duration is too big": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					{
						InterestRateNominator:   0,
						Start:                   uint64(time.Now().Add(-60 * time.Hour).Unix()),
						End:                     uint64(time.Now().Add(+60 * time.Hour).Unix()),
						MinAmount:               1,
						MinDuration:             60,
						MaxDuration:             30,
						UnlockPeriodDuration:    60,
						NoRewardsPeriodDuration: 0,
					},
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errDepositDurationToBig,
		},
		"Deposit's amount is too small": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					{
						InterestRateNominator:   0,
						Start:                   uint64(time.Now().Add(-60 * time.Hour).Unix()),
						End:                     uint64(time.Now().Add(+60 * time.Hour).Unix()),
						MinAmount:               defaultCaminoValidatorWeight * 2,
						MinDuration:             60,
						MaxDuration:             60,
						UnlockPeriodDuration:    60,
						NoRewardsPeriodDuration: 0,
					},
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errDepositToSmall,
		},
		"No fee burning": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					testDepositOffer,
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, existingTxID),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, existingTxID),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errFlowCheckFailed,
		},
		"Deposit already deposited amount": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					testDepositOffer,
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultCaminoBalance, outputOwners, existingTxID, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, existingTxID),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errFlowCheckFailed,
		},
		"Deposit amount of not owned utxos": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					testDepositOffer,
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, dummyOutputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errFlowCheckFailed,
		},
		"Not enough balance to deposit": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					testDepositOffer,
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errFlowCheckFailed,
		},
		"Supply overflow": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					{
						InterestRateNominator:   1000 * units.MegaAvax,
						Start:                   uint64(time.Now().Add(-60 * time.Hour).Unix()),
						End:                     uint64(time.Now().Add(+60 * time.Hour).Unix()),
						MinAmount:               1,
						MinDuration:             60,
						MaxDuration:             60,
						UnlockPeriodDuration:    60,
						NoRewardsPeriodDuration: 0,
					},
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: errSupplyOverflow,
		},
		"Happy path deposit unlocked": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					testDepositOffer,
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: nil,
		},
		"Happy path deposit unlocked, fee change to new address": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					testDepositOffer,
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance+10, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, 10, dummyOutputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: nil,
		},
		"Happy path deposit bonded": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					testDepositOffer,
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, existingTxID),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, locked.ThisTxID, existingTxID),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: nil,
		},
		"Happy path deposit bonded and unlocked": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					testDepositOffer,
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultTxFee+defaultCaminoValidatorWeight/2, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultCaminoValidatorWeight/2, outputOwners, ids.Empty, existingTxID),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight/2, outputOwners, locked.ThisTxID, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight/2, outputOwners, locked.ThisTxID, existingTxID),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: nil,
		},
		"Happy path, deposited amount transferred to another owner": {
			caminoGenesisConf: genesis.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []genesis.DepositOffer{
					testDepositOffer,
				},
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoBalance, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoBalance-defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, dummyOutputOwners, locked.ThisTxID, ids.Empty),
			},
			depositOfferID: func(env caminoEnvironment) ids.ID {
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				return genesisOffers[0].ID
			},
			expectedErr: nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, tt.caminoGenesisConf)
			env.ctx.Lock.Lock()
			defer func() {
				if err := shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			env.config.BanffTime = env.state.GetTimestamp()
			env.state.SetTimestamp(time.Now())

			for _, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
			}

			err := env.state.Commit()
			require.NoError(t, err)
			ins := tt.generateIns(tt.utxos)

			avax.SortTransferableInputsWithSigners(ins, tt.signers)
			avax.SortTransferableOutputs(tt.outs, txs.Codec)

			utx := &txs.DepositTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          ins,
					Outs:         tt.outs,
				}},
				DepositOfferID: tt.depositOfferID(*env),
				Duration:       60,
				RewardsOwner: &secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.ShortEmpty},
				},
			}

			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			onAcceptState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(t, err)

			executor := CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      tx,
				},
			}

			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorUnlockDepositTx(t *testing.T) {
	testUndepositAmount := uint64(10000)
	testKey, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)
	dummyKey, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{testKey.PublicKey().Address()},
	}
	dummyOutputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{dummyKey.PublicKey().Address()},
	}
	sigIndices := []uint32{0}
	inputSigners := []*crypto.PrivateKeySECP256K1R{testKey.(*crypto.PrivateKeySECP256K1R)}
	existingTxID := ids.GenerateTestID()
	depositTxID := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()
	depositStartTime := time.Now()

	depositExpiredTime := depositStartTime.Add(100 * time.Second)
	depositEndedTime := depositStartTime.Add(80 * time.Second)
	depositNotEndedTime := depositStartTime.Add(50 * time.Second)
	genesisDepositOffer := genesis.DepositOffer{
		InterestRateNominator:   0,
		Start:                   uint64(time.Now().Add(-60 * time.Hour).Unix()),
		End:                     uint64(time.Now().Add(+60 * time.Hour).Unix()),
		MinAmount:               1,
		MinDuration:             60,
		MaxDuration:             100,
		UnlockPeriodDuration:    60,
		NoRewardsPeriodDuration: 0,
	}
	caminoGenesisConf := genesis.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		DepositOffers: []genesis.DepositOffer{
			genesisDepositOffer,
		},
	}
	depositOffer := &deposits.Offer{
		InterestRateNominator:   0,
		Start:                   uint64(time.Now().Add(-60 * time.Hour).Unix()),
		End:                     uint64(time.Now().Add(+60 * time.Hour).Unix()),
		MinAmount:               1,
		MinDuration:             60,
		MaxDuration:             100,
		UnlockPeriodDuration:    60,
		NoRewardsPeriodDuration: 0,
	}
	deposit := &deposits.Deposit{
		Duration: 60,
		Amount:   defaultCaminoValidatorWeight,
		Start:    uint64(depositStartTime.Unix()),
	}

	tests := map[string]struct {
		utxos       []*avax.UTXO
		generateIns func([]*avax.UTXO) []*avax.TransferableInput
		signers     [][]*crypto.PrivateKeySECP256K1R
		outs        []*avax.TransferableOutput
		preExecute  func(env caminoEnvironment)
		expectedErr error
	}{
		"Stakeable ins": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestStakeableIn(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), sigIndices),
				}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs:        []*avax.TransferableOutput{},
			preExecute:  func(env caminoEnvironment) {},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakeable outs": {
			utxos:       []*avax.UTXO{},
			generateIns: noInputs,
			outs: []*avax.TransferableOutput{
				generateTestStakeableOut(avaxAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), outputOwners),
			},
			preExecute:  func(env caminoEnvironment) {},
			expectedErr: locked.ErrWrongOutType,
		},
		"Inputs and utxos length mismatch": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
					generateTestIn(avaxAssetID, 10, ids.Empty, ids.Empty, sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners, inputSigners, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositExpiredTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock bonded UTXOs": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, existingTxID),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute:  func(env caminoEnvironment) {},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock deposited UTXOs but with unlocked ins": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				in := generateTestInFromUTXO(utxos[0], sigIndices)
				innerIn := &secp256k1fx.TransferInput{
					Amt:   in.In.Amount(),
					Input: secp256k1fx.Input{SigIndices: sigIndices},
				}
				in.In = innerIn
				return []*avax.TransferableInput{in, generateTestInFromUTXO(utxos[1], sigIndices)}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute:  func(env caminoEnvironment) {},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock deposited UTXOs but with bonded ins": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				in := generateTestInFromUTXO(utxos[0], sigIndices)
				out := utxos[0].Out.(*locked.Out)
				innerIn := &locked.In{
					IDs: locked.IDs{BondTxID: existingTxID},
					TransferableIn: &secp256k1fx.TransferInput{
						Amt:   out.Amount(),
						Input: secp256k1fx.Input{SigIndices: sigIndices},
					},
				}
				in.In = innerIn
				return []*avax.TransferableInput{in, generateTestInFromUTXO(utxos[1], sigIndices)}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute:  func(env caminoEnvironment) {},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount, before deposit's half period": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, 1, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-1, outputOwners, depositTxID, ids.Empty),
			},
			preExecute:  func(env caminoEnvironment) {},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount, deposit expired": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-1, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, 1, outputOwners, depositTxID, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositExpiredTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount of not owned utxos, deposit not ended": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, dummyOutputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, testUndepositAmount, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-testUndepositAmount, outputOwners, depositTxID, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositNotEndedTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount, utxos and input amount mismatch, deposit ended but not expired": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				in := generateTestInFromUTXO(utxos[0], sigIndices)
				out := utxos[0].Out.(*locked.Out)
				innerIn := &locked.In{
					IDs: utxos[0].Out.(*locked.Out).IDs,
					TransferableIn: &secp256k1fx.TransferInput{
						Amt:   out.Amount() + 1,
						Input: secp256k1fx.Input{SigIndices: sigIndices},
					},
				}
				in.In = innerIn
				return []*avax.TransferableInput{in}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, testUndepositAmount, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-testUndepositAmount, outputOwners, depositTxID, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositEndedTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount, deposit not ended": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: func() []*avax.TransferableOutput {
				unlockableAmount := deposit.UnlockableAmount(depositOffer, uint64(depositNotEndedTime.Unix()))
				return []*avax.TransferableOutput{
					generateTestOut(avaxAssetID, unlockableAmount+1, outputOwners, ids.Empty, ids.Empty),
					generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-unlockableAmount-1, outputOwners, depositTxID, ids.Empty),
				}
			}(),
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositNotEndedTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount, deposit ended but not expired": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: func() []*avax.TransferableOutput {
				unlockableAmount := deposit.UnlockableAmount(depositOffer, uint64(depositEndedTime.Unix()))
				return []*avax.TransferableOutput{
					generateTestOut(avaxAssetID, unlockableAmount+1, outputOwners, ids.Empty, ids.Empty),
					generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-unlockableAmount-1, outputOwners, depositTxID, ids.Empty),
				}
			}(),
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositEndedTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"One deposit, two utxos with diff owners, consumed 1.5 utxo < unlockable ,produced as owner1": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, testUndepositAmount, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultCaminoValidatorWeight, dummyOutputOwners, depositTxID2, ids.Empty),
				generateTestUTXO(ids.ID{3}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
					generateTestInFromUTXO(utxos[2], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, {}, inputSigners},
			outs: func() []*avax.TransferableOutput {
				unlockableAmount := deposit.UnlockableAmount(depositOffer, uint64(depositNotEndedTime.Unix()))
				return []*avax.TransferableOutput{
					generateTestOut(avaxAssetID, unlockableAmount, outputOwners, ids.Empty, ids.Empty),
					generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-(unlockableAmount-testUndepositAmount), dummyOutputOwners, depositTxID2, ids.Empty),
				}
			}(),
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositNotEndedTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock all amount of not owned utxos, deposit expired": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, dummyOutputOwners, depositTxID, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositStartTime.Add(120 * time.Second))
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock all amount, utxos and input amount mismatch, deposit expired": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				in := generateTestInFromUTXO(utxos[0], sigIndices)
				out := utxos[0].Out.(*locked.Out)
				innerIn := &locked.In{
					IDs: utxos[0].Out.(*locked.Out).IDs,
					TransferableIn: &secp256k1fx.TransferInput{
						Amt:   out.Amount() + 1,
						Input: secp256k1fx.Input{SigIndices: sigIndices},
					},
				}
				in.In = innerIn
				return []*avax.TransferableInput{in}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositStartTime.Add(120 * time.Second))
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock all amount but also consume bonded utxo, deposit expired": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{1}, avaxAssetID, 10, outputOwners, ids.Empty, existingTxID),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, 10, outputOwners, ids.Empty, existingTxID),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositStartTime.Add(120 * time.Second))
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock deposit, one expired-not-owned and one active deposit": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, dummyOutputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID2, ids.Empty),
				generateTestUTXO(ids.ID{3}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
					generateTestInFromUTXO(utxos[2], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, {}, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, testUndepositAmount, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-testUndepositAmount, outputOwners, depositTxID2, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositExpiredTime)
				// Add a second deposit to state
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				deposit := &deposits.Deposit{
					DepositOfferID: genesisOffers[0].ID,
					Duration:       100,
					Amount:         defaultCaminoValidatorWeight,
					Start:          uint64(depositStartTime.Unix()),
				}
				env.state.UpdateDeposit(depositTxID2, deposit)
				err = env.state.Commit()
				require.NoError(t, err)
			},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock deposit, one expired and one active-not-owned deposit": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultCaminoValidatorWeight, dummyOutputOwners, depositTxID2, ids.Empty),
				generateTestUTXO(ids.ID{3}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
					generateTestInFromUTXO(utxos[2], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, {}, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, testUndepositAmount, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-testUndepositAmount, outputOwners, depositTxID2, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositExpiredTime)
				// Add a second deposit to state
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				deposit := &deposits.Deposit{
					DepositOfferID: genesisOffers[0].ID,
					Duration:       100,
					Amount:         defaultCaminoValidatorWeight,
					Start:          uint64(depositStartTime.Unix()),
				}
				env.state.UpdateDeposit(depositTxID2, deposit)
				err = env.state.Commit()
				require.NoError(t, err)
			},
			expectedErr: errFlowCheckFailed,
		},
		"Producing more than consumed": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, testUndepositAmount, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, testUndepositAmount+1, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositNotEndedTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"No fee burning inputs are unlocked": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositExpiredTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"No fee burning inputs are deposited": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-testUndepositAmount, outputOwners, depositTxID, ids.Empty),
				generateTestOut(avaxAssetID, testUndepositAmount, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositEndedTime)
			},
			expectedErr: errFlowCheckFailed,
		},
		"Happy path burn only fees": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{inputSigners, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute:  func(env caminoEnvironment) {},
			expectedErr: nil,
		},
		"Happy path unlock all amount with creds provided, deposit expired": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositExpiredTime)
			},
			expectedErr: nil,
		},
		"Happy path unlock all amount, deposit expired": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositStartTime.Add(120 * time.Second))
			},
			expectedErr: nil,
		},
		"Happy path unlock some amount, deposit ended but not expired": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: func() []*avax.TransferableOutput {
				unlockableAmount := deposit.UnlockableAmount(depositOffer, uint64(depositEndedTime.Unix()))
				return []*avax.TransferableOutput{
					generateTestOut(avaxAssetID, unlockableAmount, outputOwners, ids.Empty, ids.Empty),
					generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-unlockableAmount, outputOwners, depositTxID, ids.Empty),
				}
			}(),
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositEndedTime)
			},
			expectedErr: nil,
		},
		"Happy path unlock some amount, deposit not ended": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: func() []*avax.TransferableOutput {
				unlockableAmount := deposit.UnlockableAmount(depositOffer, uint64(depositNotEndedTime.Unix()))
				return []*avax.TransferableOutput{
					generateTestOut(avaxAssetID, unlockableAmount, outputOwners, ids.Empty, ids.Empty),
					generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-unlockableAmount, outputOwners, depositTxID, ids.Empty),
				}
			}(),
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositNotEndedTime)
			},
			expectedErr: nil,
		},
		"Happy path unlock some amount, deposit not ended, fee change to new address": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultTxFee+10, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, inputSigners},
			outs: func() []*avax.TransferableOutput {
				unlockableAmount := deposit.UnlockableAmount(depositOffer, uint64(depositNotEndedTime.Unix()))
				return []*avax.TransferableOutput{
					generateTestOut(avaxAssetID, 10, dummyOutputOwners, ids.Empty, ids.Empty),
					generateTestOut(avaxAssetID, unlockableAmount, outputOwners, ids.Empty, ids.Empty),
					generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-unlockableAmount, outputOwners, depositTxID, ids.Empty),
				}
			}(),
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositNotEndedTime)
			},
			expectedErr: nil,
		},
		"Happy path unlock deposit, one expired deposit and one active": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID, ids.Empty),
				generateTestUTXO(ids.ID{2}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, depositTxID2, ids.Empty),
				generateTestUTXO(ids.ID{3}, avaxAssetID, defaultTxFee, outputOwners, ids.Empty, ids.Empty),
			},
			generateIns: func(utxos []*avax.UTXO) []*avax.TransferableInput {
				return []*avax.TransferableInput{
					generateTestInFromUTXO(utxos[0], sigIndices),
					generateTestInFromUTXO(utxos[1], sigIndices),
					generateTestInFromUTXO(utxos[2], sigIndices),
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, {}, inputSigners},
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, testUndepositAmount, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight-testUndepositAmount, outputOwners, depositTxID2, ids.Empty),
			},
			preExecute: func(env caminoEnvironment) {
				env.state.SetTimestamp(depositExpiredTime)
				// Add a second deposit to state
				genesisOffers, err := env.state.GetAllDepositOffers()
				require.NoError(t, err)
				deposit := &deposits.Deposit{
					DepositOfferID: genesisOffers[0].ID,
					Duration:       100,
					Amount:         defaultCaminoValidatorWeight,
					Start:          uint64(depositStartTime.Unix()),
				}
				env.state.UpdateDeposit(depositTxID2, deposit)
				err = env.state.Commit()
				require.NoError(t, err)
			},
			expectedErr: nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, caminoGenesisConf)
			env.ctx.Lock.Lock()
			defer func() {
				if err = shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			env.config.BanffTime = env.state.GetTimestamp()
			env.state.SetTimestamp(depositStartTime)

			genesisOffers, err := env.state.GetAllDepositOffers()
			require.NoError(t, err)

			// Add a deposit to state
			deposit.DepositOfferID = genesisOffers[0].ID
			env.state.UpdateDeposit(depositTxID, deposit)
			err = env.state.Commit()
			require.NoError(t, err)

			var ins []*avax.TransferableInput
			for _, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
			}
			ins = append(ins, tt.generateIns(tt.utxos)...)

			err = env.state.Commit()
			require.NoError(t, err)

			avax.SortTransferableInputsWithSigners(ins, tt.signers)
			avax.SortTransferableOutputs(tt.outs, txs.Codec)

			utx := &txs.UnlockDepositTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          ins,
					Outs:         tt.outs,
				}},
			}

			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			tt.preExecute(*env)

			onAcceptState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(t, err)

			executor := CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      tx,
				},
			}

			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
