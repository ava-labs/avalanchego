// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/test"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/expect"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/generate"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestCaminoStandardTxExecutorAddValidatorTx(t *testing.T) {
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment(t, test.PhaseLast, caminoGenesisConf)

	nodeID1 := ids.NodeID{1, 1, 1}
	nodeID2 := ids.NodeID{2, 2, 2}
	// msigKey, err := testKeyfactory.NewPrivateKey()
	// require.NoError(t, err)
	// msigAlias := msigKey.Address()

	addr0 := test.FundedKeys[0].Address()
	addr1 := test.FundedKeys[1].Address()

	require.NoError(t, env.state.Commit())

	type args struct {
		stakeAmount   uint64
		startTime     uint64
		endTime       uint64
		nodeID        ids.NodeID
		nodeOwnerAddr ids.ShortID
		rewardAddress ids.ShortID
		shares        uint32
		keys          []*secp256k1.PrivateKey
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
					startTime:     uint64(test.ValidatorStartTime.Unix()) + 1,
					endTime:       uint64(test.ValidatorEndTime.Unix()),
					nodeID:        nodeID1,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{test.FundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID1), state.ShortLinkKeyRegisterNode, &addr0)
			},
			expectedErr: nil,
		},
		"Validator's start time too early": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(test.ValidatorStartTime.Unix()) - 1,
					endTime:       uint64(test.ValidatorEndTime.Unix()),
					nodeID:        nodeID1,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{test.FundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID1), state.ShortLinkKeyRegisterNode, &addr0)
			},
			expectedErr: errTimestampNotBeforeStartTime,
		},
		"Validator's start time too far in the future": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(test.ValidatorStartTime.Add(MaxFutureStartTime).Unix() + 1),
					endTime:       uint64(test.ValidatorEndTime.Add(MaxFutureStartTime).Add(test.MinStakingDuration).Unix() + 1),
					nodeID:        nodeID1,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{test.FundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID1), state.ShortLinkKeyRegisterNode, &addr0)
			},
			expectedErr: errFutureStakeTime,
		},
		"Validator already validating primary network": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(test.ValidatorStartTime.Unix() + 1),
					endTime:       uint64(test.ValidatorEndTime.Unix()),
					nodeID:        test.FundedNodeIDs[0],
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{test.FundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(test.FundedNodeIDs[0]), state.ShortLinkKeyRegisterNode, &addr0)
			},
			expectedErr: errValidatorExists,
		},
		"Validator in pending validator set of primary network": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(test.GenesisTime.Add(1 * time.Second).Unix()),
					endTime:       uint64(test.GenesisTime.Add(1 * time.Second).Add(test.MinStakingDuration).Unix()),
					nodeID:        nodeID2,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{test.FundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID2), state.ShortLinkKeyRegisterNode, &addr0)
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
				require.NoError(t, env.state.Commit())
			},
			expectedErr: errValidatorExists,
		},
		"Validator in deferred validator set of primary network": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(test.GenesisTime.Add(1 * time.Second).Unix()),
					endTime:       uint64(test.GenesisTime.Add(1 * time.Second).Add(test.MinStakingDuration).Unix()),
					nodeID:        nodeID2,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{test.FundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID2), state.ShortLinkKeyRegisterNode, &addr0)
				staker, err := state.NewCurrentStaker(
					tx.ID(),
					tx.Unsigned.(*txs.CaminoAddValidatorTx),
					0,
				)
				require.NoError(t, err)
				env.state.PutDeferredValidator(staker)
				env.state.AddTx(tx, status.Committed)
				dummyHeight := uint64(1)
				env.state.SetHeight(dummyHeight)
				require.NoError(t, env.state.Commit())
			},
			expectedErr: errValidatorExists,
		},
		"AddValidatorTx flow check failed": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(test.ValidatorStartTime.Unix() + 1),
					endTime:       uint64(test.ValidatorEndTime.Unix()),
					nodeID:        nodeID1,
					nodeOwnerAddr: addr1,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{test.FundedKeys[1]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID1), state.ShortLinkKeyRegisterNode, &addr1)
				utxoIDs, err := env.state.UTXOIDs(test.FundedKeys[1].Address().Bytes(), ids.Empty, math.MaxInt32)
				require.NoError(t, err)
				for _, utxoID := range utxoIDs {
					env.state.DeleteUTXO(utxoID)
				}
			},
			expectedErr: errFlowCheckFailed,
		},
		"Not signed by node owner": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(test.ValidatorStartTime.Unix() + 1),
					endTime:       uint64(test.ValidatorEndTime.Unix()),
					nodeID:        nodeID1,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{test.FundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID1), state.ShortLinkKeyRegisterNode, &addr1)
			},
			expectedErr: errSignatureMissing,
		},
		// "Not enough sigs from msig node owner": {// TODO @evlekht can't be created with tx builder, needs manual creation
		// 	generateArgs: func() args {
		// 		return args{
		// 			stakeAmount:          env.config.MinValidatorStake,
		// 			startTime:            uint64(test.ValidatorStartTime.Unix() + 1),
		// 			endTime:              uint64(test.ValidatorEndTime.Unix()),
		// 			nodeID:               nodeID,
		// 			nodeOwnerAddr: msigAlias,
		// 			rewardAddress:        ids.ShortEmpty,
		// 			shares:               reward.PercentDenominator,
		// 			keys:                 []*secp256k1.PrivateKey{ test.PreFundedKeys[0]},
		// 			changeAddr:           ids.ShortEmpty,
		// 		}
		// 	},
		// 	preExecute: func(t *testing.T, tx *txs.Tx) {
		// 		env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &msigAlias)
		// 		env.state.SetMultisigAlias(&multisig.AliasWithNonce{
		// 			Alias: multisig.Alias{
		// 				ID: msigAlias,
		// 				Owners: &secp256k1fx.OutputOwners{
		// 					Threshold: 2,
		// 					Addrs: []ids.ShortID{
		// 						 test.PreFundedKeys[0].Address(),
		// 						 test.PreFundedKeys[1].Address(),
		// 					},
		// 				},
		// 			},
		// 		})
		// 	},
		// 	expectedErr: errSignatureMissing,
		// },
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			addValidatorArgs := tt.generateArgs()
			tx, err := env.txBuilder.NewCaminoAddValidatorTx(
				addValidatorArgs.stakeAmount,
				addValidatorArgs.startTime,
				addValidatorArgs.endTime,
				addValidatorArgs.nodeID,
				addValidatorArgs.nodeOwnerAddr,
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
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment(t, test.PhaseLast, caminoGenesisConf)
	env.addCaminoSubnet(t)

	nodeKey, nodeID := test.FundedNodeKeys[0], test.FundedNodeIDs[0]
	tempNodeKey, tempNodeID := test.Keys[0], ids.NodeID(test.Keys[0].Address())

	pendingValidatorNodeKey, pendingValidatorNodeID := test.Keys[1], ids.NodeID(test.Keys[1].Address())
	dsStartTime := test.GenesisTime.Add(10 * time.Second)
	dsEndTime := dsStartTime.Add(5 * test.MinStakingDuration)

	// Add `pendingDSValidatorID` as validator to pending set
	addDSTx, err := env.txBuilder.NewCaminoAddValidatorTx(
		env.config.MinValidatorStake,
		uint64(dsStartTime.Unix()),
		uint64(dsEndTime.Unix()),
		pendingValidatorNodeID,
		test.FundedKeys[0].Address(),
		ids.ShortEmpty,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{test.FundedKeys[0], pendingValidatorNodeKey},
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

	// Add `test.PreFundedNodeIDs[1]` as subnet validator
	subnetTx, err := env.txBuilder.NewAddSubnetValidatorTx(
		env.config.MinValidatorStake,
		uint64(test.ValidatorStartTime.Unix()),
		uint64(test.ValidatorEndTime.Unix()),
		test.FundedNodeIDs[1],
		testSubnet1.ID(),
		[]*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], test.FundedNodeKeys[1]},
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
		keys       []*secp256k1.PrivateKey
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
					startTime:  uint64(test.ValidatorStartTime.Unix()) + 1,
					endTime:    uint64(test.ValidatorEndTime.Unix()),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
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
					startTime:  uint64(test.ValidatorStartTime.Unix()) + 1,
					endTime:    uint64(test.ValidatorEndTime.Unix() + 1),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
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
					startTime:  uint64(test.ValidatorStartTime.Unix()) + 1,
					endTime:    uint64(test.ValidatorEndTime.Unix()),
					nodeID:     tempNodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], tempNodeKey},
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
					nodeID:     pendingValidatorNodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], pendingValidatorNodeKey},
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
					nodeID:     pendingValidatorNodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], pendingValidatorNodeKey},
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
					nodeID:     pendingValidatorNodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], pendingValidatorNodeKey},
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
					startTime:  uint64(test.ValidatorStartTime.Unix()),
					endTime:    uint64(test.ValidatorEndTime.Unix()),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
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
					startTime:  uint64(test.ValidatorStartTime.Unix() + 1),
					endTime:    uint64(test.ValidatorEndTime.Unix()),
					nodeID:     test.FundedNodeIDs[1],
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], test.FundedNodeKeys[1]},
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
					startTime:  uint64(test.ValidatorStartTime.Unix() + 1),
					endTime:    uint64(test.ValidatorEndTime.Unix()),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
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
					startTime:  uint64(test.ValidatorStartTime.Unix() + 1),
					endTime:    uint64(test.ValidatorEndTime.Unix()),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{test.FundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
					changeAddr: ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				// Replace a valid signature with one from keys[3]
				sig, err := test.FundedKeys[3].SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
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
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment(t, test.PhaseLast, caminoGenesisConf)

	nodeID := ids.NodeID{1, 1, 1}
	addr0 := test.FundedKeys[0].Address()
	env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr0)

	existingTxID := ids.GenerateTestID()
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{test.FundedKeys[0].Address()},
	}
	sigIndices := []uint32{0}
	inputSigners := []*secp256k1.PrivateKey{test.FundedKeys[0]}

	tests := map[string]struct {
		utxos       []*avax.UTXO
		outs        []*avax.TransferableOutput
		expectedErr error
	}{
		"Happy path bonding": {
			utxos: []*avax.UTXO{
				generate.UTXO(ids.ID{1}, env.ctx.AVAXAssetID, test.ValidatorWeight*2, outputOwners, ids.Empty, ids.Empty, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.Empty, ids.Empty),
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, locked.ThisTxID),
			},
			expectedErr: nil,
		},
		"Happy path bonding deposited": {
			utxos: []*avax.UTXO{
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, ids.Empty, true),
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight*2, outputOwners, existingTxID, ids.Empty, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.Empty, ids.Empty),
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, existingTxID, ids.Empty),
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, existingTxID, locked.ThisTxID),
			},
			expectedErr: nil,
		},
		"Happy path bonding deposited and unlocked": {
			utxos: []*avax.UTXO{
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight/2, outputOwners, existingTxID, ids.Empty, true),
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, ids.Empty, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight/2-test.TxFee, outputOwners, ids.Empty, ids.Empty),
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight/2, outputOwners, ids.Empty, locked.ThisTxID),
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight/2, outputOwners, existingTxID, locked.ThisTxID),
			},
			expectedErr: nil,
		},
		"Bonding bonded UTXO": {
			utxos: []*avax.UTXO{
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, ids.Empty, true),
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, existingTxID, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.Empty, ids.Empty),
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, locked.ThisTxID),
			},
			expectedErr: errFlowCheckFailed,
		},
		"Fee burning bonded UTXO": {
			utxos: []*avax.UTXO{
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, ids.Empty, true),
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, existingTxID, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, locked.ThisTxID),
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.Empty, existingTxID),
			},
			expectedErr: errFlowCheckFailed,
		},
		"Fee burning deposited UTXO": {
			utxos: []*avax.UTXO{
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, ids.Empty, true),
				generate.UTXO(ids.GenerateTestID(), env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, existingTxID, ids.Empty, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, existingTxID, ids.Empty),
				generate.Out(env.ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, existingTxID, locked.ThisTxID),
			},
			expectedErr: errFlowCheckFailed,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ins := make([]*avax.TransferableInput, len(tt.utxos))
			signers := make([][]*secp256k1.PrivateKey, len(tt.utxos))
			for i, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
				ins[i] = generate.InFromUTXO(t, utxo, sigIndices, false)
				signers[i] = inputSigners
			}
			signers = append(signers, []*secp256k1.PrivateKey{test.FundedKeys[0]})

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
					Validator: txs.Validator{
						NodeID: nodeID,
						Start:  uint64(test.ValidatorStartTime.Unix()) + 1,
						End:    uint64(test.ValidatorEndTime.Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					RewardsOwner: &secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.ShortEmpty},
					},
				},
				NodeOwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
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

