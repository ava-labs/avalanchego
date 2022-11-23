// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

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
		err := shutdownEnvironment(env)
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
		if err := shutdownEnvironment(env); err != nil {
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
				staker := state.NewCurrentStaker(
					tx.ID(),
					tx.Unsigned.(*txs.CaminoAddValidatorTx),
					0,
				)

				env.state.PutCurrentValidator(staker)
				env.state.AddTx(tx, status.Committed)
				dummyHeight := uint64(1)
				env.state.SetHeight(dummyHeight)
				err := env.state.Commit()
				require.NoError(t, err)
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
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BanffTime = env.state.GetTimestamp()
	nodeKey, nodeID := caminoPreFundedNodeKeys[0], caminoPreFundedNodeIDs[0]
	tempNodeKey, tempNodeID := nodeid.GenerateCaminoNodeKeyAndID()

	pendingDSValidatorKey, pendingDSValidatorID := nodeid.GenerateCaminoNodeKeyAndID()
	DSStartTime := defaultGenesisTime.Add(10 * time.Second)
	DSEndTime := DSStartTime.Add(5 * defaultMinStakingDuration)

	// Add `pendingDSValidatorID` as validator to pending set
	addDSTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake,
		uint64(DSStartTime.Unix()),
		uint64(DSEndTime.Unix()),
		pendingDSValidatorID,
		ids.ShortEmpty,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], pendingDSValidatorKey},
		ids.ShortEmpty,
	)
	require.NoError(t, err)
	staker := state.NewCurrentStaker(
		addDSTx.ID(),
		addDSTx.Unsigned.(*txs.CaminoAddValidatorTx),
		0,
	)
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
	staker = state.NewCurrentStaker(
		subnetTx.ID(),
		subnetTx.Unsigned.(*txs.AddSubnetValidatorTx),
		0,
	)
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
					startTime:  uint64(DSStartTime.Unix()) - 1,
					endTime:    uint64(DSEndTime.Unix()),
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
					startTime:  uint64(DSStartTime.Unix()),
					endTime:    uint64(DSEndTime.Unix()) + 1,
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
					startTime:  uint64(DSStartTime.Unix()),
					endTime:    uint64(DSEndTime.Unix()),
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
		if err := shutdownEnvironment(env); err != nil {
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
				if err := shutdownEnvironment(env); err != nil {
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
				if err := shutdownEnvironment(env); err != nil {
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
	UTXOsBeforeReward, err := avax.GetAllUTXOs(env.state, stakeOwners)
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
			ins:                      ins,
			outs:                     outs,
			preExecute:               func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO { return UTXOsBeforeReward },
			expectedErr:              errRewardBeforeEndTime,
		},
		"Wrong validator": {
			ins:  ins,
			outs: outs,
			preExecute: func(t *testing.T, tx *txs.Tx) {
				rewValTx := tx.Unsigned.(*txs.CaminoRewardValidatorTx)
				rewValTx.RewardValidatorTx.TxID = ids.GenerateTestID()
			},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO { return UTXOsBeforeReward },
			expectedErr:              database.ErrNotFound,
		},
		"No zero credentials": {
			ins:  ins,
			outs: outs,
			preExecute: func(t *testing.T, tx *txs.Tx) {
				tx.Creds = append(tx.Creds, &secp256k1fx.Credential{})
			},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO { return UTXOsBeforeReward },
			expectedErr:              errWrongNumberOfCredentials,
		},
		"Invalid inputs (one excess)": {
			ins:                      append(ins, &avax.TransferableInput{In: &secp256k1fx.TransferInput{}}),
			outs:                     outs,
			preExecute:               func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO { return UTXOsBeforeReward },
			expectedErr:              errInvalidSystemTxBody,
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
			outs:                     outs,
			preExecute:               func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO { return UTXOsBeforeReward },
			expectedErr:              errInvalidSystemTxBody,
		},
		"Invalid outs (one excess)": {
			ins:                      ins,
			outs:                     append(outs, &avax.TransferableOutput{Out: &secp256k1fx.TransferOutput{}}),
			preExecute:               func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO { return UTXOsBeforeReward },
			expectedErr:              errInvalidSystemTxBody,
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
			preExecute:               func(t *testing.T, tx *txs.Tx) {},
			generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO { return UTXOsBeforeReward },
			expectedErr:              errInvalidSystemTxBody,
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
		UTXOsAfterReward := tt.generateUTXOsAfterReward(tx.ID())
		require.Equal(t, onCommitUTXOs, UTXOsAfterReward)
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
	err = shutdownEnvironment(env)
	require.NoError(t, err)
}