func TestCaminoLockedInsOrLockedOuts(t *testing.T) {
	ctx := test.Context(t)
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{test.FundedKeys[0].Address()},
	}
	sigIndices := []uint32{0}

	nodeKey, nodeID := test.Keys[0], ids.NodeID(test.Keys[0].Address())

	now := time.Now()
	signers := [][]*secp256k1.PrivateKey{{test.FundedKeys[0]}}
	signers[len(signers)-1] = []*secp256k1.PrivateKey{nodeKey}

	tests := map[string]struct {
		outs         []*avax.TransferableOutput
		ins          []*avax.TransferableInput
		expectedErr  error
		caminoConfig api.Camino
	}{
		"Locked out - LockModeBondDeposit: true": {
			outs: []*avax.TransferableOutput{
				generate.Out(ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, ids.GenerateTestID()),
			},
			ins:         []*avax.TransferableInput{},
			expectedErr: locked.ErrWrongOutType,
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
		},
		"Locked in - LockModeBondDeposit: true": {
			outs: []*avax.TransferableOutput{},
			ins: []*avax.TransferableInput{
				generate.In(ctx.AVAXAssetID, test.ValidatorWeight, ids.GenerateTestID(), ids.Empty, sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
		},
		"Locked out - LockModeBondDeposit: false": {
			outs: []*avax.TransferableOutput{
				generate.Out(ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, ids.GenerateTestID()),
			},
			ins:         []*avax.TransferableInput{},
			expectedErr: locked.ErrWrongOutType,
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
		},
		"Locked in - LockModeBondDeposit: false": {
			outs: []*avax.TransferableOutput{},
			ins: []*avax.TransferableInput{
				generate.In(ctx.AVAXAssetID, test.ValidatorWeight, ids.GenerateTestID(), ids.Empty, sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
		},
		"Stakeable out - LockModeBondDeposit: true": {
			outs: []*avax.TransferableOutput{
				generate.StakeableOut(ctx.AVAXAssetID, test.ValidatorWeight, uint64(test.MinStakingDuration), outputOwners),
			},
			ins:         []*avax.TransferableInput{},
			expectedErr: locked.ErrWrongOutType,
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
		},
		"Stakeable in - LockModeBondDeposit: true": {
			outs: []*avax.TransferableOutput{},
			ins: []*avax.TransferableInput{
				generate.StakeableIn(ctx.AVAXAssetID, test.ValidatorWeight, uint64(test.MinStakingDuration), sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
		},
		"Stakeable out - LockModeBondDeposit: false": {
			outs: []*avax.TransferableOutput{
				generate.StakeableOut(ctx.AVAXAssetID, test.ValidatorWeight, uint64(test.MinStakingDuration), outputOwners),
			},
			ins:         []*avax.TransferableInput{},
			expectedErr: locked.ErrWrongOutType,
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
		},
		"Stakeable in - LockModeBondDeposit: false": {
			outs: []*avax.TransferableOutput{},
			ins: []*avax.TransferableInput{
				generate.StakeableIn(ctx.AVAXAssetID, test.ValidatorWeight, uint64(test.MinStakingDuration), sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
		},
	}

	generateExecutor := func(unsignedTx txs.UnsignedTx, env *caminoEnvironment) CaminoStandardTxExecutor {
		tx, err := txs.NewSigned(unsignedTx, txs.Codec, signers)
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
			env := newCaminoEnvironment(t, test.PhaseLast, tt.caminoConfig)

			exportTx := &txs.ExportTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				DestinationChain: env.ctx.XChainID,
				ExportedOutputs: []*avax.TransferableOutput{
					generate.Out(env.ctx.AVAXAssetID, test.TxFee*10, outputOwners, ids.Empty, ids.Empty),
				},
			}

			executor := generateExecutor(exportTx, env)

			err := executor.ExportTx(exportTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("ImportTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment(t, test.PhaseLast, tt.caminoConfig)

			importTx := &txs.ImportTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				SourceChain: env.ctx.XChainID,
				ImportedInputs: []*avax.TransferableInput{
					generate.In(env.ctx.AVAXAssetID, 10, ids.GenerateTestID(), ids.Empty, sigIndices),
				},
			}

			executor := generateExecutor(importTx, env)

			err := executor.ImportTx(importTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("AddressStateTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment(t, test.PhaseLast, tt.caminoConfig)

			addressStateTxLockedTx := &txs.AddressStateTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				Address:  test.FundedKeys[0].Address(),
				StateBit: 0,
				Remove:   false,
			}

			executor := generateExecutor(addressStateTxLockedTx, env)

			err := executor.AddressStateTx(addressStateTxLockedTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("CreateChainTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment(t, test.PhaseLast, tt.caminoConfig)

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
			env := newCaminoEnvironment(t, test.PhaseLast, tt.caminoConfig)

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
			env := newCaminoEnvironment(t, test.PhaseLast, tt.caminoConfig)

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
			env := newCaminoEnvironment(t, test.PhaseLast, tt.caminoConfig)

			addSubnetValidatorTx := &txs.AddSubnetValidatorTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				SubnetValidator: txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						Start:  uint64(now.Unix()),
						End:    uint64(now.Add(time.Hour).Unix()),
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
			env := newCaminoEnvironment(t, test.PhaseLast, tt.caminoConfig)

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
	ctx := test.Context(t)
	nodeKey1, nodeID1 := test.FundedNodeKeys[0], test.FundedNodeIDs[0]
	nodeKey2 := test.FundedNodeKeys[1]

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{test.FundedKeys[0].Address()},
	}
	sigIndices := []uint32{0}
	inputSigners := []*secp256k1.PrivateKey{test.FundedKeys[0]}

	tests := map[string]struct {
		caminoConfig api.Camino
		nodeID       ids.NodeID
		nodeKey      *secp256k1.PrivateKey
		utxos        []*avax.UTXO
		outs         []*avax.TransferableOutput
		stakedOuts   []*avax.TransferableOutput
		expectedErr  error
	}{
		"Happy path, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey1,
			utxos: []*avax.UTXO{
				generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.ValidatorWeight*2, outputOwners, ids.Empty, ids.Empty, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.Empty, ids.Empty),
			},
			stakedOuts: []*avax.TransferableOutput{
				generate.StakeableOut(ctx.AVAXAssetID, test.ValidatorWeight, uint64(test.MinStakingDuration), outputOwners),
			},
			expectedErr: nil,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit false, VerifyNodeSignature true": {
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey2,
			utxos: []*avax.UTXO{
				generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.ValidatorWeight*2, outputOwners, ids.Empty, ids.Empty, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.Empty, ids.Empty),
			},
			stakedOuts: []*avax.TransferableOutput{
				generate.StakeableOut(ctx.AVAXAssetID, test.ValidatorWeight, uint64(test.MinStakingDuration), outputOwners),
			},
			expectedErr: errNodeSignatureMissing,
		},
		"NodeId node and signature mismatch, LockModeBondDeposit true, VerifyNodeSignature true": {
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey2,
			utxos: []*avax.UTXO{
				generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.ValidatorWeight*2, outputOwners, ids.Empty, ids.Empty, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.Empty, ids.Empty),
			},
			expectedErr: errNodeSignatureMissing,
		},
		"Inputs and credentials mismatch, LockModeBondDeposit true, VerifyNodeSignature false": {
			caminoConfig: api.Camino{
				VerifyNodeSignature: false,
				LockModeBondDeposit: true,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey2,
			utxos: []*avax.UTXO{
				generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.ValidatorWeight*2, outputOwners, ids.Empty, ids.Empty, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.Empty, ids.Empty),
			},
			expectedErr: errUnauthorizedSubnetModification,
		},
		"Inputs and credentials mismatch, LockModeBondDeposit false, VerifyNodeSignature false": {
			caminoConfig: api.Camino{
				VerifyNodeSignature: false,
				LockModeBondDeposit: false,
			},
			nodeID:  nodeID1,
			nodeKey: nodeKey1,
			utxos: []*avax.UTXO{
				generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.ValidatorWeight*2, outputOwners, ids.Empty, ids.Empty, true),
			},
			outs: []*avax.TransferableOutput{
				generate.Out(ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.Empty, ids.Empty),
			},
			stakedOuts: []*avax.TransferableOutput{
				generate.StakeableOut(ctx.AVAXAssetID, test.ValidatorWeight, uint64(test.MinStakingDuration), outputOwners),
			},
			expectedErr: errUnauthorizedSubnetModification,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			env := newCaminoEnvironment(t, test.PhaseLast, tt.caminoConfig)
			env.addCaminoSubnet(t)

			ins := make([]*avax.TransferableInput, len(tt.utxos))
			var signers [][]*secp256k1.PrivateKey
			for i, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
				ins[i] = generate.InFromUTXO(t, utxo, sigIndices, false)
				signers = append(signers, inputSigners)
			}

			avax.SortTransferableInputsWithSigners(ins, signers)
			avax.SortTransferableOutputs(tt.outs, txs.Codec)

			subnetAuth, subnetSigners, err := env.utxosHandler.Authorize(env.state, testSubnet1.ID(), testCaminoSubnet1ControlKeys)
			require.NoError(t, err)
			signers = append(signers, subnetSigners)
			signers = append(signers, []*secp256k1.PrivateKey{tt.nodeKey})

			addSubnetValidatorTx := &txs.AddSubnetValidatorTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          ins,
					Outs:         tt.outs,
				}},
				SubnetValidator: txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: tt.nodeID,
						Start:  uint64(test.ValidatorStartTime.Unix()) + 1,
						End:    uint64(test.ValidatorEndTime.Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					Subnet: testSubnet1.ID(),
				},
				SubnetAuth: subnetAuth,
			}

			var utx txs.UnsignedTx = addSubnetValidatorTx
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
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	env := newCaminoEnvironment(t, test.PhaseLast, caminoGenesisConf)

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
	secpOut := innerOut.TransferableOut.(*secp256k1fx.TransferOutput)
	stakeOwnersAddresses := secpOut.AddressesSet()
	stakeOwners := secpOut.OutputOwners
	utxosBeforeReward, err := avax.GetAllUTXOs(env.state, stakeOwnersAddresses)
	require.NoError(t, err)

	unlockedUTXOTxID := ids.Empty
	for _, utxo := range utxosBeforeReward {
		if _, ok := utxo.Out.(*locked.Out); !ok {
			unlockedUTXOTxID = utxo.TxID
			break
		}
	}
	require.NotEqual(t, ids.Empty, unlockedUTXOTxID)

	type testCase struct {
		ins                      []*avax.TransferableInput
		outs                     []*avax.TransferableOutput
		preExecute               func(*testing.T, *txs.Tx)
		generateUTXOsAfterReward func(ids.ID) []*avax.UTXO
		expectedErr              error
	}

	tests := map[string]testCase{
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
			expectedErr: errWrongCredentialsNumber,
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

	execute := func(t *testing.T, tt testCase) (CaminoProposalTxExecutor, *txs.Tx) {
		tx := &txs.Tx{Unsigned: &txs.CaminoRewardValidatorTx{
			RewardValidatorTx: txs.RewardValidatorTx{TxID: stakerToRemove.TxID},
			Ins:               tt.ins,
			Outs:              tt.outs,
		}}
		require.NoError(t, tx.Initialize(txs.Codec))

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
	assertBalance := func(t *testing.T, tt testCase, tx *txs.Tx) {
		onCommitUTXOs, err := avax.GetAllUTXOs(env.state, stakeOwnersAddresses)
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
			require.NoError(t, txExecutor.OnAbortState.Apply(env.state))
			env.state.SetHeight(uint64(1))
			require.NoError(t, env.state.Commit())
			assertBalance(t, tt, tx)
		})
		t.Run(name+" On commit", func(t *testing.T) {
			txExecutor, tx := execute(t, tt)
			require.NoError(t, txExecutor.OnCommitState.Apply(env.state))
			env.state.SetHeight(uint64(1))
			require.NoError(t, env.state.Commit())
			assertBalance(t, tt, tx)
		})
	}

	happyPathTest := testCase{
		ins:  ins,
		outs: outs,
		preExecute: func(t *testing.T, tx *txs.Tx) {
			env.state.SetTimestamp(stakerToRemove.EndTime)
		},
		generateUTXOsAfterReward: func(txID ids.ID) []*avax.UTXO {
			return []*avax.UTXO{
				generate.UTXO(txID, env.ctx.AVAXAssetID, test.ValidatorWeight, stakeOwners, ids.Empty, ids.Empty, true),
				generate.UTXOWithIndex(unlockedUTXOTxID, 2, env.ctx.AVAXAssetID, test.PreFundedBalance, stakeOwners, ids.Empty, ids.Empty, true),
			}
		},
		expectedErr: nil,
	}

	t.Run("Happy path on commit", func(t *testing.T) {
		txExecutor, tx := execute(t, happyPathTest)
		require.NoError(t, txExecutor.OnCommitState.Apply(env.state))
		env.state.SetHeight(uint64(1))
		require.NoError(t, env.state.Commit())
		assertBalance(t, happyPathTest, tx)
		assertNextStaker(t)
	})

	// We need to start again the environment because the staker is already removed from the previous test
	env = newCaminoEnvironment(t, test.PhaseLast, caminoGenesisConf)

	t.Run("Happy path on abort", func(t *testing.T) {
		// utxoids are polluted with cached ids, need to clean this non-exported field
		for _, in := range ins {
			in.UTXOID = avax.UTXOID{
				TxID:        in.TxID,
				OutputIndex: in.OutputIndex,
			}
		}
		txExecutor, tx := execute(t, happyPathTest)
		require.NoError(t, txExecutor.OnAbortState.Apply(env.state))
		env.state.SetHeight(uint64(1))
		require.NoError(t, env.state.Commit())
		assertBalance(t, happyPathTest, tx)
		assertNextStaker(t)
	})
}

func TestCaminoStandardTxAddressStateTx(t *testing.T) {
	ctx := test.Context(t)
	caminoGenesisConf := api.Camino{VerifyNodeSignature: true, LockModeBondDeposit: true}

	otherAddr := ids.ShortID{1}
	msigAliasAddr := ids.ShortID{2}
	deferredNodeShortID := ids.ShortID{1, 1}
	deferredNodeID := ids.NodeID(deferredNodeShortID)
	deferredStaker := &state.Staker{TxID: ids.ID{1, 1, 1}}
	msigAliasOwner := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{msigAliasAddr}}

	feeOwnerKey, feeOwnerAddr, feeOwner := generate.KeyAndOwner(t, test.Keys[0])
	executorKey, executorAddr, executorOwner := generate.KeyAndOwner(t, test.Keys[1])

	feeUTXO := generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty, false)
	halfFeeUTXO1 := generate.UTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee/2, feeOwner, ids.Empty, ids.Empty, false)
	halfFeeUTXO2 := generate.UTXO(ids.ID{3}, ctx.AVAXAssetID, defaultTxFee-defaultTxFee/2, executorOwner, ids.Empty, ids.Empty, false)
	msigFeeUTXO := generate.UTXO(ids.ID{4}, ctx.AVAXAssetID, defaultTxFee, msigAliasOwner, ids.Empty, ids.Empty, false)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, feeUTXO, []uint32{0}, false)},
	}}

	type testData struct {
		selfModify             bool
		txStateBit             as.AddressStateBit
		remove                 bool
		currentTargetAddrState as.AddressState
		executorAddrState      as.AddressState
	}

	type testCase struct {
		state       func(*testing.T, *gomock.Controller, *txs.AddressStateTx, ids.ID, *config.Config) *state.MockDiff
		utx         *txs.AddressStateTx
		signers     [][]*secp256k1.PrivateKey
		phase       test.Phase
		expectedErr error
	}

	testCases := map[string]testCase{}

	type testCaseFunc func(t *testing.T, tt testData, testCaseName string, phase test.Phase)
	type testCaseSimpleFunc func(t *testing.T, phase test.Phase)
	type testCaseFuncWithErr func(t *testing.T, tt testData, testCaseName string, expectedErr error, phase test.Phase)

	var failCaseSimpleNoOp testCaseSimpleFunc = func(t *testing.T, phase test.Phase) {}

	testCaseFailMultisigAlias := map[codec.UpgradeVersionID]testCaseSimpleFunc{}
	testCaseFailMultisigAlias[codec.UpgradeVersion0] = func(t *testing.T, phase test.Phase) {
		testCaseName := fmt.Sprintf("%d_%s/Upgrade 0/Fail with multisig alias", phase, test.PhaseName(t, phase))
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				require.Zero(t, utx.UpgradeVersionID.Version())

				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))

				// not getting addr state for msigAlias addr
				s.EXPECT().GetAddressStates(feeOwnerAddr).Return(as.AddressStateEmpty, nil)
				s.EXPECT().GetAddressStates(executorAddr).Return(as.AddressStateEmpty, nil)
				// fails to verify permission to change target
				return s
			},
			utx: &txs.AddressStateTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, msigFeeUTXO, []uint32{0}, false)},
				}},
				Address:      otherAddr,
				StateBit:     as.AddressStateBitKYCVerified,
				ExecutorAuth: &secp256k1fx.Input{},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey, executorKey},
			},
			phase:       phase,
			expectedErr: errAddrStateNotPermitted,
		}
	}
	testCaseFailMultisigAlias[codec.UpgradeVersion1] = failCaseSimpleNoOp

	testCaseFailUpgradeVersionForbidden := map[codec.UpgradeVersionID]testCaseSimpleFunc{}
	testCaseFailUpgradeVersionForbidden[codec.UpgradeVersion0] = func(t *testing.T, phase test.Phase) {
		require.GreaterOrEqual(t, phase, test.PhaseBerlin)
		testCaseName := fmt.Sprintf("%d_%s/Upgrade 0/Upgrade version is forbidden", phase, test.PhaseName(t, phase))
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				require.Zero(t, utx.UpgradeVersionID.Version())
				require.GreaterOrEqual(t, phase, test.PhaseBerlin)
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))
				return s
			},
			utx: &txs.AddressStateTx{
				BaseTx:       baseTx,
				Address:      otherAddr,
				StateBit:     as.AddressStateBitKYCVerified,
				ExecutorAuth: &secp256k1fx.Input{},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
			},
			phase:       phase,
			expectedErr: errBerlinPhase,
		}
	}
	testCaseFailUpgradeVersionForbidden[codec.UpgradeVersion1] = func(t *testing.T, phase test.Phase) {
		require.Equal(t, test.PhaseSunrise, phase)
		testCaseName := fmt.Sprintf("%d_%s/Upgrade 1/Upgrade version is forbidden", phase, test.PhaseName(t, phase))
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				require.Greater(t, utx.UpgradeVersionID.Version(), uint16(0))
				require.Equal(t, phase, test.PhaseSunrise)
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))
				return s
			},
			utx: &txs.AddressStateTx{
				UpgradeVersionID: codec.UpgradeVersion1,
				BaseTx:           baseTx,
				Address:          otherAddr,
				StateBit:         as.AddressStateBitKYCVerified,
				Executor:         executorAddr,
				ExecutorAuth:     &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {executorKey},
			},
			phase:       phase,
			expectedErr: errNotAthensPhase,
		}
	}

	testCaseFailWrongExecutorCredential := map[codec.UpgradeVersionID]testCaseSimpleFunc{}
	testCaseFailWrongExecutorCredential[codec.UpgradeVersion0] = failCaseSimpleNoOp
	testCaseFailWrongExecutorCredential[codec.UpgradeVersion1] = func(t *testing.T, phase test.Phase) {
		if phase < test.PhaseAthens {
			return
		}
		testCaseName := fmt.Sprintf("%d_%s/Upgrade 1/Wrong executor credential", phase, test.PhaseName(t, phase))
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.Executor}, nil)
				return s
			},
			utx: &txs.AddressStateTx{
				UpgradeVersionID: codec.UpgradeVersion1,
				BaseTx:           baseTx,
				Address:          otherAddr,
				StateBit:         as.AddressStateBitKYCVerified,
				Executor:         executorAddr,
				ExecutorAuth:     &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {feeOwnerKey},
			},
			phase:       phase,
			expectedErr: errSignatureMissing,
		}
	}

	testCaseFailNoPermission := map[codec.UpgradeVersionID]testCaseFunc{}
	testCaseFailNoPermission[codec.UpgradeVersion0] = func(
		t *testing.T,
		tt testData,
		testCaseName string,
		phase test.Phase,
	) {
		testCaseName = fmt.Sprintf("%d_%s/Upgrade 0/%s", phase, test.PhaseName(t, phase), testCaseName)
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))
				s.EXPECT().GetAddressStates(feeOwnerAddr).Return(tt.executorAddrState, nil)
				return s
			},
			utx: &txs.AddressStateTx{
				BaseTx:       baseTx,
				Address:      otherAddr,
				StateBit:     tt.txStateBit,
				ExecutorAuth: &secp256k1fx.Input{},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
			},
			phase:       phase,
			expectedErr: errAddrStateNotPermitted,
		}
	}
	testCaseFailNoPermission[codec.UpgradeVersion1] = func(
		t *testing.T,
		tt testData,
		testCaseName string,
		phase test.Phase,
	) {
		testCaseName = fmt.Sprintf("%d_%s/Upgrade 1/%s", phase, test.PhaseName(t, phase), testCaseName)
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.Executor}, nil)
				s.EXPECT().GetAddressStates(executorAddr).Return(tt.executorAddrState, nil)
				return s
			},
			utx: &txs.AddressStateTx{
				UpgradeVersionID: codec.UpgradeVersion1,
				BaseTx:           baseTx,
				Address:          otherAddr,
				StateBit:         tt.txStateBit,
				Executor:         executorAddr,
				ExecutorAuth:     &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {executorKey},
			},
			phase:       phase,
			expectedErr: errAddrStateNotPermitted,
		}
	}

	testCaseFailBitForbidden := map[codec.UpgradeVersionID]testCaseFuncWithErr{}
	testCaseFailBitForbidden[codec.UpgradeVersion0] = func(
		t *testing.T,
		tt testData,
		testCaseName string,
		expectedErr error,
		phase test.Phase,
	) {
		testCaseName = fmt.Sprintf("%d_%s/Upgrade 0/%s", phase, test.PhaseName(t, phase), testCaseName)
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))
				return s
			},
			utx: &txs.AddressStateTx{
				BaseTx:       baseTx,
				Address:      otherAddr,
				StateBit:     tt.txStateBit,
				ExecutorAuth: &secp256k1fx.Input{},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
			},
			phase:       phase,
			expectedErr: expectedErr,
		}
	}
	testCaseFailBitForbidden[codec.UpgradeVersion1] = func(
		t *testing.T,
		tt testData,
		testCaseName string,
		expectedErr error,
		phase test.Phase,
	) {
		testCaseName = fmt.Sprintf("%d_%s/Upgrade 1/%s", phase, test.PhaseName(t, phase), testCaseName)
		require.NotContains(t, testCases, testCaseName, fmt.Sprintf("duplicate test-case name: %s", testCaseName))
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))
				return s
			},
			utx: &txs.AddressStateTx{
				UpgradeVersionID: codec.UpgradeVersion1,
				BaseTx:           baseTx,
				Address:          otherAddr,
				StateBit:         tt.txStateBit,
				Executor:         executorAddr,
				ExecutorAuth:     &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {executorKey},
			},
			phase:       phase,
			expectedErr: expectedErr,
		}
	}

	testCaseFailAdminSelfRemove := map[codec.UpgradeVersionID]testCaseSimpleFunc{}
	testCaseFailAdminSelfRemove[codec.UpgradeVersion0] = func(t *testing.T, phase test.Phase) {
		testCaseName := fmt.Sprintf("%d_%s/Upgrade 0/AddressStateRoleAdmin (%d) self-remove", phase, test.PhaseName(t, phase), as.AddressStateBitRoleAdmin)
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))
				return s
			},
			utx: &txs.AddressStateTx{
				BaseTx:       baseTx,
				Address:      feeOwnerAddr,
				StateBit:     as.AddressStateBitRoleAdmin,
				Remove:       true,
				ExecutorAuth: &secp256k1fx.Input{},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
			},
			phase:       phase,
			expectedErr: errAdminCannotBeDeleted,
		}
	}
	testCaseFailAdminSelfRemove[codec.UpgradeVersion1] = func(t *testing.T, phase test.Phase) {
		testCaseName := fmt.Sprintf("%d_%s/Upgrade 1/AddressStateRoleAdmin (%d) self-remove", phase, test.PhaseName(t, phase), as.AddressStateBitRoleAdmin)
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.Executor}, nil)
				return s
			},
			utx: &txs.AddressStateTx{
				UpgradeVersionID: codec.UpgradeVersion1,
				BaseTx:           baseTx,
				Address:          executorAddr,
				StateBit:         as.AddressStateBitRoleAdmin,
				Remove:           true,
				Executor:         executorAddr,
				ExecutorAuth:     &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {executorKey},
			},
			phase:       phase,
			expectedErr: errAdminCannotBeDeleted,
		}
	}

	testCaseOK := map[codec.UpgradeVersionID]testCaseFunc{}
	testCaseOK[codec.UpgradeVersion0] = func(
		t *testing.T,
		tt testData,
		testCaseName string,
		phase test.Phase,
	) {
		testCaseName = fmt.Sprintf("%d_%s/Upgrade 0/%s", phase, test.PhaseName(t, phase), testCaseName)
		require.NotContains(t, testCases, testCaseName, testCaseName)
		targetAddr := otherAddr
		if tt.selfModify {
			targetAddr = feeOwnerAddr
		}
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				require.Zero(t, utx.UpgradeVersionID.Version())
				if feeOwnerAddr == utx.Address {
					require.Equal(t, tt.currentTargetAddrState, as.AddressStateRoleAdmin)
				}

				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))

				s.EXPECT().GetAddressStates(feeOwnerAddr).Return(tt.executorAddrState, nil)

				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)

				if utx.StateBit == as.AddressStateBitNodeDeferred {
					s.EXPECT().GetShortIDLink(utx.Address, state.ShortLinkKeyRegisterNode).Return(deferredNodeShortID, nil)
					if utx.Remove {
						s.EXPECT().GetDeferredValidator(constants.PrimaryNetworkID, deferredNodeID).Return(deferredStaker, nil)
						s.EXPECT().DeleteDeferredValidator(deferredStaker)
						s.EXPECT().PutCurrentValidator(deferredStaker)
					} else {
						s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, deferredNodeID).Return(deferredStaker, nil)
						s.EXPECT().DeleteCurrentValidator(deferredStaker)
						s.EXPECT().PutDeferredValidator(deferredStaker)
					}
				}

				s.EXPECT().GetAddressStates(utx.Address).Return(tt.currentTargetAddrState, nil)
				txAddrState := utx.StateBit.ToAddressState()
				newTargetAddrState := tt.currentTargetAddrState
				if utx.Remove && (tt.currentTargetAddrState&txAddrState) != 0 {
					newTargetAddrState ^= txAddrState
				} else if !utx.Remove {
					newTargetAddrState |= txAddrState
				}
				if tt.currentTargetAddrState != newTargetAddrState {
					s.EXPECT().SetAddressStates(utx.Address, newTargetAddrState)
				}

				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.AddressStateTx{
				BaseTx:       baseTx,
				Address:      targetAddr,
				StateBit:     tt.txStateBit,
				Remove:       tt.remove,
				ExecutorAuth: &secp256k1fx.Input{},
			},
			phase: phase,
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
			},
		}
	}
	testCaseOK[codec.UpgradeVersion1] = func(
		t *testing.T,
		tt testData,
		testCaseName string,
		phase test.Phase,
	) {
		testCaseName = fmt.Sprintf("%d_%s/Upgrade 1/%s", phase, test.PhaseName(t, phase), testCaseName)
		require.NotContains(t, testCases, testCaseName, testCaseName)
		targetAddr := otherAddr
		if tt.selfModify {
			targetAddr = executorAddr
		}
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				require.Greater(t, utx.UpgradeVersionID.Version(), uint16(0))
				require.Greater(t, phase, 0)
				if utx.Executor == utx.Address {
					require.Equal(t, as.AddressStateRoleAdmin, tt.currentTargetAddrState)
				}

				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))

				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.Executor}, nil)
				s.EXPECT().GetAddressStates(utx.Executor).Return(tt.executorAddrState, nil)

				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)

				if utx.StateBit == as.AddressStateBitNodeDeferred {
					s.EXPECT().GetShortIDLink(utx.Address, state.ShortLinkKeyRegisterNode).Return(deferredNodeShortID, nil)
					if utx.Remove {
						s.EXPECT().GetDeferredValidator(constants.PrimaryNetworkID, deferredNodeID).Return(deferredStaker, nil)
						s.EXPECT().DeleteDeferredValidator(deferredStaker)
						s.EXPECT().PutCurrentValidator(deferredStaker)
					} else {
						s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, deferredNodeID).Return(deferredStaker, nil)
						s.EXPECT().DeleteCurrentValidator(deferredStaker)
						s.EXPECT().PutDeferredValidator(deferredStaker)
					}
				}

				s.EXPECT().GetAddressStates(utx.Address).Return(tt.currentTargetAddrState, nil)
				txAddrState := utx.StateBit.ToAddressState()
				newTargetAddrState := tt.currentTargetAddrState
				if utx.Remove && (tt.currentTargetAddrState&txAddrState) != 0 {
					newTargetAddrState ^= txAddrState
				} else if !utx.Remove {
					newTargetAddrState |= txAddrState
				}
				if tt.currentTargetAddrState != newTargetAddrState {
					s.EXPECT().SetAddressStates(utx.Address, newTargetAddrState)
				}

				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.AddressStateTx{
				UpgradeVersionID: codec.UpgradeVersion1,
				BaseTx:           baseTx,
				Address:          targetAddr,
				StateBit:         tt.txStateBit,
				Remove:           tt.remove,
				Executor:         executorAddr,
				ExecutorAuth:     &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			phase: phase,
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {executorKey},
			},
		}
	}

	testCaseOKMultiInput := map[codec.UpgradeVersionID]testCaseSimpleFunc{}
	testCaseOKMultiInput[codec.UpgradeVersion0] = func(t *testing.T, phase test.Phase) {
		testCaseName := fmt.Sprintf("%d_%s/Upgrade 0/OK: Second input owner has admin role", phase, test.PhaseName(t, phase))
		require.NotContains(t, testCases, testCaseName, testCaseName)
		testCases[testCaseName] = testCase{
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddressStateTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				require.Zero(t, utx.UpgradeVersionID.Version())

				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(test.PhaseTime(t, phase, cfg))

				// not getting addr state for msigAlias addr
				s.EXPECT().GetAddressStates(feeOwnerAddr).Return(as.AddressStateEmpty, nil)
				s.EXPECT().GetAddressStates(executorAddr).Return(as.AddressStateRoleAdmin, nil)

				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{halfFeeUTXO1, halfFeeUTXO2},
					[]ids.ShortID{feeOwnerAddr, executorAddr}, nil)

				s.EXPECT().GetAddressStates(utx.Address).Return(as.AddressStateEmpty, nil)
				s.EXPECT().SetAddressStates(utx.Address, as.AddressStateKYCVerified)

				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.AddressStateTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.InFromUTXO(t, halfFeeUTXO1, []uint32{0}, false),
						generate.InFromUTXO(t, halfFeeUTXO2, []uint32{0}, false),
					},
				}},
				Address:      otherAddr,
				StateBit:     as.AddressStateBitKYCVerified,
				ExecutorAuth: &secp256k1fx.Input{},
			},
			phase: phase,
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {executorKey},
			},
		}
	}
	testCaseOKMultiInput[codec.UpgradeVersion1] = failCaseSimpleNoOp

	simpleOKCases := map[string]testData{
		"OK: remove other admin": {
			txStateBit:             as.AddressStateBitRoleAdmin,
			remove:                 true,
			currentTargetAddrState: as.AddressStateRoleAdmin,
			executorAddrState:      as.AddressStateRoleAdmin,
		},
		"OK: target state is not empty": {
			txStateBit:             as.AddressStateBitKYCVerified,
			currentTargetAddrState: as.AddressStateConsortium,
			executorAddrState:      as.AddressStateRoleAdmin,
		},
		"OK: not modified": {
			txStateBit:             as.AddressStateBitKYCVerified,
			currentTargetAddrState: as.AddressStateKYCVerified,
			executorAddrState:      as.AddressStateRoleAdmin,
		},
		"OK: modifying executors own address state": {
			selfModify:             true,
			txStateBit:             as.AddressStateBitKYCVerified,
			currentTargetAddrState: as.AddressStateRoleAdmin,
			executorAddrState:      as.AddressStateRoleAdmin,
		},
		"OK: removal": {
			txStateBit:             as.AddressStateBitKYCVerified,
			remove:                 true,
			currentTargetAddrState: as.AddressStateKYCVerified,
			executorAddrState:      as.AddressStateRoleAdmin,
		},
		"OK: removal, not modified": {
			txStateBit:             as.AddressStateBitKYCVerified,
			remove:                 true,
			currentTargetAddrState: as.AddressStateConsortium,
			executorAddrState:      as.AddressStateRoleAdmin,
		},
	}

	validBits := getBitsFromAddressState(as.AddressStateValidBits)

	// set role-bit permissions
	permissionsMatrix := map[as.AddressStateBit]map[as.AddressStateBit]bool{}
	for _, role := range validBits {
		permissionsMatrix[role] = map[as.AddressStateBit]bool{}
		for _, bit := range validBits {
			permissionsMatrix[role][bit] = false
		}
	}
	for _, bit := range validBits {
		permissionsMatrix[as.AddressStateBitRoleAdmin][bit] = true
	}
	permissionsMatrix[as.AddressStateBitRoleKYCAdmin][as.AddressStateBitKYCVerified] = true
	permissionsMatrix[as.AddressStateBitRoleKYCAdmin][as.AddressStateBitKYCExpired] = true
	permissionsMatrix[as.AddressStateBitRoleOffersAdmin][as.AddressStateBitOffersCreator] = true

	// set phase-bit restrictions
	bitsPhaseMatrix := map[as.AddressStateBit]map[test.Phase]error{}
	for _, bit := range validBits {
		bitsPhaseMatrix[bit] = map[test.Phase]error{}
		for phase := test.PhaseFirst; phase <= test.PhaseLast; phase++ {
			bitsPhaseMatrix[bit][phase] = nil
		}
	}
	// sunriseBits := getBitsFromAddressState(as.AddressStateSunrisePhaseBits)
	athensBits := getBitsFromAddressState(as.AddressStateAthensPhaseBits)
	berlinBits := getBitsFromAddressState(as.AddressStateBerlinPhaseBits)
	for _, bit := range athensBits {
		for phase := test.PhaseFirst; phase < test.PhaseAthens; phase++ {
			bitsPhaseMatrix[bit][phase] = errNotAthensPhase
		}
	}
	for _, bit := range berlinBits {
		for phase := test.PhaseFirst; phase < test.PhaseBerlin; phase++ {
			bitsPhaseMatrix[bit][phase] = errNotBerlinPhase
		}
	}
	bitsPhaseMatrix[as.AddressStateBitConsortium][test.PhaseBerlin] = errBerlinPhase

	// set phase-txUpgrade restrictions := getBitsFromAddressState(as.AddressStateSunrisePhaseBits)
	txUpgradeMatrix := map[test.Phase][]codec.UpgradeVersionID{}
	for phase := test.PhaseFirst; phase < test.PhaseBerlin; phase++ {
		txUpgradeMatrix[phase] = append(txUpgradeMatrix[phase], codec.UpgradeVersion0)
	}
	for phase := test.PhaseAthens; phase <= test.PhaseLast; phase++ {
		txUpgradeMatrix[phase] = append(txUpgradeMatrix[phase], codec.UpgradeVersion1)
	}

	for role, permissions := range permissionsMatrix {
		for bit, allowed := range permissions {
			if allowed {
				for phase := test.PhaseFirst; phase <= test.PhaseLast; phase++ {
					if bitsPhaseMatrix[bit][phase] != nil {
						continue
					}
					txUpgrades := txUpgradeMatrix[phase]
					for _, txUpgrade := range txUpgrades {
						if bit == as.AddressStateBitConsortium {
							// resume node, defer is tested below
							testCaseOK[txUpgrade](t, testData{
								txStateBit:        bit,
								executorAddrState: role.ToAddressState(),
								remove:            true,
							}, fmt.Sprintf("OK: (%0d) modifies (%0d), resume node", role, bit), phase)
						}
						testCaseOK[txUpgrade](t, testData{
							txStateBit:        bit,
							executorAddrState: role.ToAddressState(),
						}, fmt.Sprintf("OK: (%0d) modifies (%0d)", role, bit), phase)
					}
				}
			} else {
				for phase := test.PhaseFirst; phase <= test.PhaseLast; phase++ {
					if bitsPhaseMatrix[bit][phase] != nil {
						continue
					}
					txUpgrades := txUpgradeMatrix[phase]
					for _, txUpgrade := range txUpgrades {
						testCaseFailNoPermission[txUpgrade](t, testData{
							txStateBit:        bit,
							executorAddrState: role.ToAddressState(),
						}, fmt.Sprintf("Fail: (%0d) modifies (%0d)", role, bit), phase)
					}
				}
			}
		}
	}

	for _, bit := range validBits {
		for phase := test.PhaseFirst; phase <= test.PhaseLast; phase++ {
			expectedErr := bitsPhaseMatrix[bit][phase]
			if expectedErr == nil {
				continue
			}
			txUpgrades := txUpgradeMatrix[phase]
			for _, txUpgrade := range txUpgrades {
				testCaseFailBitForbidden[txUpgrade](t, testData{txStateBit: bit},
					fmt.Sprintf("Forbid bit %d", bit), expectedErr, phase)
			}
		}
	}

	for phase := test.PhaseFirst; phase <= test.PhaseLast; phase++ {
		txUpgrades := txUpgradeMatrix[phase]
		for _, txUpgrade := range txUpgrades {
			for name, tt := range simpleOKCases {
				testCaseOK[txUpgrade](t, tt, name, phase)
			}
			testCaseOKMultiInput[txUpgrade](t, phase) // noop for upgr1
			testCaseFailAdminSelfRemove[txUpgrade](t, phase)
			testCaseFailWrongExecutorCredential[txUpgrade](t, phase) // noop for upgr0 or sunrise phase
			testCaseFailMultisigAlias[txUpgrade](t, phase)           // noop for upgr1
		}
	}

	testCaseFailUpgradeVersionForbidden[codec.UpgradeVersion1](t, test.PhaseSunrise)
	for phase := test.PhaseBerlin; phase <= test.PhaseLast; phase++ {
		testCaseFailUpgradeVersionForbidden[codec.UpgradeVersion0](t, phase)
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			backend := newExecutorBackend(t, caminoGenesisConf, tt.phase, nil)

			avax.SortTransferableInputsWithSigners(tt.utx.Ins, tt.signers)
			avax.SortTransferableOutputs(tt.utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(t, gomock.NewController(t), tt.utx, tx.ID(), backend.Config),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorDepositTx(t *testing.T) {
	ctx := test.Context(t)

	feeOwnerKey, feeOwnerAddr, feeOwner := generate.KeyAndOwner(t, test.Keys[0])
	utxoOwnerKey, utxoOwnerAddr, utxoOwner := generate.KeyAndOwner(t, test.Keys[1])
	_, newUTXOOwnerAddr, newUTXOOwner := generate.KeyAndOwner(t, test.Keys[2])
	offerOwnerKey, offerOwnerAddr := test.Keys[3], test.Keys[3].Address()
	depositCreatorKey, depositCreatorAddr := test.Keys[4], test.Keys[4].Address()

	offer := &deposit.Offer{
		ID:          ids.ID{0, 0, 1},
		End:         100,
		MinAmount:   2,
		MinDuration: 10,
		MaxDuration: 20,
	}

	offerWithMaxAmount := &deposit.Offer{
		ID:              ids.ID{0, 0, 2},
		End:             100,
		MinAmount:       2,
		MinDuration:     10,
		MaxDuration:     20,
		TotalMaxAmount:  200,
		DepositedAmount: 100,
	}

	offerWithMaxRewardAmount := &deposit.Offer{
		ID:                    ids.ID{0, 0, 3},
		End:                   100,
		MinAmount:             2,
		MinDuration:           10,
		MaxDuration:           2000000000, // can't use small numbers, cause reward could be the same with small "steps"
		InterestRateNominator: 30000000,
		TotalMaxRewardAmount:  2000000,
		RewardedAmount:        1000000,
	}

	offerWithOwner := &deposit.Offer{
		ID:           ids.ID{0, 0, 4},
		End:          100,
		MinAmount:    2,
		MinDuration:  10,
		MaxDuration:  20,
		OwnerAddress: offerOwnerAddr,
	}

	feeUTXO := generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.TxFee, feeOwner, ids.Empty, ids.Empty, true)
	doubleFeeUTXO := generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.TxFee*2, feeOwner, ids.Empty, ids.Empty, true)
	unlockedUTXO1 := generate.UTXO(ids.ID{2}, ctx.AVAXAssetID, offer.MinAmount, utxoOwner, ids.Empty, ids.Empty, true)
	unlockedUTXO2 := generate.UTXO(ids.ID{3}, ctx.AVAXAssetID, offerWithMaxAmount.RemainingAmount(), utxoOwner, ids.Empty, ids.Empty, true)
	unlockedUTXO3 := generate.UTXO(ids.ID{4}, ctx.AVAXAssetID, offerWithMaxRewardAmount.MaxRemainingAmountByReward(), utxoOwner, ids.Empty, ids.Empty, true)
	bondedUTXOWithMinAmount := generate.UTXO(ids.ID{4}, ctx.AVAXAssetID, offer.MinAmount, utxoOwner, ids.Empty, ids.ID{100}, true)

	phases := []struct {
		name    string
		prepare func(*Backend, time.Time)
	}{
		{
			name: "SunrisePhase0",
			prepare: func(b *Backend, chaintime time.Time) {
				b.Config.AthensPhaseTime = chaintime.Add(1 * time.Second)
			},
		},
		{
			name: "AthensPhase",
			prepare: func(b *Backend, chaintime time.Time) {
				b.Config.AthensPhaseTime = chaintime
			},
		},
	}

	tests := map[string]struct {
		caminoGenesisConf   api.Camino
		state               func(*testing.T, *gomock.Controller, *txs.DepositTx, ids.ID, *config.Config, int) *state.MockDiff
		utx                 func() *txs.DepositTx
		chaintime           time.Time
		signers             [][]*secp256k1.PrivateKey
		offerPermissionCred func(*testing.T) *secp256k1fx.Credential
		expectedErr         []error // expectedErr[i] is expected for phase[i]
	}{
		"Wrong lockModeBondDeposit flag": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: false}, nil)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
					}},
					RewardsOwner: &secp256k1fx.OutputOwners{},
				}
			},
			expectedErr: []error{errWrongLockMode, errWrongLockMode},
		},
		"Stakeable ins": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.StakeableIn(ctx.AVAXAssetID, test.PreFundedBalance, uint64(test.MinStakingDuration), []uint32{0}),
						},
					}},
					RewardsOwner: &secp256k1fx.OutputOwners{},
				}
			},
			expectedErr: []error{locked.ErrWrongInType, locked.ErrWrongInType},
		},
		"Stakeable outs": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Outs: []*avax.TransferableOutput{
							generate.StakeableOut(ctx.AVAXAssetID, test.PreFundedBalance, uint64(test.MinStakingDuration), utxoOwner),
						},
					}},
					RewardsOwner: &secp256k1fx.OutputOwners{},
				}
			},
			expectedErr: []error{locked.ErrWrongOutType, locked.ErrWrongOutType},
		},
		"Not existing deposit offer ID": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(nil, database.ErrNotFound)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
					}},
					DepositOfferID: offer.ID,
					RewardsOwner:   &secp256k1fx.OutputOwners{},
				}
			},
			expectedErr: []error{database.ErrNotFound, database.ErrNotFound},
		},
		"Deposit offer is inactive by flag": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(&deposit.Offer{Flags: deposit.OfferFlagLocked}, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
					}},
					DepositOfferID: offer.ID,
					RewardsOwner:   &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offer.StartTime(),
			expectedErr: []error{errDepositOfferInactive, errDepositOfferInactive},
		},
		"Deposit offer is not active yet": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime().Add(-1 * time.Second))
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
					}},
					DepositOfferID: offer.ID,
					RewardsOwner:   &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offer.StartTime().Add(-1),
			expectedErr: []error{errDepositOfferInactive, errDepositOfferInactive},
		},
		"Deposit offer has expired": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.EndTime().Add(time.Second))
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
					}},
					DepositOfferID: offer.ID,
					RewardsOwner:   &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offer.EndTime().Add(time.Second),
			expectedErr: []error{errDepositOfferInactive, errDepositOfferInactive},
		},
		"Deposit duration is too small": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration - 1,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offer.StartTime(),
			expectedErr: []error{errDepositDurationTooSmall, errDepositDurationTooSmall},
		},
		"Deposit duration is too big": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MaxDuration + 1,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offer.StartTime(),
			expectedErr: []error{errDepositDurationTooBig, errDepositDurationTooBig},
		},
		"Deposit amount is too small": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount-1, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offer.StartTime(),
			expectedErr: []error{errDepositTooSmall, errDepositTooSmall},
		},
		"Deposit amount is too big (offer.TotalMaxAmount)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithMaxAmount, nil)
				s.EXPECT().GetTimestamp().Return(offerWithMaxAmount.StartTime())
				return s
			},
			utx: func() *txs.DepositTx {
				amt := offerWithMaxAmount.RemainingAmount() + 1
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, amt, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offerWithMaxAmount.ID,
					DepositDuration: offerWithMaxAmount.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offerWithMaxAmount.StartTime(),
			expectedErr: []error{errDepositTooBig, errDepositTooBig},
		},
		"Deposit amount is too big (offer.TotalMaxRewardAmount)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithMaxRewardAmount, nil)
				s.EXPECT().GetTimestamp().Return(offerWithMaxRewardAmount.StartTime())
				return s
			},
			utx: func() *txs.DepositTx {
				amt := offerWithMaxRewardAmount.MaxRemainingAmountByReward() + 1
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, amt, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offerWithMaxRewardAmount.ID,
					DepositDuration: offerWithMaxRewardAmount.MaxDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offerWithMaxRewardAmount.StartTime(),
			expectedErr: []error{errNotAthensPhase, errDepositTooBig},
		},
		"UTXO not found": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{unlockedUTXO1, nil}, nil, nil)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
							generate.InFromUTXO(t, bondedUTXOWithMinAmount, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offer.StartTime(),
			expectedErr: []error{errFlowCheckFailed, errFlowCheckFailed},
		},
		"Inputs and credentials length mismatch": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{unlockedUTXO1}, nil, nil)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offer.StartTime(),
			signers:     [][]*secp256k1.PrivateKey{{utxoOwnerKey}, {utxoOwnerKey}},
			expectedErr: []error{errFlowCheckFailed, errFlowCheckFailed},
		},
		"Owned offer, bad offer permission credential (wrong deposit creator addr)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithOwner, nil)
				s.EXPECT().GetTimestamp().Return(offerWithOwner.StartTime())
				if phaseIndex > 0 { // if Athens
					expect.VerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress}, nil)
				}
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					UpgradeVersionID: codec.UpgradeVersion1,
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:        offerWithOwner.ID,
					DepositDuration:       offerWithOwner.MaxDuration,
					RewardsOwner:          &secp256k1fx.OutputOwners{},
					DepositCreatorAddress: depositCreatorAddr,
					DepositCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					DepositOfferOwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			chaintime: offerWithOwner.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}, {depositCreatorKey}},
			offerPermissionCred: func(t *testing.T) *secp256k1fx.Credential {
				hash := hashing.ComputeHash256(offerWithOwner.PermissionMsg(utxoOwnerAddr)) // not depositCreatorAddr
				sig, err := offerOwnerKey.SignHash(hash)
				require.NoError(t, err)
				cred := &secp256k1fx.Credential{
					Sigs: make([][secp256k1.SignatureLen]byte, 1),
				}
				copy(cred.Sigs[0][:], sig)
				return cred
			},
			expectedErr: []error{errNotAthensPhase, errOfferPermissionCredentialMismatch},
		},
		"Owned offer, bad offer permission credential (wrong offer owner key)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithOwner, nil)
				s.EXPECT().GetTimestamp().Return(offerWithOwner.StartTime())
				if phaseIndex > 0 { // if Athens
					expect.VerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress}, nil)
				}
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					UpgradeVersionID: codec.UpgradeVersion1,
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:        offerWithOwner.ID,
					DepositDuration:       offerWithOwner.MaxDuration,
					RewardsOwner:          &secp256k1fx.OutputOwners{},
					DepositCreatorAddress: depositCreatorAddr,
					DepositCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					DepositOfferOwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			chaintime: offerWithOwner.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}, {depositCreatorKey}},
			offerPermissionCred: func(t *testing.T) *secp256k1fx.Credential {
				hash := hashing.ComputeHash256(offerWithOwner.PermissionMsg(depositCreatorAddr))
				sig, err := utxoOwnerKey.SignHash(hash) // not offerOwnerKey
				require.NoError(t, err)
				cred := &secp256k1fx.Credential{
					Sigs: make([][secp256k1.SignatureLen]byte, 1),
				}
				copy(cred.Sigs[0][:], sig)
				return cred
			},
			expectedErr: []error{errNotAthensPhase, errOfferPermissionCredentialMismatch},
		},
		"Owned offer, bad offer permission credential (wrong offer owner auth)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithOwner, nil)
				s.EXPECT().GetTimestamp().Return(offerWithOwner.StartTime())
				if phaseIndex > 0 { // if Athens
					expect.VerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress}, nil)
				}
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					UpgradeVersionID: codec.UpgradeVersion1,
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:        offerWithOwner.ID,
					DepositDuration:       offerWithOwner.MaxDuration,
					RewardsOwner:          &secp256k1fx.OutputOwners{},
					DepositCreatorAddress: depositCreatorAddr,
					DepositCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					DepositOfferOwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{1}}, // wrong index
				}
			},
			chaintime: offerWithOwner.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}, {depositCreatorKey}},
			offerPermissionCred: func(t *testing.T) *secp256k1fx.Credential {
				hash := hashing.ComputeHash256(offerWithOwner.PermissionMsg(depositCreatorAddr))
				sig, err := offerOwnerKey.SignHash(hash)
				require.NoError(t, err)
				cred := &secp256k1fx.Credential{
					Sigs: make([][secp256k1.SignatureLen]byte, 1),
				}
				copy(cred.Sigs[0][:], sig)
				return cred
			},
			expectedErr: []error{errNotAthensPhase, errOfferPermissionCredentialMismatch},
		},
		"Owned offer, bad deposit creator credential (wrong key)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithOwner, nil)
				s.EXPECT().GetTimestamp().Return(offerWithOwner.StartTime())
				if phaseIndex > 0 { // if Athens
					expect.VerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress, utx.DepositCreatorAddress}, nil)
				}
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					UpgradeVersionID: codec.UpgradeVersion1,
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:        offerWithOwner.ID,
					DepositDuration:       offerWithOwner.MaxDuration,
					RewardsOwner:          &secp256k1fx.OutputOwners{},
					DepositCreatorAddress: depositCreatorAddr,
					DepositCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					DepositOfferOwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			chaintime: offerWithOwner.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}, {utxoOwnerKey}},
			offerPermissionCred: func(t *testing.T) *secp256k1fx.Credential {
				hash := hashing.ComputeHash256(offerWithOwner.PermissionMsg(depositCreatorAddr))
				sig, err := offerOwnerKey.SignHash(hash)
				require.NoError(t, err)
				cred := &secp256k1fx.Credential{
					Sigs: make([][secp256k1.SignatureLen]byte, 1),
				}
				copy(cred.Sigs[0][:], sig)
				return cred
			},
			expectedErr: []error{errNotAthensPhase, errDepositCreatorCredentialMismatch},
		},
		"Owned offer, bad deposit creator credential (wrong auth)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithOwner, nil)
				s.EXPECT().GetTimestamp().Return(offerWithOwner.StartTime())
				if phaseIndex > 0 { // if Athens
					expect.VerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress, utx.DepositCreatorAddress}, nil)
				}
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					UpgradeVersionID: codec.UpgradeVersion1,
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:        offerWithOwner.ID,
					DepositDuration:       offerWithOwner.MaxDuration,
					RewardsOwner:          &secp256k1fx.OutputOwners{},
					DepositCreatorAddress: depositCreatorAddr,
					DepositCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{1}},
					DepositOfferOwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			chaintime: offerWithOwner.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}, {depositCreatorKey}},
			offerPermissionCred: func(t *testing.T) *secp256k1fx.Credential {
				hash := hashing.ComputeHash256(offerWithOwner.PermissionMsg(depositCreatorAddr))
				sig, err := offerOwnerKey.SignHash(hash)
				require.NoError(t, err)
				cred := &secp256k1fx.Credential{
					Sigs: make([][secp256k1.SignatureLen]byte, 1),
				}
				copy(cred.Sigs[0][:], sig)
				return cred
			},
			expectedErr: []error{errNotAthensPhase, errDepositCreatorCredentialMismatch},
		},
		"Supply overflow": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, unlockedUTXO1},
					[]ids.ShortID{
						feeOwnerAddr, utxoOwnerAddr, // consumed
						utxoOwnerAddr, // produced
					}, nil)

				deposit1 := &deposit.Deposit{
					DepositOfferID: utx.DepositOfferID,
					Duration:       utx.DepositDuration,
					Amount:         utx.DepositAmount(),
					Start:          offer.Start, // current chaintime
				}
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-deposit1.TotalReward(offer)+1, nil)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offer.StartTime(),
			signers:     [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}},
			expectedErr: []error{errSupplyOverflow, errSupplyOverflow},
		},
		"OK": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, unlockedUTXO1},
					[]ids.ShortID{
						feeOwnerAddr, utxoOwnerAddr, // consumed
						utxoOwnerAddr, // produced
					}, nil)

				deposit1 := &deposit.Deposit{
					DepositOfferID: utx.DepositOfferID,
					Duration:       utx.DepositDuration,
					Amount:         utx.DepositAmount(),
					Start:          offer.Start, // current chaintime
					RewardOwner:    utx.RewardsOwner,
				}
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-deposit1.TotalReward(offer), nil)
				s.EXPECT().AddDeposit(txID, deposit1)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime: offer.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}},
		},
		"OK: fee change to new address": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{doubleFeeUTXO, unlockedUTXO1},
					[]ids.ShortID{
						feeOwnerAddr, utxoOwnerAddr, // consumed
						newUTXOOwnerAddr, utxoOwnerAddr, // produced
					}, nil)

				deposit1 := &deposit.Deposit{
					DepositOfferID: utx.DepositOfferID,
					Duration:       utx.DepositDuration,
					Amount:         utx.DepositAmount(),
					Start:          offer.Start, // current chaintime
					RewardOwner:    utx.RewardsOwner,
				}
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-deposit1.TotalReward(offer), nil)
				s.EXPECT().AddDeposit(txID, deposit1)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, doubleFeeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, test.TxFee, newUTXOOwner, ids.Empty, ids.Empty),
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime: offer.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}},
		},
		"OK: deposit bonded": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, bondedUTXOWithMinAmount},
					[]ids.ShortID{
						feeOwnerAddr, utxoOwnerAddr, // consumed
						utxoOwnerAddr, // produced
					}, nil)

				deposit1 := &deposit.Deposit{
					DepositOfferID: utx.DepositOfferID,
					Duration:       utx.DepositDuration,
					Amount:         utx.DepositAmount(),
					Start:          offer.Start, // current chaintime
					RewardOwner:    utx.RewardsOwner,
				}
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-deposit1.TotalReward(offer), nil)
				s.EXPECT().AddDeposit(txID, deposit1)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, bondedUTXOWithMinAmount, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.ID{100}),
						},
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime: offer.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}},
		},
		"OK: deposit bonded and unlocked": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, unlockedUTXO1, bondedUTXOWithMinAmount},
					[]ids.ShortID{
						feeOwnerAddr, utxoOwnerAddr, utxoOwnerAddr, // consumed
						utxoOwnerAddr, utxoOwnerAddr, // produced
					}, nil)

				deposit1 := &deposit.Deposit{
					DepositOfferID: utx.DepositOfferID,
					Duration:       utx.DepositDuration,
					Amount:         utx.DepositAmount(),
					Start:          offer.Start, // current chaintime
					RewardOwner:    utx.RewardsOwner,
				}
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-deposit1.TotalReward(offer), nil)
				s.EXPECT().AddDeposit(txID, deposit1)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
							generate.InFromUTXO(t, bondedUTXOWithMinAmount, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.ID{100}),
						},
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime: offer.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}, {utxoOwnerKey}},
		},
		"OK: deposited for new owner": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offer, nil)
				s.EXPECT().GetTimestamp().Return(offer.StartTime())
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, unlockedUTXO1},
					[]ids.ShortID{
						feeOwnerAddr, utxoOwnerAddr, // consumed
						newUTXOOwnerAddr, // produced
					}, nil)

				deposit1 := &deposit.Deposit{
					DepositOfferID: utx.DepositOfferID,
					Duration:       utx.DepositDuration,
					Amount:         utx.DepositAmount(),
					Start:          offer.Start, // current chaintime
					RewardOwner:    utx.RewardsOwner,
				}
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-deposit1.TotalReward(offer), nil)
				s.EXPECT().AddDeposit(txID, deposit1)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, newUTXOOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offer.ID,
					DepositDuration: offer.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime: offer.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}},
		},
		"OK: deposit offer with max amount": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithMaxAmount, nil)
				s.EXPECT().GetTimestamp().Return(offerWithMaxAmount.StartTime())
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, unlockedUTXO2},
					[]ids.ShortID{
						feeOwnerAddr, utxoOwnerAddr, // consumed
						utxoOwnerAddr, // produced
					}, nil)

				deposit1 := &deposit.Deposit{
					DepositOfferID: utx.DepositOfferID,
					Duration:       utx.DepositDuration,
					Amount:         utx.DepositAmount(),
					Start:          offerWithMaxAmount.Start, // current chaintime
					RewardOwner:    utx.RewardsOwner,
				}
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-deposit1.TotalReward(offerWithMaxAmount), nil)
				updatedOffer := *offerWithMaxAmount
				updatedOffer.DepositedAmount += utx.DepositAmount()
				s.EXPECT().SetDepositOffer(&updatedOffer)
				s.EXPECT().AddDeposit(txID, deposit1)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				amt := offerWithMaxAmount.RemainingAmount()
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO2, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, amt, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offerWithMaxAmount.ID,
					DepositDuration: offerWithMaxAmount.MinDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime: offerWithMaxAmount.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}},
		},
		"OK: deposit offer with max reward amount": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithMaxRewardAmount, nil)
				s.EXPECT().GetTimestamp().Return(offerWithMaxRewardAmount.StartTime())
				if phaseIndex > 0 {
					s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
					expect.VerifyLock(t, s, utx.Ins,
						[]*avax.UTXO{feeUTXO, unlockedUTXO3},
						[]ids.ShortID{
							feeOwnerAddr, utxoOwnerAddr, // consumed
							utxoOwnerAddr, // produced
						}, nil)

					deposit1 := &deposit.Deposit{
						DepositOfferID: utx.DepositOfferID,
						Duration:       utx.DepositDuration,
						Amount:         utx.DepositAmount(),
						Start:          offerWithMaxRewardAmount.Start, // current chaintime
						RewardOwner:    utx.RewardsOwner,
					}
					s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
						Return(cfg.RewardConfig.SupplyCap-deposit1.TotalReward(offerWithMaxRewardAmount), nil)
					updatedOffer := *offerWithMaxRewardAmount
					updatedOffer.RewardedAmount += deposit1.TotalReward(offerWithMaxRewardAmount)
					s.EXPECT().SetDepositOffer(&updatedOffer)
					s.EXPECT().SetCurrentSupply(constants.PrimaryNetworkID, cfg.RewardConfig.SupplyCap)
					s.EXPECT().AddDeposit(txID, deposit1)
					expect.ConsumeUTXOs(t, s, utx.Ins)
					expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				}
				return s
			},
			utx: func() *txs.DepositTx {
				amt := offerWithMaxRewardAmount.MaxRemainingAmountByReward()
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO3, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, amt, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:  offerWithMaxRewardAmount.ID,
					DepositDuration: offerWithMaxRewardAmount.MaxDuration,
					RewardsOwner:    &secp256k1fx.OutputOwners{},
				}
			},
			chaintime:   offerWithMaxRewardAmount.StartTime(),
			signers:     [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}},
			expectedErr: []error{errNotAthensPhase},
		},
		"OK|Fail: deposit offer with owner": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.DepositTx, txID ids.ID, cfg *config.Config, phaseIndex int) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetDepositOffer(utx.DepositOfferID).Return(offerWithOwner, nil)
				s.EXPECT().GetTimestamp().Return(offerWithOwner.StartTime())
				if phaseIndex > 0 { // if Athens
					expect.VerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress, utx.DepositCreatorAddress}, nil)
					s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
					expect.VerifyLock(t, s, utx.Ins,
						[]*avax.UTXO{feeUTXO, unlockedUTXO1},
						[]ids.ShortID{
							feeOwnerAddr, utxoOwnerAddr, // consumed
							utxoOwnerAddr, // produced
						}, nil)

					deposit1 := &deposit.Deposit{
						DepositOfferID: utx.DepositOfferID,
						Duration:       utx.DepositDuration,
						Amount:         utx.DepositAmount(),
						Start:          offerWithOwner.Start, // current chaintime
						RewardOwner:    utx.RewardsOwner,
					}
					s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
						Return(cfg.RewardConfig.SupplyCap-deposit1.TotalReward(offer), nil)
					s.EXPECT().AddDeposit(txID, deposit1)
					expect.ConsumeUTXOs(t, s, utx.Ins)
					expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				}
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					UpgradeVersionID: codec.UpgradeVersion1,
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, unlockedUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
						},
					}},
					DepositOfferID:        offerWithOwner.ID,
					DepositDuration:       offerWithOwner.MaxDuration,
					RewardsOwner:          &secp256k1fx.OutputOwners{},
					DepositCreatorAddress: depositCreatorAddr,
					DepositCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					DepositOfferOwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			chaintime: offerWithOwner.StartTime(),
			signers:   [][]*secp256k1.PrivateKey{{feeOwnerKey}, {utxoOwnerKey}, {depositCreatorKey}},
			offerPermissionCred: func(t *testing.T) *secp256k1fx.Credential {
				hash := hashing.ComputeHash256(offerWithOwner.PermissionMsg(depositCreatorAddr))
				sig, err := offerOwnerKey.SignHash(hash)
				require.NoError(t, err)
				cred := &secp256k1fx.Credential{
					Sigs: make([][secp256k1.SignatureLen]byte, 1),
				}
				copy(cred.Sigs[0][:], sig)
				return cred
			},
			expectedErr: []error{errNotAthensPhase},
		},
	}
	for name, tt := range tests {
		for phaseIndex, phase := range phases {
			t.Run(fmt.Sprintf("%s, %s", phase.name, name), func(t *testing.T) {
				backend := newExecutorBackend(t, tt.caminoGenesisConf, test.PhaseLast, nil)

				phase.prepare(backend, tt.chaintime)

				utx := tt.utx()
				avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
				avax.SortTransferableOutputs(utx.Outs, txs.Codec)
				tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
				require.NoError(t, err)

				// creating offer permission cred
				if tt.offerPermissionCred != nil {
					tx.Creds = append(tx.Creds, tt.offerPermissionCred(t))
					signedBytes, err := txs.Codec.Marshal(txs.Version, tx)
					require.NoError(t, err)
					tx.SetBytes(tx.Unsigned.Bytes(), signedBytes)
				}

				err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
					StandardTxExecutor{
						Backend: backend,
						State:   tt.state(t, gomock.NewController(t), utx, tx.ID(), backend.Config, phaseIndex),
						Tx:      tx,
					},
				})
				var expectedErr error
				if phaseIndex < len(tt.expectedErr) {
					expectedErr = tt.expectedErr[phaseIndex]
				}
				require.ErrorIs(t, err, expectedErr)
			})
		}
	}
}

func TestCaminoStandardTxExecutorUnlockDepositTx(t *testing.T) {
	ctx := test.Context(t)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generate.KeyAndOwner(t, test.Keys[0])
	owner1Key, owner1Addr, owner1 := generate.KeyAndOwner(t, test.Keys[1])
	owner1ID, err := txs.GetOwnerID(owner1)
	require.NoError(t, err)
	depositTxID1 := ids.ID{0, 0, 1}
	depositWithRewardTxID1 := ids.ID{0, 0, 2}
	depositTxID2 := ids.ID{0, 0, 3}

	depositOffer := &deposit.Offer{
		ID:                   ids.ID{0, 1},
		MinAmount:            1,
		MinDuration:          60,
		MaxDuration:          80,
		UnlockPeriodDuration: 50,
	}
	depositOfferWithReward := &deposit.Offer{
		ID:                    ids.ID{0, 2},
		MinAmount:             1,
		MinDuration:           60,
		MaxDuration:           60,
		UnlockPeriodDuration:  50,
		InterestRateNominator: 365 * 24 * 60 * 60 * 1_000_000 / 10, // 10%
	}
	deposit1 := &deposit.Deposit{
		Duration:       depositOffer.MinDuration,
		Amount:         10000,
		DepositOfferID: depositOffer.ID,
	}
	deposit1WithReward := &deposit.Deposit{
		Duration:       depositOfferWithReward.MinDuration,
		Amount:         10000,
		DepositOfferID: depositOfferWithReward.ID,
		RewardOwner:    &owner1,
	}
	deposit2 := &deposit.Deposit{
		Duration:       depositOffer.MaxDuration,
		Amount:         20000,
		DepositOfferID: depositOffer.ID,
	}

	deposit1StartUnlockTime := deposit1.StartTime().
		Add(time.Duration(deposit1.Duration) * time.Second).
		Add(-time.Duration(depositOffer.UnlockPeriodDuration) * time.Second)
	deposit1HalfUnlockTime := deposit1.StartTime().
		Add(time.Duration(deposit1.Duration) * time.Second).
		Add(-time.Duration(depositOffer.UnlockPeriodDuration/2) * time.Second)
	deposit1Expired := deposit1.StartTime().
		Add(time.Duration(deposit1.Duration) * time.Second)

	deposit1HalfUnlockableAmount := deposit1.UnlockableAmount(depositOffer, uint64(deposit1HalfUnlockTime.Unix()))
	deposit2HalfUnlockableAmount := deposit2.UnlockableAmount(depositOffer, uint64(deposit1HalfUnlockTime.Unix()))

	feeUTXO := generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, test.TxFee, feeOwner, ids.Empty, ids.Empty, true)
	lessFeeUTXO := generate.UTXO(ids.ID{2}, ctx.AVAXAssetID, 1, feeOwner, ids.Empty, ids.Empty, true)
	deposit1UTXO := generate.UTXO(ids.ID{3}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty, true)
	deposit2UTXO := generate.UTXO(ids.ID{4}, ctx.AVAXAssetID, deposit2.Amount, owner1, depositTxID2, ids.Empty, true)
	deposit1WithRewardUTXO := generate.UTXO(ids.ID{5}, ctx.AVAXAssetID, deposit1WithReward.Amount, owner1, depositWithRewardTxID1, ids.Empty, true)
	deposit1UTXOLargerTxID := generate.UTXO(ids.ID{6}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty, true)
	unlockedUTXOWithLargerTxID := generate.UTXO(ids.ID{7}, ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty, true)

	tests := map[string]struct {
		state       func(*testing.T, *gomock.Controller, *txs.UnlockDepositTx, ids.ID) *state.MockDiff
		utx         *txs.UnlockDepositTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Wrong lockModeBondDeposit flag": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: false}, nil)
				return s
			},
			utx:         &txs.UnlockDepositTx{},
			expectedErr: errWrongLockMode,
		},
		"Unlock before deposit's unlock period": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1StartUnlockTime.Add(-1 * time.Second))
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyUnlockDeposit(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, deposit1UTXO},
					[]ids.ShortID{
						feeOwnerAddr, owner1Addr, // consumed (not expired deposit)
						owner1Addr, // produced unlocked
					}, nil)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil).Times(2)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{feeUTXO, deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generate.Out(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
					generate.Out(ctx.AVAXAssetID, deposit1.Amount-1, owner1, depositTxID1, ids.Empty),
				},
			}}},
			signers:     [][]*secp256k1.PrivateKey{{feeOwnerKey}, {owner1Key}},
			expectedErr: errUnlockedMoreThanAvailable,
		},
		"Unlock expired deposit, tx has unlocked input before deposited input": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{feeUTXO, deposit1UTXO}),
			}}},
			signers:     [][]*secp256k1.PrivateKey{},
			expectedErr: errMixedDeposits,
		},
		"Unlock expired deposit, tx has unlocked input after deposited input": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{deposit1UTXO, unlockedUTXOWithLargerTxID}),
			}}},
			signers:     [][]*secp256k1.PrivateKey{},
			expectedErr: errMixedDeposits,
		},
		"Unlock active and expired deposits": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				s.EXPECT().GetDeposit(depositTxID2).Return(deposit2, nil)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{deposit2UTXO, deposit1UTXOLargerTxID}),
			}}},
			signers:     [][]*secp256k1.PrivateKey{},
			expectedErr: errMixedDeposits,
		},
		"Unlock not full amount, deposit expired": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				expect.VerifyUnlockDeposit(t, s, utx.Ins,
					[]*avax.UTXO{deposit1UTXO},
					[]ids.ShortID{
						owner1Addr, // produced unlocked
					}, nil)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil).Times(2)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generate.Out(ctx.AVAXAssetID, deposit1.Amount-1, owner1, ids.Empty, ids.Empty),
					generate.Out(ctx.AVAXAssetID, 1, owner1, depositTxID1, ids.Empty),
				},
			}}},
			expectedErr: errExpiredDepositNotFullyUnlocked,
		},
		"Unlock more, than available": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyUnlockDeposit(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, deposit1UTXO},
					[]ids.ShortID{
						feeOwnerAddr, owner1Addr, // consumed (not expired deposit)
						owner1Addr, // produced unlocked
					}, nil)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil).Times(2)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{feeUTXO, deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generate.Out(ctx.AVAXAssetID, deposit1HalfUnlockableAmount+1, owner1, ids.Empty, ids.Empty),
					generate.Out(ctx.AVAXAssetID, deposit1.Amount-deposit1HalfUnlockableAmount-1, owner1, depositTxID1, ids.Empty),
				},
			}}},
			signers:     [][]*secp256k1.PrivateKey{{feeOwnerKey}, {owner1Key}},
			expectedErr: errUnlockedMoreThanAvailable,
		},
		"Burned tokens, while unlocking expired deposits": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generate.Out(ctx.AVAXAssetID, deposit1.Amount-1, owner1, ids.Empty, ids.Empty),
				},
			}}},
			expectedErr: errBurnedDepositUnlock,
		},
		"Only burn fee, nothing unlocked": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyUnlockDeposit(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, lessFeeUTXO, deposit1UTXO},
					[]ids.ShortID{
						feeOwnerAddr, feeOwnerAddr, owner1Addr, // consumed (not expired deposit)
						feeOwnerAddr, // produced unlocked
					}, nil)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil).Times(2)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{feeUTXO, lessFeeUTXO, deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generate.Out(ctx.AVAXAssetID, lessFeeUTXO.Out.(avax.Amounter).Amount(), feeOwner, ids.Empty, ids.Empty),
					generate.Out(ctx.AVAXAssetID, deposit1UTXO.Out.(avax.Amounter).Amount(), owner1, depositTxID1, ids.Empty),
				},
			}}},
			signers:     [][]*secp256k1.PrivateKey{{feeOwnerKey}, {feeOwnerKey}, {owner1Key}},
			expectedErr: errNoUnlock,
		},
		"OK: unlock full amount, expired deposit with unclaimed reward": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				expect.VerifyUnlockDeposit(t, s, utx.Ins,
					[]*avax.UTXO{deposit1WithRewardUTXO},
					[]ids.ShortID{
						owner1Addr, // produced unlocked
					}, nil)
				// state update: deposit1
				s.EXPECT().GetDeposit(depositWithRewardTxID1).Return(deposit1WithReward, nil).Times(2)
				s.EXPECT().GetDepositOffer(deposit1WithReward.DepositOfferID).Return(depositOfferWithReward, nil)
				s.EXPECT().GetClaimable(owner1ID).Return(&state.Claimable{Owner: &owner1}, nil)
				remainingReward := deposit1WithReward.TotalReward(depositOfferWithReward) - deposit1WithReward.ClaimedRewardAmount
				s.EXPECT().SetClaimable(owner1ID, &state.Claimable{
					Owner:                &owner1,
					ExpiredDepositReward: remainingReward,
				})
				s.EXPECT().RemoveDeposit(depositWithRewardTxID1, deposit1WithReward)
				// state update: ins/outs/utxos
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{deposit1WithRewardUTXO}),
				Outs: []*avax.TransferableOutput{
					generate.Out(ctx.AVAXAssetID, deposit1WithReward.Amount, owner1, ids.Empty, ids.Empty),
				},
			}}},
		},
		"OK: unlock available amount, deposit is still unlocking": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyUnlockDeposit(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, deposit1UTXO, deposit2UTXO},
					[]ids.ShortID{
						feeOwnerAddr, owner1Addr, owner1Addr, // consumed (not expired deposit)
						owner1Addr, // produced unlocked
					}, nil)
				// state update: deposit1
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil).Times(2)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				s.EXPECT().ModifyDeposit(depositTxID1, &deposit.Deposit{
					DepositOfferID:      deposit1.DepositOfferID,
					UnlockedAmount:      deposit1.UnlockedAmount + deposit1HalfUnlockableAmount,
					ClaimedRewardAmount: deposit1.ClaimedRewardAmount,
					Start:               deposit1.Start,
					Duration:            deposit1.Duration,
					Amount:              deposit1.Amount,
					RewardOwner:         deposit2.RewardOwner,
				})
				// state update: deposit2
				s.EXPECT().GetDeposit(depositTxID2).Return(deposit2, nil).Times(2)
				s.EXPECT().GetDepositOffer(deposit2.DepositOfferID).Return(depositOffer, nil)
				s.EXPECT().ModifyDeposit(depositTxID2, &deposit.Deposit{
					DepositOfferID:      deposit2.DepositOfferID,
					UnlockedAmount:      deposit2.UnlockedAmount + deposit2HalfUnlockableAmount,
					ClaimedRewardAmount: deposit2.ClaimedRewardAmount,
					Start:               deposit2.Start,
					Duration:            deposit2.Duration,
					Amount:              deposit2.Amount,
					RewardOwner:         deposit2.RewardOwner,
				})
				// state update: ins/outs/utxos
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generate.InsFromUTXOs(t, []*avax.UTXO{feeUTXO, deposit1UTXO, deposit2UTXO}),
				Outs: []*avax.TransferableOutput{
					generate.Out(ctx.AVAXAssetID, deposit1HalfUnlockableAmount+deposit2HalfUnlockableAmount, owner1, ids.Empty, ids.Empty),
					generate.Out(ctx.AVAXAssetID, deposit1.Amount-deposit1HalfUnlockableAmount, owner1, depositTxID1, ids.Empty),
					generate.Out(ctx.AVAXAssetID, deposit2.Amount-deposit2HalfUnlockableAmount, owner1, depositTxID2, ids.Empty),
				},
			}}},
			signers: [][]*secp256k1.PrivateKey{{feeOwnerKey}, {owner1Key}, {owner1Key}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			backend := newExecutorBackend(t, caminoGenesisConf, test.PhaseLast, nil)

			tt.utx.BlockchainID = backend.Ctx.ChainID
			tt.utx.NetworkID = backend.Ctx.NetworkID
			tx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(t, gomock.NewController(t), tt.utx, tx.ID()),
					Tx:      tx,
				},
			})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorClaimTx(t *testing.T) {
	ctx := test.Context(t)

	feeOwnerKey, feeOwnerAddr, feeOwner := generate.KeyAndOwner(t, test.Keys[0])
	depositRewardOwnerKey, _, depositRewardOwner := generate.KeyAndOwner(t, test.Keys[1])
	claimableOwnerKey1, _, claimableOwner1 := generate.KeyAndOwner(t, test.Keys[2])
	claimableOwnerKey2, _, claimableOwner2 := generate.KeyAndOwner(t, test.Keys[3])
	_, claimToOwnerAddr1, claimToOwner1 := generate.KeyAndOwner(t, test.Keys[4])
	_, claimToOwnerAddr2, claimToOwner2 := generate.KeyAndOwner(t, test.Keys[5])
	depositRewardMsigKeys, depositRewardMsigAlias, depositRewardMsigAliasOwner, depositRewardMsigOwner := generate.MsigAliasAndKeys([]*secp256k1.PrivateKey{test.Keys[6], test.Keys[7]}, 1, false)
	claimableMsigKeys, claimableMsigAlias, claimableMsigAliasOwner, claimableMsigOwner := generate.MsigAliasAndKeys([]*secp256k1.PrivateKey{test.Keys[8], test.Keys[9], test.Keys[10]}, 2, false)
	feeMsigKeys, feeMsigAlias, feeMsigAliasOwner, feeMsigOwner := generate.MsigAliasAndKeys([]*secp256k1.PrivateKey{test.Keys[11], test.Keys[12]}, 2, false)

	feeUTXO := generate.UTXO(ids.GenerateTestID(), ctx.AVAXAssetID, test.TxFee, feeOwner, ids.Empty, ids.Empty, true)
	msigFeeUTXO := generate.UTXO(ids.GenerateTestID(), ctx.AVAXAssetID, test.TxFee, *feeMsigOwner, ids.Empty, ids.Empty, true)

	depositOfferID := ids.GenerateTestID()
	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()
	claimableOwnerID1 := ids.GenerateTestID()
	claimableOwnerID2 := ids.GenerateTestID()
	timestamp := time.Now()

	claimableValidatorReward1 := &state.Claimable{
		Owner:           &claimableOwner1,
		ValidatorReward: 10,
	}
	claimableValidatorReward2 := &state.Claimable{
		Owner:           &claimableOwner2,
		ValidatorReward: 11,
	}
	claimable1 := &state.Claimable{
		Owner:                &claimableOwner1,
		ExpiredDepositReward: 20,
		ValidatorReward:      10,
	}
	claimable2 := &state.Claimable{
		Owner:                &claimableOwner2,
		ExpiredDepositReward: 21,
		ValidatorReward:      11,
	}
	claimableMsigOwned := &state.Claimable{
		Owner:                claimableMsigOwner,
		ExpiredDepositReward: 22,
		ValidatorReward:      12,
	}

	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	baseTxWithFeeInput := func(outs []*avax.TransferableOutput) *txs.BaseTx {
		return &txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, feeUTXO, []uint32{0}, false)},
			Outs:         outs,
		}}
	}

	tests := map[string]struct {
		state       func(*testing.T, *gomock.Controller, *txs.ClaimTx, ids.ID) *state.MockDiff
		utx         *txs.ClaimTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Deposit not found": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				// deposit
				s.EXPECT().GetDeposit(depositTxID1).Return(nil, database.ErrNotFound)
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput(nil), // doesn't matter
				Claimables: []txs.ClaimAmount{{
					ID:        depositTxID1,
					Amount:    1,
					Type:      txs.ClaimTypeActiveDepositReward,
					OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{depositRewardOwnerKey},
			},
			expectedErr: errDepositNotFound,
		},
		"Bad deposit credential": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				// deposit
				s.EXPECT().GetDeposit(depositTxID1).
					Return(&deposit.Deposit{RewardOwner: &depositRewardOwner}, nil)
				expect.VerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput(nil), // doesn't matter
				Claimables: []txs.ClaimAmount{{
					ID:        depositTxID1,
					Amount:    1,
					Type:      txs.ClaimTypeActiveDepositReward,
					OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{feeOwnerKey},
			},
			expectedErr: errClaimableCredentialMismatch,
		},
		"Bad claimable credential": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimable1, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput(nil), // doesn't matter
				Claimables: []txs.ClaimAmount{{
					ID:        claimableOwnerID1,
					Amount:    1,
					Type:      txs.ClaimTypeAllTreasury,
					OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{feeOwnerKey},
			},
			expectedErr: errClaimableCredentialMismatch,
		},
		// no test case for expired deposits - expected to be alike
		"Claimed more than available (validator rewards)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)

				// claimable 1
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimableValidatorReward1, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, &state.Claimable{
					Owner:           claimableValidatorReward1.Owner,
					ValidatorReward: claimableValidatorReward1.ValidatorReward - utx.Claimables[0].Amount,
				})

				// claimable 2
				s.EXPECT().GetClaimable(claimableOwnerID2).Return(claimableValidatorReward2, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner2.Addrs, nil)
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput(nil), // doesn't matter
				Claimables: []txs.ClaimAmount{
					{
						ID:        claimableOwnerID1,
						Amount:    claimableValidatorReward1.ValidatorReward - 1,
						Type:      txs.ClaimTypeValidatorReward,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
					{
						ID:        claimableOwnerID2,
						Amount:    claimableValidatorReward2.ValidatorReward + 1,
						Type:      txs.ClaimTypeValidatorReward,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{claimableOwnerKey1},
				{claimableOwnerKey2},
			},
			expectedErr: errWrongClaimedAmount,
		},
		"Claimed more than available (all treasury)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)

				// claimable 1
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimable1, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, &state.Claimable{
					Owner:                claimable1.Owner,
					ExpiredDepositReward: 1,
				})

				// claimable 2
				s.EXPECT().GetClaimable(claimableOwnerID2).Return(claimable2, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner2.Addrs, nil)
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput(nil), // doesn't matter
				Claimables: []txs.ClaimAmount{
					{
						ID:        claimableOwnerID1,
						Amount:    claimable1.ValidatorReward - 1 + claimable1.ExpiredDepositReward,
						Type:      txs.ClaimTypeAllTreasury,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
					{
						ID:        claimableOwnerID2,
						Amount:    claimable2.ValidatorReward + 1 + claimable2.ExpiredDepositReward,
						Type:      txs.ClaimTypeAllTreasury,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{claimableOwnerKey1},
				{claimableOwnerKey2},
			},
			expectedErr: errWrongClaimedAmount,
		},
		"Claimed more than available (active deposit)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)

				// deposit1
				deposit1 := &deposit.Deposit{
					DepositOfferID: depositOfferID,
					Start:          uint64(timestamp.Unix()) - 365*24*60*60/2, // 0.5 year ago
					Duration:       365 * 24 * 60 * 60,                        // 1 year
					Amount:         10,
					RewardOwner:    &depositRewardOwner,
				}
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				expect.VerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
				s.EXPECT().GetDepositOffer(depositOfferID).Return(&deposit.Offer{
					InterestRateNominator: 1_000_000, // 100%
				}, nil)
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput(nil), // doesn't matter
				Claimables: []txs.ClaimAmount{{
					ID:        depositTxID1,
					Amount:    6, // 5 is expected claimable reward amount
					Type:      txs.ClaimTypeActiveDepositReward,
					OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{depositRewardOwnerKey},
			},
			expectedErr: errWrongClaimedAmount,
		},
		"OK, 2 deposits and claimable": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1, claimToOwnerAddr1, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)

				// deposit1
				expect.VerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
				deposit1 := &deposit.Deposit{
					DepositOfferID: depositOfferID,
					Start:          uint64(timestamp.Unix()) - 365*24*60*60/2, // 0.5 year ago
					Duration:       365 * 24 * 60 * 60,                        // 1 year
					Amount:         10,
					RewardOwner:    &depositRewardOwner,
				}
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(depositOfferID).Return(&deposit.Offer{
					InterestRateNominator: 1_000_000, // 100%
				}, nil)
				s.EXPECT().ModifyDeposit(depositTxID1, &deposit.Deposit{
					DepositOfferID:      deposit1.DepositOfferID,
					UnlockedAmount:      deposit1.UnlockedAmount,
					ClaimedRewardAmount: deposit1.ClaimedRewardAmount + utx.Claimables[0].Amount,
					Start:               deposit1.Start,
					Duration:            deposit1.Duration,
					Amount:              deposit1.Amount,
					RewardOwner:         deposit1.RewardOwner,
				})

				// deposit2
				expect.VerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
				deposit2 := &deposit.Deposit{
					DepositOfferID: depositOfferID,
					Start:          uint64(timestamp.Unix()) - 365*24*60*60/2, // 0.5 year ago
					Duration:       365 * 24 * 60 * 60,                        // 1 year
					Amount:         10,
					RewardOwner:    &depositRewardOwner,
				}
				s.EXPECT().GetDeposit(depositTxID2).Return(deposit2, nil)
				s.EXPECT().GetDepositOffer(depositOfferID).Return(&deposit.Offer{
					InterestRateNominator: 1_000_000, // 100%
				}, nil)
				s.EXPECT().ModifyDeposit(depositTxID2, &deposit.Deposit{
					DepositOfferID:      deposit2.DepositOfferID,
					UnlockedAmount:      deposit2.UnlockedAmount,
					ClaimedRewardAmount: deposit2.ClaimedRewardAmount + utx.Claimables[1].Amount,
					Start:               deposit2.Start,
					Duration:            deposit2.Duration,
					Amount:              deposit2.Amount,
					RewardOwner:         deposit1.RewardOwner,
				})

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimable1, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, nil)
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          5, // expected claimable reward amount
							OutputOwners: claimToOwner1,
						},
					},
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          5, // expected claimable reward amount
							OutputOwners: claimToOwner1,
						},
					},
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          claimable1.ValidatorReward + claimable1.ExpiredDepositReward,
							OutputOwners: claimToOwner1,
						},
					},
				}),
				Claimables: []txs.ClaimAmount{
					{
						ID:        depositTxID1,
						Amount:    5, // expected claimable reward amount
						Type:      txs.ClaimTypeActiveDepositReward,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
					{
						ID:        depositTxID2,
						Amount:    5, // expected claimable reward amount
						Type:      txs.ClaimTypeActiveDepositReward,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
					{
						ID:        claimableOwnerID1,
						Amount:    claimable1.ValidatorReward + claimable1.ExpiredDepositReward,
						Type:      txs.ClaimTypeAllTreasury,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{depositRewardOwnerKey},
				{depositRewardOwnerKey},
				{claimableOwnerKey1},
			},
		},
		"OK, 2 claimable (splitted outs)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{
						feeOwnerAddr, claimToOwnerAddr1, claimToOwnerAddr2,
						claimToOwnerAddr1, claimToOwnerAddr1,
					}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)

				// claimable1
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimableValidatorReward1, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, nil)

				// claimable2
				s.EXPECT().GetClaimable(claimableOwnerID2).Return(claimableValidatorReward2, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner2.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID2, &state.Claimable{
					Owner:           claimableValidatorReward2.Owner,
					ValidatorReward: claimableValidatorReward2.ValidatorReward - utx.Claimables[1].Amount,
				})
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          7, // part of claimableValidatorReward1.ValidatorReward
							OutputOwners: claimToOwner1,
						},
					},
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          claimableValidatorReward1.ValidatorReward - 7,
							OutputOwners: claimToOwner2,
						},
					},
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          2, // part of claimableValidatorReward2.ValidatorReward
							OutputOwners: claimToOwner1,
						},
					},
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          claimableValidatorReward2.ValidatorReward/2 - 2,
							OutputOwners: claimToOwner1,
						},
					},
				}),
				Claimables: []txs.ClaimAmount{
					{
						ID:        claimableOwnerID1,
						Amount:    claimableValidatorReward1.ValidatorReward,
						Type:      txs.ClaimTypeAllTreasury,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
					{
						ID:        claimableOwnerID2,
						Amount:    claimableValidatorReward2.ValidatorReward / 2,
						Type:      txs.ClaimTypeAllTreasury,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{claimableOwnerKey1},
				{claimableOwnerKey2},
			},
		},
		"OK, 2 claimable (compacted out)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)

				// claimable1
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimableValidatorReward1, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, nil)

				// claimable2
				s.EXPECT().GetClaimable(claimableOwnerID2).Return(claimableValidatorReward2, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner2.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID2, &state.Claimable{
					Owner:           claimableValidatorReward2.Owner,
					ValidatorReward: claimableValidatorReward2.ValidatorReward - utx.Claimables[1].Amount,
				})
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          claimableValidatorReward1.ValidatorReward + claimableValidatorReward2.ValidatorReward/2,
							OutputOwners: claimToOwner1,
						},
					},
				}),
				Claimables: []txs.ClaimAmount{
					{
						ID:        claimableOwnerID1,
						Amount:    claimableValidatorReward1.ValidatorReward,
						Type:      txs.ClaimTypeAllTreasury,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
					{
						ID:        claimableOwnerID2,
						Amount:    claimableValidatorReward2.ValidatorReward / 2,
						Type:      txs.ClaimTypeAllTreasury,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{claimableOwnerKey1},
				{claimableOwnerKey2},
			},
		},
		"OK, active deposit with non-zero already claimed reward and no rewards period": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)

				// deposit
				expect.VerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
				deposit1 := &deposit.Deposit{
					DepositOfferID:      depositOfferID,
					Start:               uint64(timestamp.Unix()) - 365*24*60*60/12*6, // 6 month
					Duration:            365 * 24 * 60 * 60 / 12 * 14,                 // 14 month
					Amount:              10,
					ClaimedRewardAmount: 1,
					RewardOwner:         &depositRewardOwner,
				}
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(depositOfferID).Return(&deposit.Offer{
					NoRewardsPeriodDuration: 365 * 24 * 60 * 60 / 12 * 2, // 2 month
					InterestRateNominator:   1_000_000,                   // 100%
				}, nil)
				s.EXPECT().ModifyDeposit(depositTxID1, &deposit.Deposit{
					DepositOfferID:      deposit1.DepositOfferID,
					UnlockedAmount:      deposit1.UnlockedAmount,
					ClaimedRewardAmount: deposit1.ClaimedRewardAmount + utx.Claimables[0].Amount,
					Start:               deposit1.Start,
					Duration:            deposit1.Duration,
					Amount:              deposit1.Amount,
					RewardOwner:         deposit1.RewardOwner,
				})
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          4, // expected claimable reward amount
						OutputOwners: claimToOwner1,
					},
				}}),
				Claimables: []txs.ClaimAmount{{
					ID:        depositTxID1,
					Amount:    4, // expected claimable reward amount: 10 * (6m / (14m - 2m)) - 1 = 10 * 0.5 - 1 = 5 - 1 = 4
					Type:      txs.ClaimTypeActiveDepositReward,
					OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{depositRewardOwnerKey},
			},
		},
		"OK, partial claim": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)

				// deposit1
				expect.VerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
				deposit1 := &deposit.Deposit{
					DepositOfferID: depositOfferID,
					Start:          uint64(timestamp.Unix()) - 365*24*60*60/2, // 0.5 year ago
					Duration:       365 * 24 * 60 * 60,                        // 1 year
					Amount:         10,
					RewardOwner:    &depositRewardOwner,
				}
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(depositOfferID).Return(&deposit.Offer{
					InterestRateNominator: 1_000_000, // 100%
				}, nil)
				s.EXPECT().ModifyDeposit(depositTxID1, &deposit.Deposit{
					DepositOfferID:      deposit1.DepositOfferID,
					UnlockedAmount:      deposit1.UnlockedAmount,
					ClaimedRewardAmount: deposit1.ClaimedRewardAmount + utx.Claimables[1].Amount,
					Start:               deposit1.Start,
					Duration:            deposit1.Duration,
					Amount:              deposit1.Amount,
					RewardOwner:         deposit1.RewardOwner,
				})

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimable1, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, &state.Claimable{
					Owner:                claimable1.Owner,
					ExpiredDepositReward: claimable1.ExpiredDepositReward / 2,
				})
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          claimable1.ValidatorReward + claimable1.ExpiredDepositReward/2,
							OutputOwners: claimToOwner1,
						},
					},
					{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          2, // 5 is expected available reward amount
							OutputOwners: claimToOwner1,
						},
					},
				}),
				Claimables: []txs.ClaimAmount{
					{
						ID:        claimableOwnerID1,
						Amount:    claimable1.ValidatorReward + claimable1.ExpiredDepositReward/2,
						Type:      txs.ClaimTypeAllTreasury,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
					{
						ID:        depositTxID1,
						Amount:    2, // 5 is expected available reward amount
						Type:      txs.ClaimTypeActiveDepositReward,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{claimableOwnerKey1},
				{depositRewardOwnerKey},
			},
		},
		"OK, claim (expired deposit rewards)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimable1, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, &state.Claimable{
					Owner:           claimable1.Owner,
					ValidatorReward: claimable1.ValidatorReward,
				})
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          claimable1.ExpiredDepositReward,
						OutputOwners: claimToOwner1,
					},
				}}),
				Claimables: []txs.ClaimAmount{{
					ID:        claimableOwnerID1,
					Amount:    claimable1.ExpiredDepositReward,
					Type:      txs.ClaimTypeExpiredDepositReward,
					OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{claimableOwnerKey1},
			},
		},
		"OK, claim (validator rewards)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimable1, nil)
				expect.VerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, &state.Claimable{
					Owner:                claimable1.Owner,
					ExpiredDepositReward: claimable1.ExpiredDepositReward,
				})
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: *baseTxWithFeeInput([]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          claimable1.ValidatorReward,
						OutputOwners: claimToOwner1,
					},
				}}),
				Claimables: []txs.ClaimAmount{{
					ID:        claimableOwnerID1,
					Amount:    claimable1.ValidatorReward,
					Type:      txs.ClaimTypeValidatorReward,
					OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{claimableOwnerKey1},
			},
		},
		"OK, msig fee, claimable and deposit": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee+
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{msigFeeUTXO},
					[]ids.ShortID{
						feeMsigAlias.ID,
						feeMsigAliasOwner.Addrs[0],
						feeMsigAliasOwner.Addrs[1],
						claimToOwnerAddr1,
					},
					[]*multisig.AliasWithNonce{feeMsigAlias})
				s.EXPECT().GetTimestamp().Return(timestamp)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)

				// deposit1
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{
					depositRewardMsigAlias.ID,
					depositRewardMsigAliasOwner.Addrs[0],
					depositRewardMsigAliasOwner.Addrs[1],
				}, []*multisig.AliasWithNonce{depositRewardMsigAlias})
				deposit1 := &deposit.Deposit{
					DepositOfferID: depositOfferID,
					Start:          uint64(timestamp.Unix()) - 365*24*60*60/2, // 0.5 year ago
					Duration:       365 * 24 * 60 * 60,                        // 1 year
					Amount:         10,
					RewardOwner:    depositRewardMsigOwner,
				}
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(depositOfferID).Return(&deposit.Offer{
					InterestRateNominator: 1_000_000, // 100%
				}, nil)
				s.EXPECT().ModifyDeposit(depositTxID1, &deposit.Deposit{
					DepositOfferID:      deposit1.DepositOfferID,
					UnlockedAmount:      deposit1.UnlockedAmount,
					ClaimedRewardAmount: deposit1.ClaimedRewardAmount + utx.Claimables[0].Amount,
					Start:               deposit1.Start,
					Duration:            deposit1.Duration,
					Amount:              deposit1.Amount,
					RewardOwner:         deposit1.RewardOwner,
				})

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimableMsigOwned, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{
					claimableMsigAlias.ID,
					claimableMsigAliasOwner.Addrs[0],
					claimableMsigAliasOwner.Addrs[1],
					claimableMsigAliasOwner.Addrs[2],
				}, []*multisig.AliasWithNonce{claimableMsigAlias})
				s.EXPECT().SetClaimable(claimableOwnerID1, nil)
				return s
			},
			utx: &txs.ClaimTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, msigFeeUTXO, []uint32{0, 1}, false)},
					Outs: []*avax.TransferableOutput{{
						Asset: avax.Asset{ID: ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          claimableMsigOwned.ExpiredDepositReward,
							OutputOwners: claimToOwner1,
						},
					}},
				}},
				Claimables: []txs.ClaimAmount{
					{
						ID:        depositTxID1,
						Amount:    5,
						Type:      txs.ClaimTypeActiveDepositReward,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
					},
					{
						ID:        claimableOwnerID1,
						Amount:    claimableMsigOwned.ValidatorReward + claimableMsigOwned.ExpiredDepositReward,
						Type:      txs.ClaimTypeAllTreasury,
						OwnerAuth: &secp256k1fx.Input{SigIndices: []uint32{0, 2}},
					},
				},
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeMsigKeys[0], feeMsigKeys[1]},
				{depositRewardMsigKeys[0]},
				{claimableMsigKeys[0], claimableMsigKeys[2]},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			backend := newExecutorBackend(t, caminoGenesisConf, test.PhaseLast, nil)

			// ensuring that ins and outs from test case are sorted, signing tx

			avax.SortTransferableInputsWithSigners(tt.utx.Ins, tt.signers)
			avax.SortTransferableOutputs(tt.utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(err)

			// testing

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(t, gomock.NewController(t), tt.utx, tx.ID()),
					Tx:      tx,
				},
			})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorRegisterNodeTx(t *testing.T) {
	ctx := test.Context(t)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generate.KeyAndOwner(t, test.Keys[0])
	consortiumMemberKey, consortiumMemberAddr := test.Keys[1], test.Keys[1].Address()
	consortiumMemberMsigKeys, consortiumMemberMsigAlias, consortiumMemberMsigAliasOwner, _ := generate.MsigAliasAndKeys([]*secp256k1.PrivateKey{test.Keys[2], test.Keys[3], test.Keys[4]}, 2, false)
	nodeKey1, nodeAddr1 := test.Keys[5], test.Keys[5].Address()
	nodeKey2, nodeAddr2 := test.Keys[6], test.Keys[6].Address()
	nodeID1 := ids.NodeID(nodeAddr1)
	nodeID2 := ids.NodeID(nodeAddr2)

	feeUTXO := generate.UTXO(ids.GenerateTestID(), ctx.AVAXAssetID, test.TxFee, feeOwner, ids.Empty, ids.Empty, true)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, feeUTXO, []uint32{0}, false)},
	}}

	tests := map[string]struct {
		state       func(*testing.T, *gomock.Controller, *txs.RegisterNodeTx) *state.MockDiff
		utx         func() *txs.RegisterNodeTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Not consortium member": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.RegisterNodeTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateEmpty, nil)
				return s
			},
			utx: func() *txs.RegisterNodeTx {
				return &txs.RegisterNodeTx{
					BaseTx:           baseTx,
					OldNodeID:        ids.EmptyNodeID,
					NewNodeID:        nodeID1,
					NodeOwnerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					NodeOwnerAddress: consortiumMemberAddr,
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {nodeKey1}, {consortiumMemberKey},
			},
			expectedErr: errNotConsortiumMember,
		},
		"Consortium member has already registered node": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.RegisterNodeTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortium, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(nodeAddr2, nil)
				return s
			},
			utx: func() *txs.RegisterNodeTx {
				return &txs.RegisterNodeTx{
					BaseTx:           baseTx,
					OldNodeID:        ids.EmptyNodeID,
					NewNodeID:        nodeID1,
					NodeOwnerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					NodeOwnerAddress: consortiumMemberAddr,
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {nodeKey1}, {consortiumMemberKey},
			},
			expectedErr: errConsortiumMemberHasNode,
		},
		"Old node is in current validator's set": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.RegisterNodeTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortium, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(nodeAddr1, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, utx.OldNodeID).Return(nil, nil) // no error
				return s
			},
			utx: func() *txs.RegisterNodeTx {
				return &txs.RegisterNodeTx{
					BaseTx:           baseTx,
					OldNodeID:        nodeID1,
					NewNodeID:        nodeID2,
					NodeOwnerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					NodeOwnerAddress: consortiumMemberAddr,
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{nodeKey2},
				{consortiumMemberKey},
			},
			expectedErr: errValidatorExists,
		},
		"Old node is in pending validator's set": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.RegisterNodeTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortium, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(nodeAddr1, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, utx.OldNodeID).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, utx.OldNodeID).Return(nil, nil) // no error
				return s
			},
			utx: func() *txs.RegisterNodeTx {
				return &txs.RegisterNodeTx{
					BaseTx:           baseTx,
					OldNodeID:        nodeID1,
					NewNodeID:        nodeID2,
					NodeOwnerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					NodeOwnerAddress: consortiumMemberAddr,
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{nodeKey2},
				{consortiumMemberKey},
			},
			expectedErr: errValidatorExists,
		},
		"Old node is in deferred validator's set": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.RegisterNodeTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortium, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(nodeAddr1, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, utx.OldNodeID).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, utx.OldNodeID).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetDeferredValidator(constants.PrimaryNetworkID, utx.OldNodeID).Return(nil, nil) // no error
				return s
			},
			utx: func() *txs.RegisterNodeTx {
				return &txs.RegisterNodeTx{
					BaseTx:           baseTx,
					OldNodeID:        nodeID1,
					NewNodeID:        nodeID2,
					NodeOwnerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					NodeOwnerAddress: consortiumMemberAddr,
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{nodeKey2},
				{consortiumMemberKey},
			},
			expectedErr: errValidatorExists,
		},
		"OK: change registered node": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.RegisterNodeTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortium, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(nodeAddr1, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, utx.OldNodeID).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, utx.OldNodeID).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetDeferredValidator(constants.PrimaryNetworkID, utx.OldNodeID).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().SetShortIDLink(ids.ShortID(utx.OldNodeID), state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().SetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().SetShortIDLink(
					ids.ShortID(utx.NewNodeID),
					state.ShortLinkKeyRegisterNode,
					&utx.NodeOwnerAddress,
				)
				link := ids.ShortID(utx.NewNodeID)
				s.EXPECT().SetShortIDLink(
					utx.NodeOwnerAddress,
					state.ShortLinkKeyRegisterNode,
					&link,
				)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				return s
			},
			utx: func() *txs.RegisterNodeTx {
				return &txs.RegisterNodeTx{
					BaseTx:           baseTx,
					OldNodeID:        nodeID1,
					NewNodeID:        nodeID2,
					NodeOwnerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					NodeOwnerAddress: consortiumMemberAddr,
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{nodeKey2},
				{consortiumMemberKey},
			},
		},
		"OK: consortium member is msig alias": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.RegisterNodeTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortium, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(ids.ShortEmpty, database.ErrNotFound)
				s.EXPECT().GetShortIDLink(ids.ShortID(utx.NewNodeID), state.ShortLinkKeyRegisterNode).
					Return(ids.ShortEmpty, database.ErrNotFound)
				expect.VerifyMultisigPermission(t, s,
					[]ids.ShortID{
						utx.NodeOwnerAddress,
						consortiumMemberMsigAliasOwner.Addrs[0],
						consortiumMemberMsigAliasOwner.Addrs[1],
						consortiumMemberMsigAliasOwner.Addrs[2],
					},
					[]*multisig.AliasWithNonce{consortiumMemberMsigAlias})
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().SetShortIDLink(
					ids.ShortID(utx.NewNodeID),
					state.ShortLinkKeyRegisterNode,
					&utx.NodeOwnerAddress,
				)
				link := ids.ShortID(utx.NewNodeID)
				s.EXPECT().SetShortIDLink(
					utx.NodeOwnerAddress,
					state.ShortLinkKeyRegisterNode,
					&link,
				)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				return s
			},
			utx: func() *txs.RegisterNodeTx {
				return &txs.RegisterNodeTx{
					BaseTx:           baseTx,
					OldNodeID:        ids.EmptyNodeID,
					NewNodeID:        nodeID1,
					NodeOwnerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0, 1}},
					NodeOwnerAddress: consortiumMemberMsigAlias.ID,
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{nodeKey1},
				{consortiumMemberMsigKeys[0], consortiumMemberMsigKeys[1]},
			},
		},
		"OK": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.RegisterNodeTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortium, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(ids.ShortEmpty, database.ErrNotFound)
				s.EXPECT().GetShortIDLink(ids.ShortID(utx.NewNodeID), state.ShortLinkKeyRegisterNode).
					Return(ids.ShortEmpty, database.ErrNotFound)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().SetShortIDLink(
					ids.ShortID(utx.NewNodeID),
					state.ShortLinkKeyRegisterNode,
					&utx.NodeOwnerAddress,
				)
				link := ids.ShortID(utx.NewNodeID)
				s.EXPECT().SetShortIDLink(
					utx.NodeOwnerAddress,
					state.ShortLinkKeyRegisterNode,
					&link,
				)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				return s
			},
			utx: func() *txs.RegisterNodeTx {
				return &txs.RegisterNodeTx{
					BaseTx:           baseTx,
					OldNodeID:        ids.EmptyNodeID,
					NewNodeID:        nodeID1,
					NodeOwnerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
					NodeOwnerAddress: consortiumMemberAddr,
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{nodeKey1},
				{consortiumMemberKey},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			backend := newExecutorBackend(t, caminoGenesisConf, test.PhaseLast, nil)

			utx := tt.utx()
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(t, gomock.NewController(t), utx),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorRewardsImportTx(t *testing.T) {
	ctx := test.Context(t)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	caminoStateConf := &state.CaminoConfig{
		VerifyNodeSignature: caminoGenesisConf.VerifyNodeSignature,
		LockModeBondDeposit: caminoGenesisConf.LockModeBondDeposit,
	}

	blockTime := time.Unix(1000, 0)

	shmWithUTXOs := func(t *testing.T, c *gomock.Controller, utxos []*avax.TimedUTXO) *atomic.MockSharedMemory {
		shm := atomic.NewMockSharedMemory(c)
		utxoIDs := make([][]byte, len(utxos))
		var utxosBytes [][]byte
		if len(utxos) != 0 {
			utxosBytes = make([][]byte, len(utxos))
		}
		for i, utxo := range utxos {
			var toMarshal interface{} = utxo
			if utxo.Timestamp == 0 {
				toMarshal = utxo.UTXO
			}
			utxoID := utxo.InputID()
			utxoIDs[i] = utxoID[:]
			utxoBytes, err := txs.Codec.Marshal(txs.Version, toMarshal)
			require.NoError(t, err)
			utxosBytes[i] = utxoBytes
		}
		shm.EXPECT().Indexed(ctx.CChainID, treasury.AddrTraitsBytes,
			ids.ShortEmpty[:], ids.Empty[:], maxPageSize).Return(utxosBytes, nil, nil, nil)
		return shm
	}

	tests := map[string]struct {
		state                  func(*gomock.Controller, *txs.RewardsImportTx, ids.ID) *state.MockDiff
		sharedMemory           func(*testing.T, *gomock.Controller, []*avax.TimedUTXO) *atomic.MockSharedMemory
		utx                    func([]*avax.TimedUTXO) *txs.RewardsImportTx
		signers                [][]*secp256k1.PrivateKey
		utxos                  []*avax.TimedUTXO // sorted by txID
		expectedAtomicInputs   func([]*avax.TimedUTXO) set.Set[ids.ID]
		expectedAtomicRequests func([]*avax.TimedUTXO) map[ids.ID]*atomic.Requests
		expectedErr            error
	}{
		"Imported inputs don't contain all reward utxos that are ready to be imported": {
			state: func(c *gomock.Controller, utx *txs.RewardsImportTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(blockTime)
				return s
			},
			sharedMemory: shmWithUTXOs,
			utx: func(utxos []*avax.TimedUTXO) *txs.RewardsImportTx {
				return &txs.RewardsImportTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.InFromUTXO(t, &utxos[0].UTXO, []uint32{0}, false),
						generate.InFromUTXO(t, &utxos[1].UTXO, []uint32{0}, false),
					},
				}}}
			},
			utxos: []*avax.TimedUTXO{
				{
					UTXO:      *generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty, true),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
					UTXO:      *generate.UTXO(ids.ID{2}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty, true),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
					UTXO:      *generate.UTXO(ids.ID{3}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty, true),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
			},
			expectedErr: errInputsUTXOSMismatch,
		},
		"Imported input doesn't match reward utxo": {
			state: func(c *gomock.Controller, utx *txs.RewardsImportTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(blockTime)
				return s
			},
			sharedMemory: shmWithUTXOs,
			utx: func(utxos []*avax.TimedUTXO) *txs.RewardsImportTx {
				return &txs.RewardsImportTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.In(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					},
				}}}
			},
			utxos: []*avax.TimedUTXO{{
				UTXO:      *generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty, true),
				Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
			}},
			expectedErr: errImportedUTXOMismatch,
		},
		"Input & utxo amount mismatch": {
			state: func(c *gomock.Controller, utx *txs.RewardsImportTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(blockTime)
				return s
			},
			sharedMemory: shmWithUTXOs,
			utx: func(utxos []*avax.TimedUTXO) *txs.RewardsImportTx {
				return &txs.RewardsImportTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{{
						UTXOID: utxos[0].UTXOID,
						Asset:  avax.Asset{ID: ctx.AVAXAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   2,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
				}}}
			},
			utxos: []*avax.TimedUTXO{{
				UTXO:      *generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty, true),
				Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
			}},
			expectedErr: errInputAmountMismatch,
		},
		"OK": {
			state: func(c *gomock.Controller, utx *txs.RewardsImportTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(blockTime)

				rewardOwner1 := &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{{1}},
				}
				rewardOwner2 := &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{{2}},
				}
				rewardOwner4 := &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{{4}},
				}

				staker1 := &state.Staker{TxID: ids.ID{0, 1}, SubnetID: constants.PrimaryNetworkID}
				staker2 := &state.Staker{TxID: ids.ID{0, 2}, SubnetID: constants.PrimaryNetworkID}
				staker3 := &state.Staker{TxID: ids.ID{0, 3}, SubnetID: ids.ID{0, 0, 1}}
				staker4 := &state.Staker{TxID: ids.ID{0, 4}, SubnetID: constants.PrimaryNetworkID}
				staker5 := &state.Staker{TxID: ids.ID{0, 5}, SubnetID: constants.PrimaryNetworkID}

				currentStakerIterator := state.NewMockStakerIterator(c)
				currentStakerIterator.EXPECT().Next().Return(true).Times(5)
				currentStakerIterator.EXPECT().Value().Return(staker1)
				currentStakerIterator.EXPECT().Value().Return(staker2)
				currentStakerIterator.EXPECT().Value().Return(staker3)
				currentStakerIterator.EXPECT().Value().Return(staker4)
				currentStakerIterator.EXPECT().Value().Return(staker5)
				currentStakerIterator.EXPECT().Next().Return(false)
				currentStakerIterator.EXPECT().Release()

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIterator, nil)
				s.EXPECT().GetTx(staker1.TxID).Return(&txs.Tx{Unsigned: &txs.CaminoAddValidatorTx{AddValidatorTx: txs.AddValidatorTx{
					RewardsOwner: rewardOwner1,
				}}}, status.Committed, nil)
				s.EXPECT().GetTx(staker2.TxID).Return(&txs.Tx{Unsigned: &txs.CaminoAddValidatorTx{AddValidatorTx: txs.AddValidatorTx{
					RewardsOwner: rewardOwner2,
				}}}, status.Committed, nil)
				s.EXPECT().GetTx(staker4.TxID).Return(&txs.Tx{Unsigned: &txs.CaminoAddValidatorTx{AddValidatorTx: txs.AddValidatorTx{
					RewardsOwner: rewardOwner4,
				}}}, status.Committed, nil)
				s.EXPECT().GetTx(staker5.TxID).Return(&txs.Tx{Unsigned: &txs.CaminoAddValidatorTx{AddValidatorTx: txs.AddValidatorTx{
					RewardsOwner: rewardOwner1, // same as staker1
				}}}, status.Committed, nil)
				s.EXPECT().GetNotDistributedValidatorReward().Return(uint64(1), nil) // old
				s.EXPECT().SetNotDistributedValidatorReward(uint64(2))               // new
				rewardOwnerID1, err := txs.GetOwnerID(rewardOwner1)
				require.NoError(t, err)
				rewardOwnerID2, err := txs.GetOwnerID(rewardOwner2)
				require.NoError(t, err)
				rewardOwnerID4, err := txs.GetOwnerID(rewardOwner4)
				require.NoError(t, err)

				s.EXPECT().GetClaimable(rewardOwnerID1).Return(&state.Claimable{
					Owner:                rewardOwner1,
					ValidatorReward:      10,
					ExpiredDepositReward: 100,
				}, nil)
				s.EXPECT().GetClaimable(rewardOwnerID2).Return(&state.Claimable{
					Owner:                rewardOwner2,
					ValidatorReward:      20,
					ExpiredDepositReward: 200,
				}, nil)
				s.EXPECT().GetClaimable(rewardOwnerID4).Return(nil, database.ErrNotFound)

				s.EXPECT().SetClaimable(rewardOwnerID1, &state.Claimable{
					Owner:                rewardOwner1,
					ValidatorReward:      12,
					ExpiredDepositReward: 100,
				})
				s.EXPECT().SetClaimable(rewardOwnerID2, &state.Claimable{
					Owner:                rewardOwner2,
					ValidatorReward:      21,
					ExpiredDepositReward: 200,
				})
				s.EXPECT().SetClaimable(rewardOwnerID4, &state.Claimable{
					Owner:           rewardOwner4,
					ValidatorReward: 1,
				})

				return s
			},
			sharedMemory: shmWithUTXOs,
			utx: func(utxos []*avax.TimedUTXO) *txs.RewardsImportTx {
				return &txs.RewardsImportTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    ctx.NetworkID,
					BlockchainID: ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generate.InFromUTXO(t, &utxos[0].UTXO, []uint32{0}, false),
						generate.InFromUTXO(t, &utxos[2].UTXO, []uint32{0}, false),
					},
				}}}
			},
			utxos: []*avax.TimedUTXO{
				{ // timed utxo, old enough
					UTXO:      *generate.UTXO(ids.ID{1}, ctx.AVAXAssetID, 3, *treasury.Owner, ids.Empty, ids.Empty, true),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{ // not timed utxo
					UTXO: *generate.UTXO(ids.ID{2}, ctx.AVAXAssetID, 5, *treasury.Owner, ids.Empty, ids.Empty, true),
				},
				{ // timed utxo, old enough
					UTXO:      *generate.UTXO(ids.ID{3}, ctx.AVAXAssetID, 2, *treasury.Owner, ids.Empty, ids.Empty, true),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{ // timed utxo, not old enough
					UTXO:      *generate.UTXO(ids.ID{4}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty, true),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound + 1,
				},
			},
			expectedAtomicInputs: func(utxos []*avax.TimedUTXO) set.Set[ids.ID] {
				return set.Set[ids.ID]{
					utxos[0].InputID(): struct{}{},
					utxos[2].InputID(): struct{}{},
				}
			},
			expectedAtomicRequests: func(utxos []*avax.TimedUTXO) map[ids.ID]*atomic.Requests {
				utxoID0 := utxos[0].InputID()
				utxoID2 := utxos[2].InputID()
				return map[ids.ID]*atomic.Requests{ctx.CChainID: {
					RemoveRequests: [][]byte{utxoID0[:], utxoID2[:]},
				}}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			backend := newExecutorBackend(t, caminoGenesisConf, test.PhaseLast, tt.sharedMemory(t, ctrl, tt.utxos))

			utx := tt.utx(tt.utxos)
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			// utx.ImportedInputs must be already sorted cause of utxos sort
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(err)

			e := &CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(ctrl, utx, tx.ID()),
					Tx:      tx,
				},
			}

			err = tx.Unsigned.Visit(e)
			require.ErrorIs(err, tt.expectedErr)

			if tt.expectedAtomicInputs != nil {
				require.Equal(tt.expectedAtomicInputs(tt.utxos), e.Inputs)
			} else {
				require.Nil(e.Inputs)
			}

			if tt.expectedAtomicRequests != nil {
				require.Equal(tt.expectedAtomicRequests(tt.utxos), e.AtomicRequests)
			} else {
				require.Nil(e.AtomicRequests)
			}
		})
	}
}

func TestCaminoStandardTxExecutorExportTxMultisig(t *testing.T) {
	ctx := test.Context(t)
	fakeMSigAlias := test.FundedKeys[0].Address()
	sourceKey := test.FundedKeys[1]
	nestedAlias := ids.ShortID{0, 0, 0, 1, 0xa}
	aliasMemberOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{sourceKey.Address()},
	}
	aliasDefinition := &multisig.AliasWithNonce{
		Alias: multisig.Alias{
			ID:     fakeMSigAlias,
			Owners: &aliasMemberOwners,
		},
	}
	nestedAliasDefinition := &multisig.AliasWithNonce{
		Alias: multisig.Alias{
			ID: nestedAlias,
			Owners: &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{fakeMSigAlias, sourceKey.Address()},
			},
		},
	}

	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		MultisigAliases:     []*multisig.Alias{&aliasDefinition.Alias, &nestedAliasDefinition.Alias},
	}

	env := newCaminoEnvironment(t, test.PhaseLast, caminoGenesisConf)

	type testCase struct {
		destinationChainID ids.ID
		to                 ids.ShortID
		expectedErr        error
		expectedMsigAddrs  []ids.ShortID
	}

	tests := map[string]testCase{
		"P->C export from msig wallet": {
			destinationChainID: ctx.CChainID,
			to:                 fakeMSigAlias,
			expectedErr:        nil,
			expectedMsigAddrs:  []ids.ShortID{fakeMSigAlias},
		},
		"P->C export simple account not multisig": {
			destinationChainID: ctx.CChainID,
			to:                 sourceKey.Address(),
			expectedErr:        nil,
			expectedMsigAddrs:  []ids.ShortID{},
		},
		// unsupported for now
		// "P->C export from nested msig wallet": {
		// 	destinationChainID: ctx.CChainID,
		// 	to:                 nestedAlias,
		// 	expectedErr:        nil,
		// 	expectedMsigAddrs:  []ids.ShortID{nestedAlias, fakeMSigAlias},
		// },
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			tx, err := env.txBuilder.NewExportTx(
				test.PreFundedBalance-test.TxFee,
				tt.destinationChainID,
				tt.to,
				test.FundedKeys,
				ids.ShortEmpty,
			)
			require.NoError(err)

			fakedState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			executor := CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   fakedState,
					Tx:      tx,
				},
			}
			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(err, tt.expectedErr)
			if err != nil {
				return
			}

			// Check atomic requests
			ar, exists := executor.AtomicRequests[tt.destinationChainID]
			require.True(exists)
			require.Len(ar.PutRequests, 1)
			req := ar.PutRequests[0]
			require.Equal(req.Traits[0], tt.to.Bytes())

			var (
				utxo    avax.UTXO
				aliases []verify.State
			)

			isMultisig := len(tt.expectedMsigAddrs) > 0
			if isMultisig {
				wrap := avax.UTXOWithMSig{}
				_, err = txs.Codec.Unmarshal(req.Value, &wrap)
				utxo = wrap.UTXO
				aliases = wrap.Aliases
				require.NoError(err)
				require.NotNil(aliases)
				require.True(len(aliases) > 0)
			} else {
				utxo = avax.UTXO{}
				_, err = txs.Codec.Unmarshal(req.Value, &utxo)
				require.NoError(err)
			}

			require.NotNil(utxo)
			out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
			require.True(ok)
			require.Equal(tt.to, out.OutputOwners.Addrs[0])

			// Check aliases contain the Output owner
			aliasIDs := make([]ids.ShortID, len(aliases))
			for i, alias := range aliases {
				aliasIDs[i] = alias.(*multisig.AliasWithNonce).ID
			}
			require.Equal(tt.expectedMsigAddrs, aliasIDs)
		})
	}
}

func TestCaminoCrossExport(t *testing.T) {
	ctx := test.Context(t)

	addr0 := test.FundedKeys[0].Address()
	addr1 := test.FundedKeys[1].Address()

	sigIndices := []uint32{0}
	signers := [][]*secp256k1.PrivateKey{{test.FundedKeys[0]}}

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{addr0},
	}

	tests := map[string]struct {
		utxos        []*avax.UTXO
		ins          []*avax.TransferableInput
		exportedOuts []*avax.TransferableOutput
		expectedErr  error
	}{
		"CrossTransferOutput OK": {
			utxos: []*avax.UTXO{
				generate.UTXO(ids.ID{0}, ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, ids.Empty, true),
			},
			ins: []*avax.TransferableInput{
				generate.InWithTxID(ids.ID{0}, ctx.AVAXAssetID, test.ValidatorWeight, ids.Empty, ids.Empty, sigIndices),
			},
			exportedOuts: []*avax.TransferableOutput{
				generate.CrossOut(ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, addr1),
			},
		},
		"CrossTransferOutput Invalid Recipient": {
			utxos: []*avax.UTXO{
				generate.UTXO(ids.ID{0}, ctx.AVAXAssetID, test.ValidatorWeight, outputOwners, ids.Empty, ids.Empty, true),
			},
			ins: []*avax.TransferableInput{
				generate.InWithTxID(ids.ID{0}, ctx.AVAXAssetID, test.ValidatorWeight, ids.Empty, ids.Empty, sigIndices),
			},
			exportedOuts: []*avax.TransferableOutput{
				generate.CrossOut(ctx.AVAXAssetID, test.ValidatorWeight-test.TxFee, outputOwners, ids.ShortEmpty),
			},
			expectedErr: secp256k1fx.ErrEmptyRecipient,
		},
	}

	for name, tt := range tests {
		t.Run("ExportTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment(t, test.PhaseLast, api.Camino{LockModeBondDeposit: true, VerifyNodeSignature: true})
			for _, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
			}

			exportTx := &txs.ExportTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         []*avax.TransferableOutput{},
				}},
				DestinationChain: env.ctx.XChainID,
				ExportedOutputs:  tt.exportedOuts,
			}

			tx, err := txs.NewSigned(exportTx, txs.Codec, signers)
			require.NoError(t, err)

			onAcceptState, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(t, err)

			err = exportTx.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   onAcceptState,
					Tx:      tx,
				},
			})

			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorMultisigAliasTx(t *testing.T) {
	ctx := test.Context(t)

	ownerKey, ownerAddr, owner := generate.KeyAndOwner(t, test.Keys[0])
	msigKeys, msigAlias, msigAliasOwners, msigOwner := generate.MsigAliasAndKeys([]*secp256k1.PrivateKey{test.Keys[1], test.Keys[2]}, 2, true)

	ownerUTXO := generate.UTXO(ids.GenerateTestID(), ctx.AVAXAssetID, test.TxFee, owner, ids.Empty, ids.Empty, true)
	msigUTXO := generate.UTXO(ids.GenerateTestID(), ctx.AVAXAssetID, test.TxFee, *msigOwner, ids.Empty, ids.Empty, true)

	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	tests := map[string]struct {
		state       func(*testing.T, *gomock.Controller, *txs.MultisigAliasTx, ids.ID) *state.MockDiff
		utx         *txs.MultisigAliasTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Add nested alias": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.MultisigAliasTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetMultisigAlias(msigAliasOwners.Addrs[0]).Return(&multisig.AliasWithNonce{}, nil)
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, ownerUTXO, []uint32{0}, false)},
					},
				},
				MultisigAlias: multisig.Alias{
					ID:     ids.ShortID{},
					Memo:   msigAlias.Memo,
					Owners: msigAlias.Owners,
				},
				Auth: &secp256k1fx.Input{SigIndices: []uint32{}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{ownerKey},
			},
			expectedErr: errNestedMsigAlias,
		},
		"Updating alias which does not exist": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.MultisigAliasTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				expect.GetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetMultisigAlias(msigAlias.ID).Return(nil, database.ErrNotFound)
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, ownerUTXO, []uint32{0}, false)},
					},
				},
				MultisigAlias: msigAlias.Alias,
				Auth:          &secp256k1fx.Input{SigIndices: []uint32{0, 1}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{ownerKey},
				{msigKeys[0], msigKeys[1]},
			},
			expectedErr: errAliasNotFound,
		},
		"Updating existing alias with less signatures than threshold": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.MultisigAliasTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				expect.GetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetMultisigAlias(msigAlias.ID).Return(msigAlias, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{
					msigAliasOwners.Addrs[0],
					msigAliasOwners.Addrs[1],
				}, []*multisig.AliasWithNonce{})
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, ownerUTXO, []uint32{0}, false)},
					},
				},
				MultisigAlias: msigAlias.Alias,
				Auth:          &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{ownerKey},
				{msigKeys[0]},
			},
			expectedErr: errAliasCredentialMismatch,
		},
		"OK, update existing alias": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.MultisigAliasTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				expect.GetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetMultisigAlias(msigAlias.ID).Return(msigAlias, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{
					msigAliasOwners.Addrs[0],
					msigAliasOwners.Addrs[1],
				}, []*multisig.AliasWithNonce{})
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{ownerUTXO}, []ids.ShortID{ownerAddr}, nil)
				s.EXPECT().SetMultisigAlias(&multisig.AliasWithNonce{
					Alias: msigAlias.Alias,
					Nonce: msigAlias.Nonce + 1,
				})
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, ownerUTXO, []uint32{0}, false)},
					},
				},
				MultisigAlias: msigAlias.Alias,
				Auth:          &secp256k1fx.Input{SigIndices: []uint32{0, 1}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{ownerKey},
				{msigKeys[0], msigKeys[1]},
			},
		},
		"OK, add new alias": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.MultisigAliasTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				expect.GetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{ownerUTXO}, []ids.ShortID{ownerAddr}, nil)
				s.EXPECT().SetMultisigAlias(&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID:     multisig.ComputeAliasID(txID),
						Memo:   msigAlias.Memo,
						Owners: msigAlias.Owners,
					},
					Nonce: 0,
				})
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, ownerUTXO, []uint32{0}, false)},
					},
				},
				MultisigAlias: multisig.Alias{
					ID:     ids.ShortID{},
					Memo:   msigAlias.Memo,
					Owners: msigAlias.Owners,
				},
				Auth: &secp256k1fx.Input{SigIndices: []uint32{}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{ownerKey},
			},
		},
		"OK, add new alias with multisig sender": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.MultisigAliasTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				expect.GetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{msigUTXO}, []ids.ShortID{
					msigAlias.ID,
					msigAliasOwners.Addrs[0],
					msigAliasOwners.Addrs[1],
				}, []*multisig.AliasWithNonce{msigAlias})
				s.EXPECT().SetMultisigAlias(&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID:     multisig.ComputeAliasID(txID),
						Memo:   msigAlias.Memo,
						Owners: msigAlias.Owners,
					},
					Nonce: 0,
				})
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, msigUTXO, []uint32{0, 1}, false)},
					},
				},
				MultisigAlias: multisig.Alias{
					ID:     ids.ShortID{},
					Memo:   msigAlias.Memo,
					Owners: msigAlias.Owners,
				},
				Auth: &secp256k1fx.Input{SigIndices: []uint32{}},
			},
			signers: [][]*secp256k1.PrivateKey{
				{msigKeys[0], msigKeys[1]},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			backend := newExecutorBackend(t, caminoGenesisConf, test.PhaseLast, nil)

			avax.SortTransferableInputsWithSigners(tt.utx.Ins, tt.signers)

			tx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(t, gomock.NewController(t), tt.utx, tx.ID()),
					Tx:      tx,
				},
			})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorAddDepositOfferTx(t *testing.T) {
	ctx := test.Context(t)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generate.KeyAndOwner(t, test.Keys[0])
	offerCreatorKey, offerCreatorAddr := test.Keys[1], test.Keys[1].Address()

	feeUTXO := generate.UTXO(ids.GenerateTestID(), ctx.AVAXAssetID, test.TxFee, feeOwner, ids.Empty, ids.Empty, true)

	offer1 := &deposit.Offer{
		UpgradeVersionID:      codec.UpgradeVersion1,
		Start:                 0,
		End:                   1,
		MinDuration:           1,
		MaxDuration:           1,
		MinAmount:             deposit.OfferMinDepositAmount,
		InterestRateNominator: 1,
		TotalMaxRewardAmount:  100,
	}

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins:          []*avax.TransferableInput{generate.InFromUTXO(t, feeUTXO, []uint32{0}, false)},
	}}

	tests := map[string]struct {
		state       func(*testing.T, *gomock.Controller, *txs.AddDepositOfferTx, ids.ID, *config.Config) *state.MockDiff
		utx         func() *txs.AddDepositOfferTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Not AthensPhase": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddDepositOfferTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(cfg.AthensPhaseTime.Add(-1 * time.Second))
				return s
			},
			utx: func() *txs.AddDepositOfferTx {
				return &txs.AddDepositOfferTx{
					BaseTx:                     baseTx,
					DepositOffer:               offer1,
					DepositOfferCreatorAddress: offerCreatorAddr,
					DepositOfferCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {offerCreatorKey},
			},
			expectedErr: errNotAthensPhase,
		},
		"Not offer creator": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddDepositOfferTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(cfg.AthensPhaseTime)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().GetAddressStates(utx.DepositOfferCreatorAddress).Return(as.AddressStateEmpty, nil)
				return s
			},
			utx: func() *txs.AddDepositOfferTx {
				return &txs.AddDepositOfferTx{
					BaseTx:                     baseTx,
					DepositOffer:               offer1,
					DepositOfferCreatorAddress: offerCreatorAddr,
					DepositOfferCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {offerCreatorKey},
			},
			expectedErr: errNotOfferCreator,
		},
		"Bad offer creator signature": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddDepositOfferTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(cfg.AthensPhaseTime)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().GetAddressStates(utx.DepositOfferCreatorAddress).Return(as.AddressStateOffersCreator, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.DepositOfferCreatorAddress}, nil)
				return s
			},
			utx: func() *txs.AddDepositOfferTx {
				return &txs.AddDepositOfferTx{
					BaseTx:                     baseTx,
					DepositOffer:               offer1,
					DepositOfferCreatorAddress: offerCreatorAddr,
					DepositOfferCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {feeOwnerKey},
			},
			expectedErr: errOfferCreatorCredentialMismatch,
		},
		"Supply overflow (v1, no existing offers)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddDepositOfferTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(cfg.AthensPhaseTime)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().GetAddressStates(utx.DepositOfferCreatorAddress).Return(as.AddressStateOffersCreator, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.DepositOfferCreatorAddress}, nil)
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-offer1.TotalMaxRewardAmount+1, nil)
				s.EXPECT().GetAllDepositOffers().Return(nil, nil)
				return s
			},
			utx: func() *txs.AddDepositOfferTx {
				return &txs.AddDepositOfferTx{
					BaseTx:                     baseTx,
					DepositOffer:               offer1,
					DepositOfferCreatorAddress: offerCreatorAddr,
					DepositOfferCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{offerCreatorKey},
			},
			expectedErr: errSupplyOverflow,
		},
		"Supply overflow (v1, existing offers)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddDepositOfferTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				chainTime := cfg.AthensPhaseTime
				existingOffers := []*deposit.Offer{
					{ // [0], expired
						UpgradeVersionID:     1,
						Start:                uint64(chainTime.Add(-2 * time.Second).Unix()),
						End:                  uint64(chainTime.Add(-1 * time.Second).Unix()),
						TotalMaxRewardAmount: 101,
					},
					{ // [1], not started yet with TotalMaxAmount
						UpgradeVersionID:      1,
						Start:                 uint64(chainTime.Add(1 * time.Second).Unix()),
						End:                   uint64(chainTime.Add(2 * time.Second).Unix()),
						MaxDuration:           100,
						TotalMaxAmount:        102,
						InterestRateNominator: deposit.InterestRateDenominator / 2,
					},
					{ // [2], not started yet with TotalMaxRewardAmount
						UpgradeVersionID:     1,
						Start:                uint64(chainTime.Add(1 * time.Second).Unix()),
						End:                  uint64(chainTime.Add(2 * time.Second).Unix()),
						TotalMaxRewardAmount: 103,
					},
					{ // [3], disabled
						UpgradeVersionID:     1,
						Start:                uint64(chainTime.Unix()),
						End:                  uint64(chainTime.Add(1 * time.Second).Unix()),
						Flags:                deposit.OfferFlagLocked,
						TotalMaxRewardAmount: 104,
					},
					{ // [4] active // shouldn't be possible, cause offers must always have one of the limits
						Start: uint64(chainTime.Unix()),
						End:   uint64(chainTime.Add(1 * time.Second).Unix()),
					},
					{ // [5] active, no interest rate
						UpgradeVersionID: 1,
						Start:            uint64(chainTime.Unix()),
						End:              uint64(chainTime.Add(1 * time.Second).Unix()),
						MaxDuration:      100,
						TotalMaxAmount:   102,
					},
					{ // [6] active with TotalMaxAmount
						MaxDuration:           100,
						InterestRateNominator: 100,
						Start:                 uint64(chainTime.Unix()),
						End:                   uint64(chainTime.Add(1 * time.Second).Unix()),
						TotalMaxAmount:        105,
						DepositedAmount:       50,
					},
					{ // [7] active with TotalMaxRewardAmount
						UpgradeVersionID:     1,
						Start:                uint64(chainTime.Add(-1 * time.Second).Unix()),
						End:                  uint64(chainTime.Add(1 * time.Second).Unix()),
						TotalMaxRewardAmount: 106,
						RewardedAmount:       50,
					},
				}
				promisedSupply := existingOffers[1].MaxRemainingRewardByTotalMaxAmount() + existingOffers[2].RemainingReward() +
					existingOffers[6].MaxRemainingRewardByTotalMaxAmount() + existingOffers[7].RemainingReward()
				currentSupply := cfg.RewardConfig.SupplyCap - promisedSupply - offer1.TotalMaxRewardAmount + 1

				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(chainTime)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().GetAddressStates(utx.DepositOfferCreatorAddress).Return(as.AddressStateOffersCreator, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.DepositOfferCreatorAddress}, nil)
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).Return(currentSupply, nil)
				s.EXPECT().GetAllDepositOffers().Return(existingOffers, nil)
				return s
			},
			utx: func() *txs.AddDepositOfferTx {
				return &txs.AddDepositOfferTx{
					BaseTx:                     baseTx,
					DepositOffer:               offer1,
					DepositOfferCreatorAddress: offerCreatorAddr,
					DepositOfferCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{offerCreatorKey},
			},
			expectedErr: errSupplyOverflow,
		},
		"OK: v1": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddDepositOfferTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(cfg.AthensPhaseTime)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().GetAddressStates(utx.DepositOfferCreatorAddress).Return(as.AddressStateOffersCreator, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.DepositOfferCreatorAddress}, nil)
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-offer1.TotalMaxRewardAmount, nil)
				s.EXPECT().GetAllDepositOffers().Return(nil, nil)

				offer := *utx.DepositOffer
				offer.ID = txID
				s.EXPECT().SetDepositOffer(&offer)

				expect.ConsumeUTXOs(t, s, utx.Ins)
				return s
			},
			utx: func() *txs.AddDepositOfferTx {
				return &txs.AddDepositOfferTx{
					BaseTx:                     baseTx,
					DepositOffer:               offer1,
					DepositOfferCreatorAddress: offerCreatorAddr,
					DepositOfferCreatorAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey},
				{offerCreatorKey},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			backend := newExecutorBackend(t, caminoGenesisConf, test.PhaseLast, nil)

			utx := tt.utx()
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(t, gomock.NewController(t), utx, tx.ID(), backend.Config),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorAddProposalTx(t *testing.T) {
	ctx := test.Context(t)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	caminoStateConf := &state.CaminoConfig{
		VerifyNodeSignature: caminoGenesisConf.VerifyNodeSignature,
		LockModeBondDeposit: caminoGenesisConf.LockModeBondDeposit,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generate.KeyAndOwner(t, test.Keys[0])
	bondOwnerKey, bondOwnerAddr, bondOwner := generate.KeyAndOwner(t, test.Keys[1])
	proposerKey, proposerAddr := test.Keys[2], test.Keys[2].Address()

	proposalBondAmt := uint64(100)
	feeUTXO := generate.UTXO(ids.ID{1, 2, 3, 4, 5}, ctx.AVAXAssetID, test.TxFee, feeOwner, ids.Empty, ids.Empty, true)
	bondUTXO := generate.UTXO(ids.ID{1, 2, 3, 4, 6}, ctx.AVAXAssetID, proposalBondAmt, bondOwner, ids.Empty, ids.Empty, true)

	applicantAddress := ids.ShortID{1, 1, 1}
	proposerNodeShortID := ids.ShortID{2, 2, 2}

	proposalWrapper := &txs.ProposalWrapper{Proposal: &dac.GeneralProposal{
		Start: 100, End: 100 + dac.GeneralProposalMinDuration, Options: [][]byte{{}},
	}}
	proposalBytes, err := txs.Codec.Marshal(txs.Version, proposalWrapper)
	require.NoError(t, err)

	baseTxWithBondAmt := func(bondAmt uint64) *txs.BaseTx {
		return &txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins: []*avax.TransferableInput{
				generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
				generate.InFromUTXO(t, bondUTXO, []uint32{0}, false),
			},
			Outs: []*avax.TransferableOutput{
				generate.Out(ctx.AVAXAssetID, bondAmt, bondOwner, ids.Empty, locked.ThisTxID),
			},
		}}
	}

	tests := map[string]struct {
		state       func(*testing.T, *gomock.Controller, *txs.AddProposalTx, ids.ID, *config.Config) *state.MockDiff
		utx         func(*config.Config) *txs.AddProposalTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Wrong lockModeBondDeposit flag": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: false}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errWrongLockMode,
		},
		"Not BerlinPhase": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime.Add(-1 * time.Second))
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errNotBerlinPhase,
		},
		"Too small bond": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount - 1),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errWrongProposalBondAmount,
		},
		"Too big bond": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount + 1),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errWrongProposalBondAmount,
		},
		"Proposal start before chaintime": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				proposalBytes, err := txs.Codec.Marshal(txs.Version, &txs.ProposalWrapper{Proposal: &dac.BaseFeeProposal{
					Start:   uint64(cfg.BerlinPhaseTime.Unix()) - 1,
					End:     uint64(cfg.BerlinPhaseTime.Unix()) + 1,
					Options: []uint64{1},
				}})
				require.NoError(t, err)
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errProposalStartToEarly,
		},
		"Proposal starts to far in the future": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				startTime := uint64(cfg.BerlinPhaseTime.Add(MaxFutureStartTime).Unix() + 1)
				proposalBytes, err := txs.Codec.Marshal(txs.Version, &txs.ProposalWrapper{Proposal: &dac.BaseFeeProposal{
					Start:   startTime,
					End:     startTime + 1,
					Options: []uint64{1},
				}})
				require.NoError(t, err)
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errProposalToFarInFuture,
		},
		"Wrong admin proposal type": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				proposalWrapper := &txs.ProposalWrapper{Proposal: &dac.AdminProposal{
					Proposal: &dac.BaseFeeProposal{
						Start: 100, End: 100 + dac.AddMemberProposalDuration, Options: []uint64{1},
					},
				}}
				proposalBytes, err := txs.Codec.Marshal(txs.Version, proposalWrapper)
				require.NoError(t, err)
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {bondOwnerKey},
			},
			expectedErr: errWrongAdminProposal,
		},
		"Admin proposal proposer doesn't have required address state": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateEmpty, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				proposalWrapper := &txs.ProposalWrapper{Proposal: &dac.AdminProposal{
					Proposal: &dac.AddMemberProposal{
						Start: 100, End: 100 + dac.AddMemberProposalDuration, ApplicantAddress: applicantAddress,
					},
				}}
				proposalBytes, err := txs.Codec.Marshal(txs.Version, proposalWrapper)
				require.NoError(t, err)
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {bondOwnerKey},
			},
			expectedErr: errNotPermittedToCreateProposal,
		},
		"Wrong proposer credential": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.ProposerAddress}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {bondOwnerKey},
			},
			expectedErr: errProposerCredentialMismatch,
		},
		// for more proposal specific test cases see camino_dac_test.go
		"Semantically invalid proposal": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.ProposerAddress}, nil)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateEmpty, nil) // not AddressStateConsortium
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errInvalidProposal,
		},
		"OK": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				staker1 := &state.Staker{TxID: ids.ID{0, 1}, SubnetID: constants.PrimaryNetworkID}
				staker2 := &state.Staker{TxID: ids.ID{0, 2}, SubnetID: ids.ID{0, 0, 1}}
				staker3 := &state.Staker{TxID: ids.ID{0, 3}, SubnetID: constants.PrimaryNetworkID}
				consortiumMemberAddr1 := ids.ShortID{0, 0, 0, 0, 1}
				consortiumMemberAddr3 := ids.ShortID{0, 0, 0, 0, 3}
				proposal, err := utx.Proposal()
				require.NoError(t, err)
				proposalState := proposal.CreateProposalState([]ids.ShortID{consortiumMemberAddr1, consortiumMemberAddr3})

				currentStakerIterator := state.NewMockStakerIterator(c)
				currentStakerIterator.EXPECT().Next().Return(true).Times(3)
				currentStakerIterator.EXPECT().Value().Return(staker3)
				currentStakerIterator.EXPECT().Value().Return(staker1)
				currentStakerIterator.EXPECT().Value().Return(staker2)
				currentStakerIterator.EXPECT().Next().Return(false)
				currentStakerIterator.EXPECT().Release()

				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.ProposerAddress}, nil)

				// * proposal verifier
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateConsortium, nil)
				s.EXPECT().GetShortIDLink(utx.ProposerAddress, state.ShortLinkKeyRegisterNode).
					Return(proposerNodeShortID, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, ids.NodeID(proposerNodeShortID)).
					Return(&state.Staker{}, nil)
				// *

				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, bondUTXO},
					[]ids.ShortID{
						feeOwnerAddr, bondOwnerAddr, // consumed
						bondOwnerAddr, // produced
					}, nil)
				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIterator, nil)
				s.EXPECT().GetShortIDLink(ids.ShortID(staker1.NodeID), state.ShortLinkKeyRegisterNode).
					Return(consortiumMemberAddr1, nil)
				s.EXPECT().GetShortIDLink(ids.ShortID(staker3.NodeID), state.ShortLinkKeyRegisterNode).
					Return(consortiumMemberAddr3, nil)
				s.EXPECT().AddProposal(txID, proposalState)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateBonded)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
		},
		"OK: Admin proposal": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddProposalTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				proposal, err := utx.Proposal()
				require.NoError(t, err)
				proposalState, err := proposal.CreateFinishedProposalState(1)
				require.NoError(t, err)
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateRoleConsortiumAdminProposer, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.ProposerAddress}, nil)

				// * proposal verifier
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(false)
				proposalsIterator.EXPECT().Release()
				proposalsIterator.EXPECT().Error().Return(nil)

				s.EXPECT().GetAddressStates(applicantAddress).Return(as.AddressStateKYCVerified, nil)
				s.EXPECT().GetProposalIterator().Return(proposalsIterator, nil)
				// *

				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, bondUTXO},
					[]ids.ShortID{
						feeOwnerAddr, bondOwnerAddr, // consumed
						bondOwnerAddr, // produced
					}, nil)
				s.EXPECT().AddProposal(txID, proposalState)
				s.EXPECT().AddProposalIDToFinish(txID)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateBonded)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddProposalTx {
				proposalWrapper := &txs.ProposalWrapper{Proposal: &dac.AdminProposal{
					OptionIndex: 1,
					Proposal: &dac.AddMemberProposal{
						Start: 100, End: 100 + dac.AddMemberProposalDuration, ApplicantAddress: applicantAddress,
					},
				}}
				proposalBytes, err := txs.Codec.Marshal(txs.Version, proposalWrapper)
				require.NoError(t, err)
				return &txs.AddProposalTx{
					BaseTx:          *baseTxWithBondAmt(cfg.CaminoConfig.DACProposalBondAmount),
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			backend := newExecutorBackend(t, caminoGenesisConf, test.PhaseLast, nil)

			backend.Config.CaminoConfig.DACProposalBondAmount = proposalBondAmt
			backend.Config.BerlinPhaseTime = proposalWrapper.StartTime()

			utx := tt.utx(backend.Config)
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(t, gomock.NewController(t), utx, tx.ID(), backend.Config),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorAddVoteTx(t *testing.T) {
	ctx := test.Context(t)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	caminoStateConf := &state.CaminoConfig{
		VerifyNodeSignature: caminoGenesisConf.VerifyNodeSignature,
		LockModeBondDeposit: caminoGenesisConf.LockModeBondDeposit,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generate.KeyAndOwner(t, test.Keys[0])
	voterKey1, voterAddr1 := test.Keys[1], test.Keys[1].Address()
	voterKey2, voterAddr2 := test.Keys[2], test.Keys[2].Address()
	_, voterAddr3 := test.Keys[3], test.Keys[3].Address()
	voterKey4, voterAddr4 := test.Keys[4], test.Keys[4].Address()

	feeUTXO := generate.UTXO(ids.ID{1, 2, 3, 4, 5}, ctx.AVAXAssetID, test.TxFee, feeOwner, ids.Empty, ids.Empty, true)

	simpleVote := &txs.VoteWrapper{Vote: &dac.SimpleVote{OptionIndex: 0}}
	voteBytes, err := txs.Codec.Marshal(txs.Version, simpleVote)
	require.NoError(t, err)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins: []*avax.TransferableInput{
			generate.InFromUTXO(t, feeUTXO, []uint32{0}, false),
		},
	}}

	proposalID := ids.ID{1, 1, 1, 1}
	proposal := &dac.BaseFeeProposalState{
		AllowedVoters: []ids.ShortID{voterAddr1, voterAddr3},
		Start:         100, End: 102,
		TotalAllowedVoters: 3,
		SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{Options: []dac.SimpleVoteOption[uint64]{
			{Value: 555},
			{Value: 123, Weight: 1},
			{Value: 7},
		}},
	}
	utils.Sort(proposal.AllowedVoters)

	tests := map[string]struct {
		state       func(*testing.T, *gomock.Controller, *txs.AddVoteTx, *config.Config) *state.MockDiff
		utx         func(*config.Config) *txs.AddVoteTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Wrong lockModeBondDeposit flag": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: false}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
			expectedErr: errWrongLockMode,
		},
		"Not BerlinPhase": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime.Add(-1 * time.Second))
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
			expectedErr: errNotBerlinPhase,
		},
		"Proposal not exist": { // should be in case of already inactive or non-existing proposal
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetProposal(utx.ProposalID).Return(nil, database.ErrNotFound)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
			expectedErr: database.ErrNotFound,
		},
		"Proposal is already inactive": { // shouldn't be possible, inactive proposals are removed
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.EndTime().Add(time.Second))
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
			expectedErr: ErrProposalInactive,
		},
		"Proposal is not active yet": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.StartTime().Add(-1 * time.Second))
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
			expectedErr: ErrProposalInactive,
		},
		"Voter isn't consortium member": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.StartTime())
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateEmpty, nil) // not AddressStateConsortiumMember
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
			expectedErr: errNotConsortiumMember,
		},
		"Wrong voter credential": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.StartTime())
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortium, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {feeOwnerKey},
			},
			expectedErr: errVoterCredentialMismatch,
		},
		"Wrong vote for this proposal (bad vote type)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.StartTime())
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortium, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				vote := &txs.VoteWrapper{Vote: &dac.DummyVote{}} // not SimpleVote
				voteBytes, err := txs.Codec.Marshal(txs.Version, vote)
				require.NoError(t, err)
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
			expectedErr: dac.ErrWrongVote,
		},
		"Wrong vote for this proposal (bad option index)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.StartTime())
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortium, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				simpleVote := &txs.VoteWrapper{Vote: &dac.SimpleVote{OptionIndex: 5}} // just 3 options in proposal
				voteBytes, err := txs.Codec.Marshal(txs.Version, simpleVote)
				require.NoError(t, err)
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
			expectedErr: dac.ErrWrongVote,
		},
		"Already voted": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.StartTime())
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortium, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr2,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey2},
			},
			expectedErr: dac.ErrNotAllowedToVoteOnProposal,
		},
		"Not allowed to vote for this proposal (wasn't active validator at proposal creation)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.StartTime())
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortium, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr4,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey4},
			},
			expectedErr: dac.ErrNotAllowedToVoteOnProposal,
		},
		"OK": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				voteIntf, err := utx.Vote()
				require.NoError(t, err)
				vote, ok := voteIntf.(*dac.SimpleVote)
				require.True(t, ok)
				updatedProposal, err := proposal.AddVote(utx.VoterAddress, vote)
				require.NoError(t, err)

				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.StartTime())
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortium, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().ModifyProposal(utx.ProposalID, updatedProposal)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
		},
		"OK: threshold is reached, proposal planned for execution": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.AddVoteTx, cfg *config.Config) *state.MockDiff {
				voteIntf, err := utx.Vote()
				require.NoError(t, err)
				vote, ok := voteIntf.(*dac.SimpleVote)
				require.True(t, ok)
				updatedProposal, err := proposal.AddVote(utx.VoterAddress, vote)
				require.NoError(t, err)

				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(proposal.StartTime())
				s.EXPECT().GetProposal(utx.ProposalID).Return(proposal, nil)
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortium, nil)
				expect.VerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(test.TxFee, nil)
				expect.VerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().ModifyProposal(utx.ProposalID, updatedProposal)
				s.EXPECT().AddProposalIDToFinish(utx.ProposalID)
				expect.ConsumeUTXOs(t, s, utx.Ins)
				return s
			},
			utx: func(cfg *config.Config) *txs.AddVoteTx {
				simpleVote := &txs.VoteWrapper{Vote: &dac.SimpleVote{OptionIndex: 1}}
				voteBytes, err := txs.Codec.Marshal(txs.Version, simpleVote)
				require.NoError(t, err)
				return &txs.AddVoteTx{
					BaseTx:       baseTx,
					ProposalID:   proposalID,
					VotePayload:  voteBytes,
					VoterAddress: voterAddr1,
					VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {voterKey1},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			backend := newExecutorBackend(t, caminoGenesisConf, test.PhaseLast, nil)

			backend.Config.BerlinPhaseTime = proposal.StartTime().Add(-1 * time.Second)

			utx := tt.utx(backend.Config)
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(t, gomock.NewController(t), utx, backend.Config),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorFinishProposalsTx(t *testing.T) {
	ctx := test.Context(t)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	caminoStateConf := &state.CaminoConfig{
		VerifyNodeSignature: caminoGenesisConf.VerifyNodeSignature,
		LockModeBondDeposit: caminoGenesisConf.LockModeBondDeposit,
	}

	proposalBondAmt := uint64(100)

	bondOwnerAddr1 := ids.ShortID{1}
	bondOwnerAddr2 := ids.ShortID{2}
	bondOwnerAddr3 := ids.ShortID{3}
	bondOwnerAddr4 := ids.ShortID{4}
	bondOwnerAddr5 := ids.ShortID{5}
	bondOwnerAddr6 := ids.ShortID{6}
	bondOwnerAddr7 := ids.ShortID{7}
	bondOwnerAddr8 := ids.ShortID{8}
	bondOwnerAddr9 := ids.ShortID{9}
	bondOwnerAddr10 := ids.ShortID{10}
	voterAddr1 := ids.ShortID{11}
	voterAddr2 := ids.ShortID{12}
	memberNodeShortID1 := ids.ShortID{13}
	memberNodeShortID2 := ids.ShortID{14}
	bondOwner1 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr1}, Threshold: 1}
	bondOwner2 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr2}, Threshold: 1}
	bondOwner3 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr3}, Threshold: 1}
	bondOwner4 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr4}, Threshold: 1}
	bondOwner5 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr5}, Threshold: 1}
	bondOwner6 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr6}, Threshold: 1}
	bondOwner7 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr7}, Threshold: 1}
	bondOwner8 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr8}, Threshold: 1}
	bondOwner9 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr9}, Threshold: 1}
	bondOwner10 := secp256k1fx.OutputOwners{Addrs: []ids.ShortID{bondOwnerAddr10}, Threshold: 1}

	earlySuccessfulProposalID := ids.ID{1, 1}
	earlyFailedProposalID := ids.ID{2, 2}
	expiredSuccessfulProposalID := ids.ID{3, 3}
	expiredFailedProposalID := ids.ID{4, 4}
	activeSuccessfulProposalID := ids.ID{5, 5}
	activeFailedProposalID := ids.ID{6, 6}
	earlySuccessfulProposalWithBondID := ids.ID{7, 7}
	expiredSuccessfulProposalWithBondID := ids.ID{8, 8}
	validatorTxID1 := ids.ID{9, 9}
	validatorTxID2 := ids.ID{10, 10}

	earlySuccessfulProposalUTXO := generate.UTXOWithIndex(earlySuccessfulProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner1, ids.Empty, earlySuccessfulProposalID, true)
	earlyFailedProposalUTXO := generate.UTXOWithIndex(earlyFailedProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner2, ids.Empty, earlyFailedProposalID, true)
	expiredSuccessfulProposalUTXO := generate.UTXOWithIndex(expiredSuccessfulProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner3, ids.Empty, expiredSuccessfulProposalID, true)
	expiredFailedProposalUTXO := generate.UTXOWithIndex(expiredFailedProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner4, ids.Empty, expiredFailedProposalID, true)
	activeSuccessfulProposalUTXO := generate.UTXOWithIndex(activeSuccessfulProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner5, ids.Empty, activeSuccessfulProposalID, true)
	activeFailedProposalUTXO := generate.UTXOWithIndex(activeFailedProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner6, ids.Empty, activeFailedProposalID, true)
	earlySuccessfulProposalWithBondUTXO := generate.UTXOWithIndex(earlySuccessfulProposalWithBondID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner7, ids.Empty, earlySuccessfulProposalWithBondID, true)
	expiredSuccessfulProposalWithBondUTXO := generate.UTXOWithIndex(expiredSuccessfulProposalWithBondID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner8, ids.Empty, expiredSuccessfulProposalWithBondID, true)
	additionalBondUTXO1 := generate.UTXOWithIndex(validatorTxID1, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner9, ids.Empty, validatorTxID1, true)
	additionalBondUTXO2 := generate.UTXOWithIndex(validatorTxID2, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner10, ids.Empty, validatorTxID2, true)

	pendingValidator1 := &state.Staker{TxID: validatorTxID1}
	pendingValidator2 := &state.Staker{TxID: validatorTxID2}

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
			earlySuccessfulProposalUTXO, earlyFailedProposalUTXO,
			expiredSuccessfulProposalUTXO, expiredFailedProposalUTXO,
		}, []uint32{}),
		Outs: []*avax.TransferableOutput{
			generate.OutFromUTXO(t, earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
			generate.OutFromUTXO(t, earlyFailedProposalUTXO, ids.Empty, ids.Empty),
			generate.OutFromUTXO(t, expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
			generate.OutFromUTXO(t, expiredFailedProposalUTXO, ids.Empty, ids.Empty),
		},
	}}

	mostVotedIndex := uint32(1)
	earlySuccessfulProposal := &dac.BaseFeeProposalState{
		AllowedVoters: []ids.ShortID{voterAddr1},
		Start:         100, End: 102,
		SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{Options: []dac.SimpleVoteOption[uint64]{
			{Value: 555},
			{Value: 123, Weight: 2},
			{Value: 7},
		}},
		TotalAllowedVoters: 3,
	}

	earlyFailedProposal := &dac.BaseFeeProposalState{
		AllowedVoters: []ids.ShortID{},
		Start:         100, End: 102,
		SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{Options: []dac.SimpleVoteOption[uint64]{
			{Value: 555, Weight: 1},
			{Value: 123, Weight: 1},
			{Value: 7, Weight: 1},
		}},
		TotalAllowedVoters: 3,
	}

	expiredSuccessfulProposal := &dac.BaseFeeProposalState{
		AllowedVoters: []ids.ShortID{voterAddr1},
		Start:         100, End: 102,
		SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{Options: []dac.SimpleVoteOption[uint64]{
			{Value: 555, Weight: 1},
			{Value: 123, Weight: 2},
			{Value: 7},
		}},
		TotalAllowedVoters: 4,
	}

	expiredFailedProposal := &dac.BaseFeeProposalState{
		AllowedVoters: []ids.ShortID{voterAddr1, voterAddr2},
		Start:         100, End: 102,
		SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{Options: []dac.SimpleVoteOption[uint64]{
			{Value: 555, Weight: 1},
			{Value: 123, Weight: 1},
			{Value: 7},
		}},
		TotalAllowedVoters: 4,
	}

	activeSuccessfulProposal := &dac.BaseFeeProposalState{
		AllowedVoters: []ids.ShortID{voterAddr1},
		Start:         100, End: 102,
		SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{Options: []dac.SimpleVoteOption[uint64]{
			{Value: 555, Weight: 1},
			{Value: 123, Weight: 2},
			{Value: 7},
		}},
		TotalAllowedVoters: 4,
	}

	activeFailedProposal := &dac.BaseFeeProposalState{
		AllowedVoters: []ids.ShortID{voterAddr1, voterAddr2},
		Start:         100, End: 102,
		SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{Options: []dac.SimpleVoteOption[uint64]{
			{Value: 555, Weight: 1},
			{Value: 123, Weight: 1},
			{Value: 7},
		}},
		TotalAllowedVoters: 4,
	}

	earlySuccessfulProposalWithBond := &dac.ExcludeMemberProposalState{
		AllowedVoters: []ids.ShortID{voterAddr1},
		Start:         100, End: 102,
		SimpleVoteOptions: dac.SimpleVoteOptions[bool]{Options: []dac.SimpleVoteOption[bool]{
			{Value: true, Weight: 2},
			{Value: false},
		}},
		TotalAllowedVoters: 3,
		MemberAddress:      bondOwnerAddr6,
	}

	expiredSuccessfulProposalWithBond := &dac.ExcludeMemberProposalState{
		AllowedVoters: []ids.ShortID{voterAddr1},
		Start:         100, End: 102,
		SimpleVoteOptions: dac.SimpleVoteOptions[bool]{Options: []dac.SimpleVoteOption[bool]{
			{Value: true, Weight: 2},
			{Value: false, Weight: 1},
		}},
		TotalAllowedVoters: 4,
		MemberAddress:      bondOwnerAddr5,
	}

	tests := map[string]struct {
		state       func(*testing.T, *gomock.Controller, *txs.FinishProposalsTx, ids.ID, *config.Config) *state.MockDiff
		utx         func(*config.Config) *txs.FinishProposalsTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Not BerlinPhase": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime.Add(-1 * time.Second))
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx:                             baseTx,
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlySuccessfulProposalID},
					EarlyFinishedFailedProposalIDs:     []ids.ID{earlyFailedProposalID},
					ExpiredSuccessfulProposalIDs:       []ids.ID{expiredSuccessfulProposalID},
					ExpiredFailedProposalIDs:           []ids.ID{expiredFailedProposalID},
				}
			},
			expectedErr: errNotBerlinPhase,
		},
		"Not zero credentials": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx:                             baseTx,
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlySuccessfulProposalID},
					EarlyFinishedFailedProposalIDs:     []ids.ID{earlyFailedProposalID},
					ExpiredSuccessfulProposalIDs:       []ids.ID{expiredSuccessfulProposalID},
					ExpiredFailedProposalIDs:           []ids.ID{expiredFailedProposalID},
				}
			},
			signers:     [][]*secp256k1.PrivateKey{{}},
			expectedErr: errWrongCredentialsNumber,
		},
		"Not expiration time": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{activeSuccessfulProposalID}, cfg.BerlinPhaseTime.Add(time.Second), nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							activeSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, activeSuccessfulProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					ExpiredSuccessfulProposalIDs: []ids.ID{activeSuccessfulProposalID},
				}
			},
			expectedErr: errProposalsAreNotExpiredYet,
		},
		"Not all expired proposals": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{expiredSuccessfulProposalID, expiredFailedProposalID}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					ExpiredSuccessfulProposalIDs: []ids.ID{expiredSuccessfulProposalID},
				}
			},
			expectedErr: errExpiredProposalsMismatch,
		},
		"Not all early finished proposals": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().
					Return([]ids.ID{earlySuccessfulProposalID, earlyFailedProposalID}, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlySuccessfulProposalID},
				}
			},
			expectedErr: errEarlyFinishedProposalsMismatch,
		},
		"Invalid inputs": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{expiredFailedProposalID}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{earlyFailedProposalID}, nil)
				lockTxIDs := append(utx.EarlyFinishedFailedProposalIDs, utx.ExpiredFailedProposalIDs...) //nolint:gocritic
				expect.Unlock(t, s, lockTxIDs, []ids.ShortID{
					bondOwnerAddr2, bondOwnerAddr4,
				}, []*avax.UTXO{
					earlyFailedProposalUTXO, expiredFailedProposalUTXO,
				}, locked.StateBonded)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{ // missing 2nd input
							generate.InFromUTXO(t, earlyFailedProposalUTXO, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlyFailedProposalUTXO, ids.Empty, ids.Empty),
							generate.OutFromUTXO(t, expiredFailedProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedFailedProposalIDs: []ids.ID{earlyFailedProposalID},
					ExpiredFailedProposalIDs:       []ids.ID{expiredFailedProposalID},
				}
			},
			expectedErr: errInvalidSystemTxBody,
		},
		"Invalid inputs: additional bond from proposal (e.g. excluded member pending validator)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return(utx.EarlyFinishedSuccessfulProposalIDs, nil)
				s.EXPECT().GetProposal(earlySuccessfulProposalWithBondID).Return(earlySuccessfulProposalWithBond, nil)

				// * proposalBondTxIDsGetter
				s.EXPECT().GetShortIDLink(earlySuccessfulProposalWithBond.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(memberNodeShortID1, nil)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID1)).
					Return(pendingValidator1, nil)
				// *

				lockTxIDs := append(utx.EarlyFinishedSuccessfulProposalIDs, validatorTxID1) //nolint:gocritic
				expect.Unlock(t, s, lockTxIDs, []ids.ShortID{
					bondOwnerAddr7, bondOwnerAddr9,
				}, []*avax.UTXO{
					earlySuccessfulProposalWithBondUTXO, additionalBondUTXO1,
				}, locked.StateBonded)

				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, earlySuccessfulProposalWithBondUTXO, []uint32{0}, false),
							// generate.TestInFromUTXO(additionalBondUTXO1, []uint32{}), // missing pending validator bond
						},
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlySuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							generate.OutFromUTXO(t, additionalBondUTXO1, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlySuccessfulProposalWithBondID},
				}
			},
			expectedErr: errInvalidSystemTxBody,
		},
		"Invalid outputs": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{expiredFailedProposalID}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{earlyFailedProposalID}, nil)
				lockTxIDs := append(utx.EarlyFinishedFailedProposalIDs, utx.ExpiredFailedProposalIDs...) //nolint:gocritic
				expect.Unlock(t, s, lockTxIDs, []ids.ShortID{
					bondOwnerAddr2, bondOwnerAddr4,
				}, []*avax.UTXO{
					earlyFailedProposalUTXO, expiredFailedProposalUTXO,
				}, locked.StateBonded)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							earlyFailedProposalUTXO, expiredFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
							generate.Out(ctx.AVAXAssetID, proposalBondAmt, bondOwner1, ids.Empty, ids.Empty), // expiredFailedProposalUTXO with different owner
						},
					}},
					EarlyFinishedFailedProposalIDs: []ids.ID{earlyFailedProposalID},
					ExpiredFailedProposalIDs:       []ids.ID{expiredFailedProposalID},
				}
			},
			expectedErr: errInvalidSystemTxBody,
		},
		"Invalid outputs: additional bond from proposal (e.g. excluded member pending validator)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return(utx.EarlyFinishedSuccessfulProposalIDs, nil)
				s.EXPECT().GetProposal(earlySuccessfulProposalWithBondID).Return(earlySuccessfulProposalWithBond, nil)

				// * proposalBondTxIDsGetter
				s.EXPECT().GetShortIDLink(earlySuccessfulProposalWithBond.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(memberNodeShortID1, nil)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID1)).
					Return(pendingValidator1, nil)
				// *

				lockTxIDs := append(utx.EarlyFinishedSuccessfulProposalIDs, validatorTxID1) //nolint:gocritic
				expect.Unlock(t, s, lockTxIDs, []ids.ShortID{
					bondOwnerAddr7, bondOwnerAddr9,
				}, []*avax.UTXO{
					earlySuccessfulProposalWithBondUTXO, additionalBondUTXO1,
				}, locked.StateBonded)

				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, earlySuccessfulProposalWithBondUTXO, []uint32{0}, false),
							generate.InFromUTXO(t, additionalBondUTXO1, []uint32{0}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlySuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							// generate.TestOutFromUTXO(additionalBondUTXO1, ids.Empty, ids.Empty), // missing pending validator bond
						},
					}},
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlySuccessfulProposalWithBondID},
				}
			},
			expectedErr: errInvalidSystemTxBody,
		},
		"Invalid inputs/outputs: additional bond from proposal (e.g. excluded member pending validator)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return(utx.EarlyFinishedSuccessfulProposalIDs, nil)
				s.EXPECT().GetProposal(earlySuccessfulProposalWithBondID).Return(earlySuccessfulProposalWithBond, nil)

				// * proposalBondTxIDsGetter
				s.EXPECT().GetShortIDLink(earlySuccessfulProposalWithBond.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(memberNodeShortID1, nil)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID1)).
					Return(pendingValidator1, nil)
				// *

				lockTxIDs := append(utx.EarlyFinishedSuccessfulProposalIDs, validatorTxID1) //nolint:gocritic
				expect.Unlock(t, s, lockTxIDs, []ids.ShortID{
					bondOwnerAddr7, bondOwnerAddr9,
				}, []*avax.UTXO{
					earlySuccessfulProposalWithBondUTXO, additionalBondUTXO1,
				}, locked.StateBonded)

				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, earlySuccessfulProposalWithBondUTXO, []uint32{0}, false),
							// generate.TestInFromUTXO(additionalBondUTXO1, []uint32{}), // missing pending validator bond
						},
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlySuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							// generate.TestOutFromUTXO(additionalBondUTXO1, ids.Empty, ids.Empty), // missing pending validator bond
						},
					}},
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlySuccessfulProposalWithBondID},
				}
			},
			expectedErr: errInvalidSystemTxBody,
		},
		"Proposal not exist": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return(utx.EarlyFinishedFailedProposalIDs, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr2,
				}, []*avax.UTXO{
					earlyFailedProposalUTXO,
				}, locked.StateBonded)
				s.EXPECT().GetProposal(earlyFailedProposalID).Return(nil, database.ErrNotFound)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							earlyFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlyFailedProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedFailedProposalIDs: []ids.ID{earlyFailedProposalID},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"Early-finish check: successful early finished swapped with successful expired": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{expiredSuccessfulProposalID}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().
					Return([]ids.ID{earlySuccessfulProposalID}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr1, bondOwnerAddr3,
				}, []*avax.UTXO{
					earlySuccessfulProposalUTXO, expiredSuccessfulProposalUTXO,
				}, locked.StateBonded)
				s.EXPECT().GetProposal(expiredSuccessfulProposalID).Return(expiredSuccessfulProposal, nil).Times(2)
				s.EXPECT().GetProposal(earlySuccessfulProposalID).Return(earlySuccessfulProposal, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							earlySuccessfulProposalUTXO,
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
							generate.OutFromUTXO(t, expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{expiredSuccessfulProposalID},
					ExpiredSuccessfulProposalIDs:       []ids.ID{earlySuccessfulProposalID},
				}
			},
			expectedErr: errNotEarlyFinishedProposal,
		},
		"Early-finish check: failed early finished swapped with failed expired": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{expiredSuccessfulProposalID}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().
					Return([]ids.ID{earlySuccessfulProposalID}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr1, bondOwnerAddr3,
				}, []*avax.UTXO{
					earlySuccessfulProposalUTXO, expiredSuccessfulProposalUTXO,
				}, locked.StateBonded)
				s.EXPECT().GetProposal(expiredSuccessfulProposalID).Return(expiredSuccessfulProposal, nil).Times(2)
				s.EXPECT().GetProposal(earlySuccessfulProposalID).Return(earlySuccessfulProposal, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							earlySuccessfulProposalUTXO,
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
							generate.OutFromUTXO(t, expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{expiredSuccessfulProposalID},
					ExpiredSuccessfulProposalIDs:       []ids.ID{earlySuccessfulProposalID},
				}
			},
			expectedErr: errNotEarlyFinishedProposal,
		},
		"Early-finish check: successful active in successful early finished": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().
					Return([]ids.ID{earlySuccessfulProposalID}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr5,
				}, []*avax.UTXO{
					activeSuccessfulProposalUTXO,
				}, locked.StateBonded)
				s.EXPECT().GetProposal(activeSuccessfulProposalID).Return(activeSuccessfulProposal, nil).Times(2)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							activeSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, activeSuccessfulProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{activeSuccessfulProposalID},
				}
			},
			expectedErr: errNotEarlyFinishedProposal,
		},
		"Early-finish check: failed active in failed early finished": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().
					Return([]ids.ID{earlyFailedProposalID}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr6,
				}, []*avax.UTXO{
					activeFailedProposalUTXO,
				}, locked.StateBonded)
				s.EXPECT().GetProposal(activeFailedProposalID).Return(activeFailedProposal, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							activeFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, activeFailedProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedFailedProposalIDs: []ids.ID{activeFailedProposalID},
				}
			},
			expectedErr: errNotEarlyFinishedProposal,
		},
		"Expire check: successful active in successful expired": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{expiredSuccessfulProposalID}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr5,
				}, []*avax.UTXO{
					activeSuccessfulProposalUTXO,
				}, locked.StateBonded)
				s.EXPECT().GetProposal(activeSuccessfulProposalID).Return(activeSuccessfulProposal, nil).Times(2)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							activeSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, activeSuccessfulProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					ExpiredSuccessfulProposalIDs: []ids.ID{activeSuccessfulProposalID},
				}
			},
			expectedErr: errNotExpiredProposal,
		},
		"Expire check: failed active in failed expired": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{expiredFailedProposalID}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr6,
				}, []*avax.UTXO{
					activeFailedProposalUTXO,
				}, locked.StateBonded)
				s.EXPECT().GetProposal(activeFailedProposalID).Return(activeFailedProposal, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							activeFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, activeFailedProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					ExpiredFailedProposalIDs: []ids.ID{activeFailedProposalID},
				}
			},
			expectedErr: errNotExpiredProposal,
		},
		"Success check: failed proposal in successful expired": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{expiredFailedProposalID}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr4,
				}, []*avax.UTXO{
					expiredFailedProposalUTXO,
				}, locked.StateBonded)

				s.EXPECT().GetProposal(expiredFailedProposalID).Return(expiredFailedProposal, nil).Times(2)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							expiredFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, expiredFailedProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					ExpiredSuccessfulProposalIDs: []ids.ID{expiredFailedProposalID},
				}
			},
			expectedErr: errNotSuccessfulProposal,
		},
		"Success check: failed proposal in successful early finished": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{earlyFailedProposalID}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr2,
				}, []*avax.UTXO{
					earlyFailedProposalUTXO,
				}, locked.StateBonded)

				s.EXPECT().GetProposal(earlyFailedProposalID).Return(earlyFailedProposal, nil).Times(2)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							earlyFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlyFailedProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlyFailedProposalID},
				}
			},
			expectedErr: errNotSuccessfulProposal,
		},
		"Success check: successful proposal in failed expired": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return([]ids.ID{expiredSuccessfulProposalID}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr3,
				}, []*avax.UTXO{
					expiredSuccessfulProposalUTXO,
				}, locked.StateBonded)

				s.EXPECT().GetProposal(expiredSuccessfulProposalID).Return(expiredSuccessfulProposal, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					ExpiredFailedProposalIDs: []ids.ID{expiredSuccessfulProposalID},
				}
			},
			expectedErr: errSuccessfulProposal,
		},
		"Success check: successful proposal in failed early finished": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return([]ids.ID{}, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{earlySuccessfulProposalID}, nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr1,
				}, []*avax.UTXO{
					earlySuccessfulProposalUTXO,
				}, locked.StateBonded)

				s.EXPECT().GetProposal(earlySuccessfulProposalID).Return(earlySuccessfulProposal, nil)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: generate.InsFromUTXOsWithSigIndices(t, []*avax.UTXO{
							earlySuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedFailedProposalIDs: []ids.ID{earlySuccessfulProposalID},
				}
			},
			expectedErr: errSuccessfulProposal,
		},
		"OK": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				expiredProposalIDs := append(utx.ExpiredSuccessfulProposalIDs, utx.ExpiredFailedProposalIDs...) //nolint:gocritic
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return(expiredProposalIDs, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().Return(append(utx.EarlyFinishedSuccessfulProposalIDs, utx.EarlyFinishedFailedProposalIDs...), nil)
				expect.Unlock(t, s, utx.ProposalIDs(), []ids.ShortID{
					bondOwnerAddr1, bondOwnerAddr2, bondOwnerAddr3, bondOwnerAddr4,
				}, []*avax.UTXO{
					earlySuccessfulProposalUTXO, earlyFailedProposalUTXO,
					expiredSuccessfulProposalUTXO, expiredFailedProposalUTXO,
				}, locked.StateBonded)

				s.EXPECT().GetProposal(earlySuccessfulProposalID).Return(earlySuccessfulProposal, nil).Times(2)
				s.EXPECT().SetBaseFee(earlySuccessfulProposal.Options[mostVotedIndex].Value) // proposal executor
				s.EXPECT().RemoveProposal(earlySuccessfulProposalID, earlySuccessfulProposal)
				s.EXPECT().RemoveProposalIDToFinish(earlySuccessfulProposalID)

				s.EXPECT().GetProposal(earlyFailedProposalID).Return(earlyFailedProposal, nil)
				s.EXPECT().RemoveProposal(earlyFailedProposalID, earlyFailedProposal)
				s.EXPECT().RemoveProposalIDToFinish(earlyFailedProposalID)

				s.EXPECT().GetProposal(expiredSuccessfulProposalID).Return(expiredSuccessfulProposal, nil).Times(2)
				s.EXPECT().SetBaseFee(expiredSuccessfulProposal.Options[mostVotedIndex].Value) // proposal executor
				s.EXPECT().RemoveProposal(expiredSuccessfulProposalID, expiredSuccessfulProposal)

				s.EXPECT().GetProposal(expiredFailedProposalID).Return(expiredFailedProposal, nil)
				s.EXPECT().RemoveProposal(expiredFailedProposalID, expiredFailedProposal)

				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx:                             baseTx,
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlySuccessfulProposalID},
					EarlyFinishedFailedProposalIDs:     []ids.ID{earlyFailedProposalID},
					ExpiredSuccessfulProposalIDs:       []ids.ID{expiredSuccessfulProposalID},
					ExpiredFailedProposalIDs:           []ids.ID{expiredFailedProposalID},
				}
			},
		},
		"OK: additional bond from proposals (e.g. excluded member pending validator)": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.FinishProposalsTx, txID ids.ID, cfg *config.Config) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(cfg.BerlinPhaseTime)
				s.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return(utx.ExpiredSuccessfulProposalIDs, cfg.BerlinPhaseTime, nil)
				s.EXPECT().GetProposalIDsToFinish().
					Return(utx.EarlyFinishedSuccessfulProposalIDs, nil)
				s.EXPECT().GetProposal(earlySuccessfulProposalWithBondID).Return(earlySuccessfulProposalWithBond, nil)
				s.EXPECT().GetProposal(expiredSuccessfulProposalWithBondID).Return(expiredSuccessfulProposalWithBond, nil)

				// * proposalBondTxIDsGetter
				s.EXPECT().GetShortIDLink(earlySuccessfulProposalWithBond.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(memberNodeShortID1, nil)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID1)).
					Return(pendingValidator1, nil)

				s.EXPECT().GetShortIDLink(expiredSuccessfulProposalWithBond.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(memberNodeShortID2, nil)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID2)).
					Return(pendingValidator2, nil)
				// *

				lockTxIDs := append(utx.EarlyFinishedSuccessfulProposalIDs, utx.ExpiredSuccessfulProposalIDs...) //nolint:gocritic
				lockTxIDs = append(lockTxIDs, validatorTxID1, validatorTxID2)
				expect.Unlock(t, s, lockTxIDs, []ids.ShortID{
					bondOwnerAddr7, bondOwnerAddr8, bondOwnerAddr9, bondOwnerAddr10,
				}, []*avax.UTXO{
					earlySuccessfulProposalWithBondUTXO, expiredSuccessfulProposalWithBondUTXO,
					additionalBondUTXO1, additionalBondUTXO2,
				}, locked.StateBonded)

				s.EXPECT().GetProposal(earlySuccessfulProposalWithBondID).Return(earlySuccessfulProposalWithBond, nil)
				s.EXPECT().RemoveProposal(earlySuccessfulProposalWithBondID, earlySuccessfulProposalWithBond)
				s.EXPECT().RemoveProposalIDToFinish(earlySuccessfulProposalWithBondID)

				s.EXPECT().GetProposal(expiredSuccessfulProposalWithBondID).Return(expiredSuccessfulProposalWithBond, nil)
				s.EXPECT().RemoveProposal(expiredSuccessfulProposalWithBondID, expiredSuccessfulProposalWithBond)

				// * proposalExecutor
				s.EXPECT().GetAddressStates(earlySuccessfulProposalWithBond.MemberAddress).
					Return(as.AddressStateConsortium, nil)
				s.EXPECT().SetAddressStates(earlySuccessfulProposalWithBond.MemberAddress, as.AddressStateEmpty)
				s.EXPECT().GetShortIDLink(earlySuccessfulProposalWithBond.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(memberNodeShortID1, nil)
				s.EXPECT().SetShortIDLink(memberNodeShortID1, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().SetShortIDLink(earlySuccessfulProposalWithBond.MemberAddress, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID1)).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID1)).
					Return(pendingValidator1, nil)
				s.EXPECT().DeletePendingValidator(pendingValidator1)

				s.EXPECT().GetAddressStates(expiredSuccessfulProposalWithBond.MemberAddress).
					Return(as.AddressStateConsortium, nil)
				s.EXPECT().SetAddressStates(expiredSuccessfulProposalWithBond.MemberAddress, as.AddressStateEmpty)
				s.EXPECT().GetShortIDLink(expiredSuccessfulProposalWithBond.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(memberNodeShortID2, nil)
				s.EXPECT().SetShortIDLink(memberNodeShortID2, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().SetShortIDLink(expiredSuccessfulProposalWithBond.MemberAddress, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID2)).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID2)).
					Return(pendingValidator2, nil)
				s.EXPECT().DeletePendingValidator(pendingValidator2)
				// *

				expect.ConsumeUTXOs(t, s, utx.Ins)
				expect.ProduceUTXOs(t, s, utx.Outs, txID, 0)

				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.InFromUTXO(t, earlySuccessfulProposalWithBondUTXO, []uint32{}, false),
							generate.InFromUTXO(t, expiredSuccessfulProposalWithBondUTXO, []uint32{}, false),
							generate.InFromUTXO(t, additionalBondUTXO1, []uint32{}, false),
							generate.InFromUTXO(t, additionalBondUTXO2, []uint32{}, false),
						},
						Outs: []*avax.TransferableOutput{
							generate.OutFromUTXO(t, earlySuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							generate.OutFromUTXO(t, expiredSuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							generate.OutFromUTXO(t, additionalBondUTXO1, ids.Empty, ids.Empty),
							generate.OutFromUTXO(t, additionalBondUTXO2, ids.Empty, ids.Empty),
						},
					}},
					EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlySuccessfulProposalWithBondID},
					ExpiredSuccessfulProposalIDs:       []ids.ID{expiredSuccessfulProposalWithBondID},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			backend := newExecutorBackend(t, caminoGenesisConf, test.PhaseLast, nil)

			backend.Config.BerlinPhaseTime = earlySuccessfulProposal.StartTime().Add(-1 * time.Second)

			utx := tt.utx(backend.Config)
			avax.SortTransferableInputs(utx.Ins)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: backend,
					State:   tt.state(t, gomock.NewController(t), utx, tx.ID(), backend.Config),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func getBitsFromAddressState(addrState as.AddressState) []as.AddressStateBit {
	bit := as.AddressStateBit(0)
	var addrStateBits []as.AddressStateBit
	for addrState > 0 {
		if addrState&1 == 1 {
			addrStateBits = append(addrStateBits, bit)
		}
		addrState >>= 1
		bit++
	}
	return addrStateBits
}

func TestGetBitsFromAddressState(t *testing.T) {
	tests := []struct {
		addressState             as.AddressState
		expectedAddressStateBits []as.AddressStateBit
	}{
		{0b00100101, []as.AddressStateBit{0, 2, 5}},
		{0b00000000, nil},
		{0b00000001, []as.AddressStateBit{0}},
		{0b11111111, []as.AddressStateBit{0, 1, 2, 3, 4, 5, 6, 7}},
		{0b10000000, []as.AddressStateBit{7}},
		{0b1111111111, []as.AddressStateBit{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
	}

	for _, tt := range tests {
		addressStateBits := getBitsFromAddressState(tt.addressState)
		require.Equal(t, tt.expectedAddressStateBits, addressStateBits)
	}
}
