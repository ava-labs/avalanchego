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
	"github.com/ava-labs/avalanchego/utils/nodeid"
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
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestCaminoEnv(t *testing.T) {
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment( /*postBanff*/ false, true, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		err := shutdownCaminoEnvironment(env)
		require.NoError(t, err)
	}()
	env.config.BanffTime = env.state.GetTimestamp()
}

func TestCaminoStandardTxExecutorAddValidatorTx(t *testing.T) {
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment( /*postBanff*/ true, false, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownCaminoEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	env.config.BanffTime = env.state.GetTimestamp()
	_, nodeID := nodeid.GenerateCaminoNodeKeyAndID()
	_, nodeID2 := nodeid.GenerateCaminoNodeKeyAndID()
	// msigKey, err := testKeyfactory.NewPrivateKey()
	// require.NoError(t, err)
	// msigAlias := msigKey.PublicKey().Address()

	addr0 := caminoPreFundedKeys[0].Address()
	addr1 := caminoPreFundedKeys[1].Address()

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
					startTime:     uint64(defaultValidateStartTime.Unix()) + 1,
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        nodeID,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr0)
			},
			expectedErr: nil,
		},
		"Validator's start time too early": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Unix()) - 1,
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        nodeID,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr0)
			},
			expectedErr: errTimestampNotBeforeStartTime,
		},
		"Validator's start time too far in the future": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Add(MaxFutureStartTime).Unix() + 1),
					endTime:       uint64(defaultValidateEndTime.Add(MaxFutureStartTime).Add(defaultMinStakingDuration).Unix() + 1),
					nodeID:        nodeID,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr0)
			},
			expectedErr: errFutureStakeTime,
		},
		"Validator already validating primary network": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Unix() + 1),
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        caminoPreFundedNodeIDs[0],
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(caminoPreFundedNodeIDs[0]), state.ShortLinkKeyRegisterNode, &addr0)
			},
			expectedErr: errValidatorExists,
		},
		"Validator in pending validator set of primary network": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultGenesisTime.Add(1 * time.Second).Unix()),
					endTime:       uint64(defaultGenesisTime.Add(1 * time.Second).Add(defaultMinStakingDuration).Unix()),
					nodeID:        nodeID2,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
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
					startTime:     uint64(defaultGenesisTime.Add(1 * time.Second).Unix()),
					endTime:       uint64(defaultGenesisTime.Add(1 * time.Second).Add(defaultMinStakingDuration).Unix()),
					nodeID:        nodeID2,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
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
					startTime:     uint64(defaultValidateStartTime.Unix() + 1),
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        nodeID,
					nodeOwnerAddr: addr1,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{caminoPreFundedKeys[1]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr1)
				utxoIDs, err := env.state.UTXOIDs(caminoPreFundedKeys[1].PublicKey().Address().Bytes(), ids.Empty, math.MaxInt32)
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
					startTime:     uint64(defaultValidateStartTime.Unix() + 1),
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        nodeID,
					nodeOwnerAddr: addr0,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr1)
			},
			expectedErr: errSignatureMissing,
		},
		// "Not enough sigs from msig node owner": {// TODO @evlekht can't be created with tx builder, needs manual creation
		// 	generateArgs: func() args {
		// 		return args{
		// 			stakeAmount:          env.config.MinValidatorStake,
		// 			startTime:            uint64(defaultValidateStartTime.Unix() + 1),
		// 			endTime:              uint64(defaultValidateEndTime.Unix()),
		// 			nodeID:               nodeID,
		// 			nodeOwnerAddr: msigAlias,
		// 			rewardAddress:        ids.ShortEmpty,
		// 			shares:               reward.PercentDenominator,
		// 			keys:                 []*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
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
		// 						caminoPreFundedKeys[0].Address(),
		// 						caminoPreFundedKeys[1].Address(),
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
	env := newCaminoEnvironment( /*postBanff*/ true, true, caminoGenesisConf)
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
	addDSTx, err := env.txBuilder.NewCaminoAddValidatorTx(
		env.config.MinValidatorStake,
		uint64(dsStartTime.Unix()),
		uint64(dsEndTime.Unix()),
		pendingDSValidatorID,
		caminoPreFundedKeys[0].Address(),
		ids.ShortEmpty,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], pendingDSValidatorKey},
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
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], caminoPreFundedNodeKeys[1]},
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
					startTime:  uint64(defaultValidateStartTime.Unix()) + 1,
					endTime:    uint64(defaultValidateEndTime.Unix()),
					nodeID:     nodeID,
					subnetID:   testSubnet1.ID(),
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
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
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
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
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], tempNodeKey},
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
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], pendingDSValidatorKey},
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
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], pendingDSValidatorKey},
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
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], pendingDSValidatorKey},
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
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
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
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], caminoPreFundedNodeKeys[1]},
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
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
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
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[0], testCaminoSubnet1ControlKeys[0], testCaminoSubnet1ControlKeys[1], nodeKey},
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
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	env := newCaminoEnvironment( /*postBanff*/ true, false, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownCaminoEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	_, nodeID := nodeid.GenerateCaminoNodeKeyAndID()
	addr0 := caminoPreFundedKeys[0].Address()
	env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr0)

	existingTxID := ids.GenerateTestID()
	env.config.BanffTime = env.state.GetTimestamp()
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}
	inputSigners := []*secp256k1.PrivateKey{caminoPreFundedKeys[0]}

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
			ins := make([]*avax.TransferableInput, len(tt.utxos))
			signers := make([][]*secp256k1.PrivateKey, len(tt.utxos))
			for i, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
				ins[i] = generateTestInFromUTXO(utxo, sigIndices)
				signers[i] = inputSigners
			}
			signers = append(signers, []*secp256k1.PrivateKey{caminoPreFundedKeys[0]})

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
						Start:  uint64(defaultValidateStartTime.Unix()) + 1,
						End:    uint64(defaultValidateEndTime.Unix()),
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
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}

	nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()

	now := time.Now()
	signers := [][]*secp256k1.PrivateKey{{caminoPreFundedKeys[0]}}
	signers[len(signers)-1] = []*secp256k1.PrivateKey{nodeKey}

	tests := map[string]struct {
		outs         []*avax.TransferableOutput
		ins          []*avax.TransferableInput
		expectedErr  error
		caminoConfig api.Camino
	}{
		"Locked out - LockModeBondDeposit: true": {
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.GenerateTestID()),
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
				generateTestIn(avaxAssetID, defaultCaminoValidatorWeight, ids.GenerateTestID(), ids.Empty, sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
			},
		},
		"Locked out - LockModeBondDeposit: false": {
			outs: []*avax.TransferableOutput{
				generateTestOut(avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.GenerateTestID()),
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
				generateTestIn(avaxAssetID, defaultCaminoValidatorWeight, ids.GenerateTestID(), ids.Empty, sigIndices),
			},
			expectedErr: locked.ErrWrongInType,
			caminoConfig: api.Camino{
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
			caminoConfig: api.Camino{
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
			caminoConfig: api.Camino{
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
			caminoConfig: api.Camino{
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
			caminoConfig: api.Camino{
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
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoConfig)
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
		})

		t.Run("ImportTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoConfig)
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

		t.Run("AddressStateTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			addressStateTxLockedTx := &txs.AddressStateTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				Address: caminoPreFundedKeys[0].PublicKey().Address(),
				State:   0,
				Remove:  false,
			}

			executor := generateExecutor(addressStateTxLockedTx, env)

			err := executor.AddressStateTx(addressStateTxLockedTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})

		t.Run("CreateChainTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoConfig)
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
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoConfig)
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
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoConfig)
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
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoConfig)
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
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoConfig)
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
	inputSigners := []*secp256k1.PrivateKey{caminoPreFundedKeys[0]}

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
			caminoConfig: api.Camino{
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
			caminoConfig: api.Camino{
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
			caminoConfig: api.Camino{
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
			caminoConfig: api.Camino{
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
			env := newCaminoEnvironment( /*postBanff*/ true, true, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				if err := shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			env.config.BanffTime = env.state.GetTimestamp()

			ins := make([]*avax.TransferableInput, len(tt.utxos))
			var signers [][]*secp256k1.PrivateKey
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
			signers = append(signers, []*secp256k1.PrivateKey{tt.nodeKey})

			addSubentValidatorTx := &txs.AddSubnetValidatorTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          ins,
					Outs:         tt.outs,
				}},
				SubnetValidator: txs.SubnetValidator{
					Validator: txs.Validator{
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
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	env := newCaminoEnvironment( /*postBanff*/ true, false, caminoGenesisConf)
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
				generateTestUTXO(txID, env.ctx.AVAXAssetID, defaultCaminoValidatorWeight, stakeOwners, ids.Empty, ids.Empty),
				generateTestUTXOWithIndex(unlockedUTXOTxID, 2, env.ctx.AVAXAssetID, defaultCaminoBalance, stakeOwners, ids.Empty, ids.Empty, true),
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
	env = newCaminoEnvironment( /*postBanff*/ true, false, caminoGenesisConf)
	env.ctx.Lock.Lock()
	env.config.BanffTime = env.state.GetTimestamp()

	t.Run("Happy path on abort", func(t *testing.T) {
		// utxoids are polluted with cached ids, need to clean this non-exported field
		for _, in := range ins {
			in.UTXOID = avax.UTXOID{
				TxID:        in.TxID,
				OutputIndex: in.OutputIndex,
			}
		}
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

// TODO @evlekht rewrite test, removal test cases not setting initial state for target, and by this not actually testing removal
func TestAddAddressStateTxExecutor(t *testing.T) {
	var (
		bob   = preFundedKeys[0].PublicKey().Address()
		alice = preFundedKeys[1].PublicKey().Address()
	)

	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	env := newCaminoEnvironment( /*postBanff*/ true, false, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		err := shutdownCaminoEnvironment(env)
		require.NoError(t, err)
	}()

	utxos, err := avax.GetAllUTXOs(env.state, set.Set[ids.ShortID]{
		caminoPreFundedKeys[0].Address(): struct{}{},
	})
	require.NoError(t, err)

	var unlockedUTXO *avax.UTXO
	for _, utxo := range utxos {
		if _, ok := utxo.Out.(*locked.Out); !ok {
			unlockedUTXO = utxo
			break
		}
	}
	require.NotNil(t, unlockedUTXO)

	out, ok := utxos[0].Out.(avax.TransferableOut)
	require.True(t, ok)
	unlockedUTXOAmount := out.Amount()

	signers := [][]*secp256k1.PrivateKey{
		{preFundedKeys[0]},
	}

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}

	tests := map[string]struct {
		UpgradeVersion uint16
		stateAddress   ids.ShortID
		targetAddress  ids.ShortID
		txFlag         as.AddressStateBit
		existingState  as.AddressState
		expectedErrs   []error
		expectedState  as.AddressState
		remove         bool
		executor       ids.ShortID
		executorAuth   *secp256k1fx.Input
	}{
		// Bob has Admin role, and he is trying to give himself Admin role (again)
		"State: Admin, Flag: Admin role, Add, Same Address": {
			stateAddress:  bob,
			targetAddress: bob,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateRoleAdmin,
			expectedState: as.AddressStateRoleAdmin,
			remove:        false,
		},
		// Bob has KYC role, and he is trying to give himself KYC role (again)
		"State: KYC, Flag: KYC role, Add, Same Address": {
			stateAddress:  bob,
			targetAddress: bob,
			txFlag:        as.AddressStateBitRoleKYC,
			existingState: as.AddressStateRoleKYC,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
			remove:        false,
		},
		// Bob has KYC role, and he is trying to give himself Admin role
		"State: KYC, Flag: Admin role, Add, Same Address": {
			stateAddress:  bob,
			targetAddress: bob,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateRoleKYC,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
			remove:        false,
		},
		// Bob has Admin role, and he is trying to give Alice Admin role
		"State: Admin, Flag: Admin role, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateRoleAdmin,
			expectedState: as.AddressStateRoleAdmin,
			remove:        false,
		},
		// Bob has Admin role, and he is trying to remove from Alice the Admin role
		"State: Admin, Flag: Admin role, Remove, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateRoleAdmin,
			expectedState: 0,
			remove:        true,
		},
		// Bob has Admin role, and he is trying to give Alice KYC Role
		"State: Admin, Flag: KYC role, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleKYC,
			existingState: as.AddressStateRoleAdmin,
			expectedState: as.AddressStateRoleKYC,
			remove:        false,
		},
		// Bob has Admin role, and he is trying to remove from Alice the KYC role
		"State: Admin, Flag: KYC role, Remove, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleKYC,
			existingState: as.AddressStateRoleAdmin,
			expectedState: 0,
			remove:        true,
		},
		// Bob has Admin role, and he is trying to remove it from himself
		"State: Admin, Flag: Admin role, Remove, Same Address": {
			stateAddress:  bob,
			targetAddress: bob,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateRoleAdmin,
			expectedState: 0,
			expectedErrs:  []error{errAdminCannotBeDeleted, errAdminCannotBeDeleted, errAdminCannotBeDeleted},
			remove:        true,
		},
		// Bob has Admin role, and he is trying to give Alice the KYC Verified state
		"State: Admin, Flag: KYC Verified, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitKYCVerified,
			existingState: as.AddressStateRoleAdmin,
			expectedState: as.AddressStateKYCVerified,
			remove:        false,
		},
		// Bob has Admin role, and he is trying to give Alice the KYC Expired state
		"State: Admin, Flag: KYC Expired, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitKYCExpired,
			existingState: as.AddressStateRoleAdmin,
			expectedState: as.AddressStateKYCExpired,
			remove:        false,
		},
		// Bob has Admin role, and he is trying to give Alice the Consortium state
		"State: Admin, Flag: Consortium, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitConsortium,
			existingState: as.AddressStateRoleAdmin,
			expectedState: as.AddressStateConsortiumMember,
			remove:        false,
			expectedErrs:  []error{nil, nil, errAddrStateNotPermitted},
		},
		// Bob has KYC role, and he is trying to give Alice KYC Expired state
		"State: KYC, Flag: KYC Expired, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitKYCExpired,
			existingState: as.AddressStateRoleKYC,
			expectedState: as.AddressStateKYCExpired,
			remove:        false,
		},
		// Bob has KYC role, and he is trying to give Alice KYC Expired state
		"State: KYC, Flag: KYC Verified, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitKYCVerified,
			existingState: as.AddressStateRoleKYC,
			expectedState: as.AddressStateKYCVerified,
			remove:        false,
		},
		// Some Address has Admin role, and he is trying to give Alice Admin role
		"Wrong address": {
			stateAddress:  ids.GenerateTestShortID(),
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateRoleAdmin,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
			remove:        false,
		},
		// An Empty Address has Admin role, and he is trying to give Alice Admin role
		"Empty State Address": {
			stateAddress:  ids.ShortEmpty,
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateRoleAdmin,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
			remove:        false,
		},
		// Bob has Admin role, and he is trying to give Admin role to an Empty Address
		"Empty Target Address": {
			stateAddress:  bob,
			targetAddress: ids.ShortEmpty,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateRoleAdmin,
			expectedErrs:  []error{txs.ErrEmptyAddress, txs.ErrEmptyAddress, txs.ErrEmptyAddress},
			remove:        false,
		},
		// Bob has empty addr state, and he is trying to give Alice Admin role
		"State: none, Flag: Admin role, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateEmpty,
			remove:        false,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
		},
		// Bob has empty addr state, and he is trying to remove Admin role from Alice
		"State: none, Flag: Admin role, Remove, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleAdmin,
			existingState: as.AddressStateEmpty,
			remove:        true,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
		},
		// Bob has empty addr state, and he is trying to give Alice KYC role
		"State: none, Flag: KYC role, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleKYC,
			existingState: as.AddressStateEmpty,
			remove:        false,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
		},
		// Bob has empty addr state, and he is trying to remove KYC role from Alice
		"State: none, Flag: KYC role, Remove, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitRoleKYC,
			existingState: as.AddressStateEmpty,
			remove:        true,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
		},
		// Bob has empty addr state, and he is trying to give Alice KYC Verified state
		"State: none, Flag: KYC Verified, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitKYCVerified,
			existingState: as.AddressStateEmpty,
			remove:        false,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
		},
		// Bob has empty addr state, and he is trying to remove KYC Verified state from Alice
		"State: none, Flag: KYC Verified, Remove, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        as.AddressStateBitKYCVerified,
			existingState: as.AddressStateEmpty,
			remove:        true,
			expectedErrs:  []error{errAddrStateNotPermitted, errAddrStateNotPermitted, errAddrStateNotPermitted},
		},
		// Bob has KYC role, and he is trying to give Alice KYC Expired state
		"Upgrade: 1, State: KYC, Flag: KYC Expired, Add, Different Address": {
			UpgradeVersion: 1,
			stateAddress:   bob,
			targetAddress:  alice,
			txFlag:         as.AddressStateBitKYCExpired,
			existingState:  as.AddressStateRoleKYC,
			expectedState:  as.AddressStateKYCExpired,
			expectedErrs:   []error{errNotAthensPhase, nil},
			remove:         false,
			executor:       bob,
			executorAuth:   &secp256k1fx.Input{SigIndices: []uint32{0}},
		},
		// Bob has KYC role, alice tries to executor her KYC Expired state
		"Upgrade: 1, State: KYC, Flag: KYC Expired, wrong executor": {
			UpgradeVersion: 1,
			stateAddress:   bob,
			targetAddress:  alice,
			txFlag:         as.AddressStateBitKYCExpired,
			existingState:  as.AddressStateRoleKYC,
			expectedState:  as.AddressStateKYCExpired,
			expectedErrs:   []error{errNotAthensPhase, errSignatureMissing, errSignatureMissing},
			remove:         false,
			executor:       alice,
			executorAuth:   &secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    env.ctx.NetworkID,
		BlockchainID: env.ctx.ChainID,
		Ins: []*avax.TransferableInput{
			generateTestInFromUTXO(unlockedUTXO, sigIndices),
		},
		Outs: []*avax.TransferableOutput{
			generateTestOut(avaxAssetID, unlockedUTXOAmount-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
		},
	}}

	chainTime := env.state.GetTimestamp()

	fnSetPhaseTimes := []func() string{
		func() string {
			env.config.AthensPhaseTime = chainTime.Add(1 * time.Second) // not yet active
			env.config.BerlinPhaseTime = chainTime.Add(2 * time.Second) // not yet active
			return "Sunrise"
		},
		func() string {
			env.config.AthensPhaseTime = chainTime                      // active
			env.config.BerlinPhaseTime = chainTime.Add(1 * time.Second) // not yet active
			return "Athens"
		},
		func() string {
			env.config.AthensPhaseTime = chainTime.Add(-1 * time.Second) // active
			env.config.BerlinPhaseTime = chainTime                       // active
			return "Berlin"
		},
	}

	for phase := 0; phase < 3; phase++ {
		phaseName := fnSetPhaseTimes[phase]()
		for name, tt := range tests {
			t.Run(fmt.Sprintf("%s; %s", phaseName, name), func(t *testing.T) {
				addressStateTx := &txs.AddressStateTx{
					UpgradeVersionID: codec.BuildUpgradeVersionID(tt.UpgradeVersion),
					BaseTx:           baseTx,
					Address:          tt.targetAddress,
					State:            tt.txFlag,
					Remove:           tt.remove,
					Executor:         tt.executor,
					ExecutorAuth:     tt.executorAuth,
				}

				tx, err := txs.NewSigned(addressStateTx, txs.Codec, signers)
				require.NoError(t, err)

				if tt.UpgradeVersion > 0 {
					tx.Creds = append(tx.Creds, tx.Creds[0])
				}

				onAcceptState, err := state.NewCaminoDiff(lastAcceptedID, env)
				require.NoError(t, err)

				executor := CaminoStandardTxExecutor{
					StandardTxExecutor{
						Backend: &env.backend,
						State:   onAcceptState,
						Tx:      tx,
					},
				}

				executor.State.SetAddressStates(tt.stateAddress, tt.existingState)

				err = addressStateTx.Visit(&executor)
				if len(tt.expectedErrs) > phase {
					require.ErrorIs(t, err, tt.expectedErrs[phase])
				} else {
					require.NoError(t, err)
				}

				if err == nil {
					targetStates, _ := executor.State.GetAddressStates(tt.targetAddress)
					require.Equal(t, targetStates, tt.expectedState)
				}
			})
		}
	}
}

func TestCaminoStandardTxExecutorDepositTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)

	feeOwnerKey, feeOwnerAddr, feeOwner := generateKeyAndOwner(t)
	utxoOwnerKey, utxoOwnerAddr, utxoOwner := generateKeyAndOwner(t)
	_, newUTXOOwnerAddr, newUTXOOwner := generateKeyAndOwner(t)
	offerOwnerKey, offerOwnerAddr, _ := generateKeyAndOwner(t)
	depositCreatorKey, depositCreatorAddr, _ := generateKeyAndOwner(t)

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

	feeUTXO := generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)
	doubleFeeUTXO := generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, defaultTxFee*2, feeOwner, ids.Empty, ids.Empty)
	unlockedUTXO1 := generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, offer.MinAmount, utxoOwner, ids.Empty, ids.Empty)
	unlockedUTXO2 := generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, offerWithMaxAmount.RemainingAmount(), utxoOwner, ids.Empty, ids.Empty)
	unlockedUTXO3 := generateTestUTXO(ids.ID{4}, ctx.AVAXAssetID, offerWithMaxRewardAmount.MaxRemainingAmountByReward(), utxoOwner, ids.Empty, ids.Empty)
	bondedUTXOWithMinAmount := generateTestUTXO(ids.ID{4}, ctx.AVAXAssetID, offer.MinAmount, utxoOwner, ids.Empty, ids.ID{100})

	phases := []struct {
		name    string
		prepare func(*caminoEnvironment, time.Time)
	}{
		{
			name: "SunrisePhase0",
			prepare: func(env *caminoEnvironment, chaintime time.Time) {
				env.config.AthensPhaseTime = chaintime.Add(1 * time.Second)
			},
		},
		{
			name: "AthensPhase",
			prepare: func(env *caminoEnvironment, chaintime time.Time) {
				env.config.AthensPhaseTime = chaintime
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
							generateTestStakeableIn(avaxAssetID, defaultCaminoBalance, uint64(defaultMinStakingDuration), []uint32{0}),
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
							generateTestStakeableOut(avaxAssetID, defaultCaminoBalance, uint64(defaultMinStakingDuration), utxoOwner),
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
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount-1, utxoOwner, locked.ThisTxID, ids.Empty),
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
							generateTestOut(ctx.AVAXAssetID, amt, utxoOwner, locked.ThisTxID, ids.Empty),
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
							generateTestOut(ctx.AVAXAssetID, amt, utxoOwner, locked.ThisTxID, ids.Empty),
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{unlockedUTXO1, nil}, nil, nil)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
							generateTestInFromUTXO(bondedUTXOWithMinAmount, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{unlockedUTXO1}, nil, nil)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
					expectVerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress}, nil)
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
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
					expectVerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress}, nil)
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
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
					expectVerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress}, nil)
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
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
					expectVerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress, utx.DepositCreatorAddress}, nil)
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
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
					expectVerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress, utx.DepositCreatorAddress}, nil)
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
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins,
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
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins,
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins,
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(doubleFeeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, defaultTxFee, newUTXOOwner, ids.Empty, ids.Empty),
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins,
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(bondedUTXOWithMinAmount, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.ID{100}),
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins,
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
							generateTestInFromUTXO(bondedUTXOWithMinAmount, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.ID{100}),
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins,
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, newUTXOOwner, locked.ThisTxID, ids.Empty),
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins,
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
				return s
			},
			utx: func() *txs.DepositTx {
				amt := offerWithMaxAmount.RemainingAmount()
				return &txs.DepositTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO2, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, amt, utxoOwner, locked.ThisTxID, ids.Empty),
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
					s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
					expectVerifyLock(t, s, utx.Ins,
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
					expectConsumeUTXOs(t, s, utx.Ins)
					expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
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
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO3, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, amt, utxoOwner, locked.ThisTxID, ids.Empty),
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
					expectVerifyMultisigPermission(t, s, []ids.ShortID{offerWithOwner.OwnerAddress, utx.DepositCreatorAddress}, nil)
					s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
					expectVerifyLock(t, s, utx.Ins,
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
					expectConsumeUTXOs(t, s, utx.Ins)
					expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateDeposited)
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
							generateTestInFromUTXO(feeUTXO, []uint32{0}),
							generateTestInFromUTXO(unlockedUTXO1, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, offer.MinAmount, utxoOwner, locked.ThisTxID, ids.Empty),
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
				ctrl := gomock.NewController(t)
				defer ctrl.Finish()
				env := newCaminoEnvironmentWithMocks(tt.caminoGenesisConf, nil)
				defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }() //nolint:lint

				phase.prepare(env, tt.chaintime)

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
						Backend: &env.backend,
						State:   tt.state(t, ctrl, utx, tx.ID(), env.config, phaseIndex),
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
	ctx, _ := defaultCtx(nil)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generateKeyAndOwner(t)
	owner1Key, owner1Addr, owner1 := generateKeyAndOwner(t)
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

	feeUTXO := generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)
	lessFeeUTXO := generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, 1, feeOwner, ids.Empty, ids.Empty)
	deposit1UTXO := generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty)
	deposit2UTXO := generateTestUTXO(ids.ID{4}, ctx.AVAXAssetID, deposit2.Amount, owner1, depositTxID2, ids.Empty)
	deposit1WithRewardUTXO := generateTestUTXO(ids.ID{5}, ctx.AVAXAssetID, deposit1WithReward.Amount, owner1, depositWithRewardTxID1, ids.Empty)
	deposit1UTXOLargerTxID := generateTestUTXO(ids.ID{6}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty)
	unlockedUTXOWithLargerTxID := generateTestUTXO(ids.ID{7}, ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty)

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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyUnlockDeposit(t, s, utx.Ins,
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
				Ins: generateInsFromUTXOs([]*avax.UTXO{feeUTXO, deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, deposit1.Amount-1, owner1, depositTxID1, ids.Empty),
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
				Ins: generateInsFromUTXOs([]*avax.UTXO{feeUTXO, deposit1UTXO}),
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
				Ins: generateInsFromUTXOs([]*avax.UTXO{deposit1UTXO, unlockedUTXOWithLargerTxID}),
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
				Ins: generateInsFromUTXOs([]*avax.UTXO{deposit2UTXO, deposit1UTXOLargerTxID}),
			}}},
			signers:     [][]*secp256k1.PrivateKey{},
			expectedErr: errMixedDeposits,
		},
		"Unlock not full amount, deposit expired": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				expectVerifyUnlockDeposit(t, s, utx.Ins,
					[]*avax.UTXO{deposit1UTXO},
					[]ids.ShortID{
						owner1Addr, // produced unlocked
					}, nil)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil).Times(2)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generateInsFromUTXOs([]*avax.UTXO{deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, deposit1.Amount-1, owner1, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, 1, owner1, depositTxID1, ids.Empty),
				},
			}}},
			expectedErr: errExpiredDepositNotFullyUnlocked,
		},
		"Unlock more, than available": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyUnlockDeposit(t, s, utx.Ins,
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
				Ins: generateInsFromUTXOs([]*avax.UTXO{feeUTXO, deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, deposit1HalfUnlockableAmount+1, owner1, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, deposit1.Amount-deposit1HalfUnlockableAmount-1, owner1, depositTxID1, ids.Empty),
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
				Ins: generateInsFromUTXOs([]*avax.UTXO{deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, deposit1.Amount-1, owner1, ids.Empty, ids.Empty),
				},
			}}},
			expectedErr: errBurnedDepositUnlock,
		},
		"Only burn fee, nothing unlocked": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyUnlockDeposit(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, lessFeeUTXO, deposit1UTXO},
					[]ids.ShortID{
						feeOwnerAddr, feeOwnerAddr, owner1Addr, // consumed (not expired deposit)
						feeOwnerAddr, // produced unlocked
					}, nil)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil).Times(2)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generateInsFromUTXOs([]*avax.UTXO{feeUTXO, lessFeeUTXO, deposit1UTXO}),
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, lessFeeUTXO.Out.(avax.Amounter).Amount(), feeOwner, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, deposit1UTXO.Out.(avax.Amounter).Amount(), owner1, depositTxID1, ids.Empty),
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
				expectVerifyUnlockDeposit(t, s, utx.Ins,
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generateInsFromUTXOs([]*avax.UTXO{deposit1WithRewardUTXO}),
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, deposit1WithReward.Amount, owner1, ids.Empty, ids.Empty),
				},
			}}},
		},
		"OK: unlock available amount, deposit is still unlocking": {
			state: func(t *testing.T, c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				// checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyUnlockDeposit(t, s, utx.Ins,
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				Ins: generateInsFromUTXOs([]*avax.UTXO{feeUTXO, deposit1UTXO, deposit2UTXO}),
				Outs: []*avax.TransferableOutput{
					generateTestOut(ctx.AVAXAssetID, deposit1HalfUnlockableAmount+deposit2HalfUnlockableAmount, owner1, ids.Empty, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, deposit1.Amount-deposit1HalfUnlockableAmount, owner1, depositTxID1, ids.Empty),
					generateTestOut(ctx.AVAXAssetID, deposit2.Amount-deposit2HalfUnlockableAmount, owner1, depositTxID2, ids.Empty),
				},
			}}},
			signers: [][]*secp256k1.PrivateKey{{feeOwnerKey}, {owner1Key}, {owner1Key}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(shutdownCaminoEnvironment(env)) }() //nolint:lint

			tt.utx.BlockchainID = env.ctx.ChainID
			tt.utx.NetworkID = env.ctx.NetworkID
			tx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(t, ctrl, tt.utx, tx.ID()),
					Tx:      tx,
				},
			})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorClaimTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)

	feeOwnerKey, feeOwnerAddr, feeOwner := generateKeyAndOwner(t)
	depositRewardOwnerKey, _, depositRewardOwner := generateKeyAndOwner(t)
	claimableOwnerKey1, _, claimableOwner1 := generateKeyAndOwner(t)
	claimableOwnerKey2, _, claimableOwner2 := generateKeyAndOwner(t)
	_, claimToOwnerAddr1, claimToOwner1 := generateKeyAndOwner(t)
	_, claimToOwnerAddr2, claimToOwner2 := generateKeyAndOwner(t)
	depositRewardMsigKeys, depositRewardMsigAlias, depositRewardMsigAliasOwner, depositRewardMsigOwner := generateMsigAliasAndKeys(t, 1, 2, false)
	claimableMsigKeys, claimableMsigAlias, claimableMsigAliasOwner, claimableMsigOwner := generateMsigAliasAndKeys(t, 2, 3, false)
	feeMsigKeys, feeMsigAlias, feeMsigAliasOwner, feeMsigOwner := generateMsigAliasAndKeys(t, 2, 2, false)

	feeUTXO := generateTestUTXO(ids.GenerateTestID(), ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)
	msigFeeUTXO := generateTestUTXO(ids.GenerateTestID(), ctx.AVAXAssetID, defaultTxFee, *feeMsigOwner, ids.Empty, ids.Empty)

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
			Ins:          []*avax.TransferableInput{generateTestInFromUTXO(feeUTXO, []uint32{0})},
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
				expectVerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
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
				expectVerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
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
				expectVerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, &state.Claimable{
					Owner:           claimableValidatorReward1.Owner,
					ValidatorReward: claimableValidatorReward1.ValidatorReward - utx.Claimables[0].Amount,
				})

				// claimable 2
				s.EXPECT().GetClaimable(claimableOwnerID2).Return(claimableValidatorReward2, nil)
				expectVerifyMultisigPermission(t, s, claimableOwner2.Addrs, nil)
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
				expectVerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, &state.Claimable{
					Owner:                claimable1.Owner,
					ExpiredDepositReward: 1,
				})

				// claimable 2
				s.EXPECT().GetClaimable(claimableOwnerID2).Return(claimable2, nil)
				expectVerifyMultisigPermission(t, s, claimableOwner2.Addrs, nil)
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
				expectVerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1, claimToOwnerAddr1, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)

				// deposit1
				expectVerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
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
				expectVerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
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
				expectVerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{
						feeOwnerAddr, claimToOwnerAddr1, claimToOwnerAddr2,
						claimToOwnerAddr1, claimToOwnerAddr1,
					}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)

				// claimable1
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimableValidatorReward1, nil)
				expectVerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, nil)

				// claimable2
				s.EXPECT().GetClaimable(claimableOwnerID2).Return(claimableValidatorReward2, nil)
				expectVerifyMultisigPermission(t, s, claimableOwner2.Addrs, nil)
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)

				// claimable1
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimableValidatorReward1, nil)
				expectVerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, nil)

				// claimable2
				s.EXPECT().GetClaimable(claimableOwnerID2).Return(claimableValidatorReward2, nil)
				expectVerifyMultisigPermission(t, s, claimableOwner2.Addrs, nil)
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)

				// deposit
				expectVerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)

				// deposit1
				expectVerifyMultisigPermission(t, s, depositRewardOwner.Addrs, nil)
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
				expectVerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimable1, nil)
				expectVerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO},
					[]ids.ShortID{feeOwnerAddr, claimToOwnerAddr1}, nil)
				s.EXPECT().GetTimestamp().Return(timestamp)
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimable1, nil)
				expectVerifyMultisigPermission(t, s, claimableOwner1.Addrs, nil)
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{msigFeeUTXO},
					[]ids.ShortID{
						feeMsigAlias.ID,
						feeMsigAliasOwner.Addrs[0],
						feeMsigAliasOwner.Addrs[1],
						claimToOwnerAddr1,
					},
					[]*multisig.AliasWithNonce{feeMsigAlias})
				s.EXPECT().GetTimestamp().Return(timestamp)
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)

				// deposit1
				expectVerifyMultisigPermission(t, s, []ids.ShortID{
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
				expectVerifyMultisigPermission(t, s, []ids.ShortID{
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
					Ins:          []*avax.TransferableInput{generateTestInFromUTXO(msigFeeUTXO, []uint32{0, 1})},
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(shutdownCaminoEnvironment(env)) }() //nolint:lint

			// ensuring that ins and outs from test case are sorted, signing tx

			avax.SortTransferableInputsWithSigners(tt.utx.Ins, tt.signers)
			avax.SortTransferableOutputs(tt.utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(err)

			// testing

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(t, ctrl, tt.utx, tx.ID()),
					Tx:      tx,
				},
			})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorRegisterNodeTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generateKeyAndOwner(t)
	consortiumMemberKey, consortiumMemberAddr, _ := generateKeyAndOwner(t)
	consortiumMemberMsigKeys, consortiumMemberMsigAlias, consortiumMemberMsigAliasOwner, _ := generateMsigAliasAndKeys(t, 2, 3, false)
	nodeKey1, nodeAddr1, _ := generateKeyAndOwner(t)
	nodeKey2, nodeAddr2, _ := generateKeyAndOwner(t)
	nodeID1 := ids.NodeID(nodeAddr1)
	nodeID2 := ids.NodeID(nodeAddr2)

	feeUTXO := generateTestUTXO(ids.GenerateTestID(), ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins:          []*avax.TransferableInput{generateTestInFromUTXO(feeUTXO, []uint32{0})},
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
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortiumMember, nil)
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
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(nodeAddr1, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
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
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(nodeAddr1, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
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
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(nodeAddr1, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
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
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(nodeAddr1, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, utx.OldNodeID).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, utx.OldNodeID).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetDeferredValidator(constants.PrimaryNetworkID, utx.OldNodeID).
					Return(nil, database.ErrNotFound)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
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
				expectConsumeUTXOs(t, s, utx.Ins)
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
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(ids.ShortEmpty, database.ErrNotFound)
				s.EXPECT().GetShortIDLink(ids.ShortID(utx.NewNodeID), state.ShortLinkKeyRegisterNode).
					Return(ids.ShortEmpty, database.ErrNotFound)
				expectVerifyMultisigPermission(t, s,
					[]ids.ShortID{
						utx.NodeOwnerAddress,
						consortiumMemberMsigAliasOwner.Addrs[0],
						consortiumMemberMsigAliasOwner.Addrs[1],
						consortiumMemberMsigAliasOwner.Addrs[2],
					},
					[]*multisig.AliasWithNonce{consortiumMemberMsigAlias})
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
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
				expectConsumeUTXOs(t, s, utx.Ins)
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
				s.EXPECT().GetAddressStates(utx.NodeOwnerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.NodeOwnerAddress, state.ShortLinkKeyRegisterNode).
					Return(ids.ShortEmpty, database.ErrNotFound)
				s.EXPECT().GetShortIDLink(ids.ShortID(utx.NewNodeID), state.ShortLinkKeyRegisterNode).
					Return(ids.ShortEmpty, database.ErrNotFound)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.NodeOwnerAddress}, nil)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
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
				expectConsumeUTXOs(t, s, utx.Ins)
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }() //nolint:lint

			utx := tt.utx()
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(t, ctrl, utx),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorRewardsImportTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)
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
						generateTestInFromUTXO(&utxos[0].UTXO, []uint32{0}),
						generateTestInFromUTXO(&utxos[1].UTXO, []uint32{0}),
					},
				}}}
			},
			utxos: []*avax.TimedUTXO{
				{
					UTXO:      *generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
					UTXO:      *generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
					UTXO:      *generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
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
						generateTestIn(ctx.AVAXAssetID, 1, ids.Empty, ids.Empty, []uint32{}),
					},
				}}}
			},
			utxos: []*avax.TimedUTXO{{
				UTXO:      *generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
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
				UTXO:      *generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
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
						generateTestInFromUTXO(&utxos[0].UTXO, []uint32{0}),
						generateTestInFromUTXO(&utxos[2].UTXO, []uint32{0}),
					},
				}}}
			},
			utxos: []*avax.TimedUTXO{
				{ // timed utxo, old enough
					UTXO:      *generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 3, *treasury.Owner, ids.Empty, ids.Empty),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{ // not timed utxo
					UTXO: *generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, 5, *treasury.Owner, ids.Empty, ids.Empty),
				},
				{ // timed utxo, old enough
					UTXO:      *generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, 2, *treasury.Owner, ids.Empty, ids.Empty),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{ // timed utxo, not old enough
					UTXO:      *generateTestUTXO(ids.ID{4}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
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
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, tt.sharedMemory(t, ctrl, tt.utxos))
			defer func() { require.NoError(shutdownCaminoEnvironment(env)) }() //nolint:lint

			utx := tt.utx(tt.utxos)
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			// utx.ImportedInputs must be already sorted cause of utxos sort
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(err)

			e := &CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(ctrl, utx, tx.ID()),
					Tx:      tx,
				},
			}

			require.ErrorIs(tx.Unsigned.Visit(e), tt.expectedErr)

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

func TestCaminoStandardTxExecutorAddressStateTxSuspendValidator(t *testing.T) {
	// finding first staker to remove
	env := newCaminoEnvironment( /*postBanff*/ true, false, api.Camino{LockModeBondDeposit: true})
	stakerIterator, err := env.state.GetCurrentStakerIterator()
	require.NoError(t, err)
	require.True(t, stakerIterator.Next())
	stakerToRemove := stakerIterator.Value()
	stakerIterator.Release()
	nodeID := stakerToRemove.NodeID
	consortiumMemberAddress, err := env.state.GetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode)
	require.NoError(t, err)
	require.NoError(t, shutdownCaminoEnvironment(env))

	initAdmin := caminoPreFundedKeys[0]
	// other common data

	outputOwners := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{consortiumMemberAddress},
	}

	type args struct {
		address    ids.ShortID
		remove     bool
		keys       []*secp256k1.PrivateKey
		changeAddr *secp256k1fx.OutputOwners
	}
	tests := map[string]struct {
		generateArgs func() args
		preExecute   func(*testing.T, *txs.Tx, state.State)
		expectedErr  error
		assert       func(*testing.T)
	}{
		"Set state to deferred from address with no roles -> ErrAddrStateNotPermitted": {
			generateArgs: func() args {
				return args{
					address:    consortiumMemberAddress,
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[1]}, // non-admin address
					changeAddr: outputOwners,
				}
			},
			preExecute:  func(t *testing.T, tx *txs.Tx, state state.State) {},
			expectedErr: errAddrStateNotPermitted,
		},
		"Set state to deferred from kyc address -> ErrAddrStateNotPermitted": {
			generateArgs: func() args {
				return args{
					address:    consortiumMemberAddress,
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[1]},
					changeAddr: outputOwners,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx, state state.State) {
				state.SetAddressStates(caminoPreFundedKeys[1].Address(), as.AddressStateRoleKYC)
			},
			expectedErr: errAddrStateNotPermitted,
		},
		"Happy path set state to deferred": {
			generateArgs: func() args {
				return args{
					address:    consortiumMemberAddress,
					keys:       []*secp256k1.PrivateKey{initAdmin},
					changeAddr: outputOwners,
				}
			},
			preExecute:  func(t *testing.T, tx *txs.Tx, state state.State) {},
			expectedErr: nil,
		},
		"Remove deferred state from non-admin address -> ErrAddrStateNotPermitted": {
			generateArgs: func() args {
				return args{
					address:    consortiumMemberAddress,
					remove:     true,
					keys:       []*secp256k1.PrivateKey{caminoPreFundedKeys[1]}, // non-admin address
					changeAddr: outputOwners,
				}
			},
			preExecute:  func(t *testing.T, tx *txs.Tx, state state.State) {},
			expectedErr: errAddrStateNotPermitted,
		},
		"Happy path set state to active": {
			generateArgs: func() args {
				return args{
					address:    consortiumMemberAddress,
					remove:     true,
					keys:       []*secp256k1.PrivateKey{initAdmin},
					changeAddr: outputOwners,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx, state state.State) {
				stakerToTransfer, err := state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
				require.NoError(t, err)
				state.DeleteCurrentValidator(stakerToTransfer)
				state.PutDeferredValidator(stakerToTransfer)
			},
			expectedErr: nil,
		},
		"Remove deferred state of an active validator": {
			generateArgs: func() args {
				return args{
					address:    consortiumMemberAddress,
					remove:     true,
					keys:       []*secp256k1.PrivateKey{initAdmin},
					changeAddr: outputOwners,
				}
			},
			preExecute:  func(t *testing.T, tx *txs.Tx, state state.State) {},
			expectedErr: errValidatorNotFound,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			caminoGenesisConf := api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        initAdmin.Address(),
			}
			env := newCaminoEnvironment( /*postBanff*/ true, false, caminoGenesisConf)
			env.ctx.Lock.Lock()
			defer func() {
				if err := shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			env.config.BanffTime = env.state.GetTimestamp()
			require.NoError(t, env.state.Commit())

			setAddressStateArgs := tt.generateArgs()
			tx, err := env.txBuilder.NewAddressStateTx(
				setAddressStateArgs.address,
				setAddressStateArgs.remove,
				as.AddressStateBitNodeDeferred,
				setAddressStateArgs.keys,
				setAddressStateArgs.changeAddr,
			)
			require.NoError(t, err)

			tt.preExecute(t, tx, env.state)
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
			if tt.expectedErr != nil {
				return
			}
			var stakerIterator state.StakerIterator
			if setAddressStateArgs.remove {
				stakerIterator, err = onAcceptState.GetCurrentStakerIterator()
				require.NoError(t, err)
			} else {
				stakerIterator, err = onAcceptState.GetDeferredStakerIterator()
				require.NoError(t, err)
			}
			require.True(t, stakerIterator.Next())
			stakerToRemove := stakerIterator.Value()
			stakerIterator.Release()
			require.Equal(t, stakerToRemove.NodeID, nodeID)
		})
	}
}

func TestCaminoStandardTxExecutorExportTxMultisig(t *testing.T) {
	fakeMSigAlias := preFundedKeys[0].Address()
	sourceKey := preFundedKeys[1]
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

	env := newCaminoEnvironment( /*postBanff*/ true, false, caminoGenesisConf)
	env.ctx.Lock.Lock()
	defer func() {
		if err := shutdownCaminoEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	type test struct {
		destinationChainID ids.ID
		to                 ids.ShortID
		expectedErr        error
		expectedMsigAddrs  []ids.ShortID
	}

	tests := map[string]test{
		"P->C export from msig wallet": {
			destinationChainID: cChainID,
			to:                 fakeMSigAlias,
			expectedErr:        nil,
			expectedMsigAddrs:  []ids.ShortID{fakeMSigAlias},
		},
		"P->C export simple account not multisig": {
			destinationChainID: cChainID,
			to:                 sourceKey.Address(),
			expectedErr:        nil,
			expectedMsigAddrs:  []ids.ShortID{},
		},
		// unsupported for now
		// "P->C export from nested msig wallet": {
		// 	destinationChainID: cChainID,
		// 	to:                 nestedAlias,
		// 	expectedErr:        nil,
		// 	expectedMsigAddrs:  []ids.ShortID{nestedAlias, fakeMSigAlias},
		// },
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			tx, err := env.txBuilder.NewExportTx(
				defaultBalance-defaultTxFee,
				tt.destinationChainID,
				tt.to,
				preFundedKeys,
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

			// Check atomic elts
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
	addr0 := caminoPreFundedKeys[0].Address()
	addr1 := caminoPreFundedKeys[1].Address()

	sigIndices := []uint32{0}
	signers := [][]*secp256k1.PrivateKey{{caminoPreFundedKeys[0]}}

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
				generateTestUTXO(ids.ID{0}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(avaxAssetID, defaultCaminoValidatorWeight, ids.Empty, ids.Empty, sigIndices),
			},
			exportedOuts: []*avax.TransferableOutput{
				generateCrossOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, addr1),
			},
		},
		"CrossTransferOutput Invalid Recipient": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{0}, avaxAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(avaxAssetID, defaultCaminoValidatorWeight, ids.Empty, ids.Empty, sigIndices),
			},
			exportedOuts: []*avax.TransferableOutput{
				generateCrossOut(avaxAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.ShortEmpty),
			},
			expectedErr: secp256k1fx.ErrEmptyRecipient,
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
			env := newCaminoEnvironment( /*postBanff*/ true, false, api.Camino{LockModeBondDeposit: true, VerifyNodeSignature: true})
			for _, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
			}
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
					Outs:         []*avax.TransferableOutput{},
				}},
				DestinationChain: env.ctx.XChainID,
				ExportedOutputs:  tt.exportedOuts,
			}

			executor := generateExecutor(exportTx, env)

			err := executor.ExportTx(exportTx)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorMultisigAliasTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)

	ownerKey, ownerAddr, owner := generateKeyAndOwner(t)
	msigKeys, msigAlias, msigAliasOwners, msigOwner := generateMsigAliasAndKeys(t, 2, 2, true)

	ownerUTXO := generateTestUTXO(ids.GenerateTestID(), ctx.AVAXAssetID, defaultTxFee, owner, ids.Empty, ids.Empty)
	msigUTXO := generateTestUTXO(ids.GenerateTestID(), ctx.AVAXAssetID, defaultTxFee, *msigOwner, ids.Empty, ids.Empty)

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
						Ins:          []*avax.TransferableInput{generateTestInFromUTXO(ownerUTXO, []uint32{0})},
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
				expectGetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetMultisigAlias(msigAlias.ID).Return(nil, database.ErrNotFound)
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generateTestInFromUTXO(ownerUTXO, []uint32{0})},
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
				expectGetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetMultisigAlias(msigAlias.ID).Return(msigAlias, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{
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
						Ins:          []*avax.TransferableInput{generateTestInFromUTXO(ownerUTXO, []uint32{0})},
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
				expectGetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetMultisigAlias(msigAlias.ID).Return(msigAlias, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{
					msigAliasOwners.Addrs[0],
					msigAliasOwners.Addrs[1],
				}, []*multisig.AliasWithNonce{})
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{ownerUTXO}, []ids.ShortID{ownerAddr}, nil)
				s.EXPECT().SetMultisigAlias(&multisig.AliasWithNonce{
					Alias: msigAlias.Alias,
					Nonce: msigAlias.Nonce + 1,
				})
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generateTestInFromUTXO(ownerUTXO, []uint32{0})},
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
				expectGetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{ownerUTXO}, []ids.ShortID{ownerAddr}, nil)
				s.EXPECT().SetMultisigAlias(&multisig.AliasWithNonce{
					Alias: multisig.Alias{
						ID:     multisig.ComputeAliasID(txID),
						Memo:   msigAlias.Memo,
						Owners: msigAlias.Owners,
					},
					Nonce: 0,
				})
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generateTestInFromUTXO(ownerUTXO, []uint32{0})},
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
				expectGetMultisigAliases(t, s, msigAliasOwners.Addrs, nil)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{msigUTXO}, []ids.ShortID{
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)
				return s
			},
			utx: &txs.MultisigAliasTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins:          []*avax.TransferableInput{generateTestInFromUTXO(msigUTXO, []uint32{0, 1})},
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)

			defer func() { require.NoError(shutdownCaminoEnvironment(env)) }() //nolint:lint

			avax.SortTransferableInputsWithSigners(tt.utx.Ins, tt.signers)

			tx, err := txs.NewSigned(tt.utx, txs.Codec, tt.signers)
			require.NoError(err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(t, ctrl, tt.utx, tx.ID()),
					Tx:      tx,
				},
			})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorAddDepositOfferTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generateKeyAndOwner(t)
	offerCreatorKey, offerCreatorAddr, _ := generateKeyAndOwner(t)

	feeUTXO := generateTestUTXO(ids.GenerateTestID(), ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)

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
		Ins:          []*avax.TransferableInput{generateTestInFromUTXO(feeUTXO, []uint32{0})},
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
				s.EXPECT().GetTimestamp().Return(time.Unix(100, 0))
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
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
				s.EXPECT().GetTimestamp().Return(time.Unix(100, 0))
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().GetAddressStates(utx.DepositOfferCreatorAddress).Return(as.AddressStateOffersCreator, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.DepositOfferCreatorAddress}, nil)
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
				chainTime := time.Unix(100, 0)

				s := state.NewMockDiff(c)
				s.EXPECT().GetTimestamp().Return(chainTime)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().GetAddressStates(utx.DepositOfferCreatorAddress).Return(as.AddressStateOffersCreator, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.DepositOfferCreatorAddress}, nil)
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
				chainTime := time.Unix(100, 0)
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
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().GetAddressStates(utx.DepositOfferCreatorAddress).Return(as.AddressStateOffersCreator, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.DepositOfferCreatorAddress}, nil)
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
				s.EXPECT().GetTimestamp().Return(time.Time{})
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().GetAddressStates(utx.DepositOfferCreatorAddress).Return(as.AddressStateOffersCreator, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.DepositOfferCreatorAddress}, nil)
				s.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).
					Return(cfg.RewardConfig.SupplyCap-offer1.TotalMaxRewardAmount, nil)
				s.EXPECT().GetAllDepositOffers().Return(nil, nil)

				offer := *utx.DepositOffer
				offer.ID = txID
				s.EXPECT().SetDepositOffer(&offer)

				expectConsumeUTXOs(t, s, utx.Ins)
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }()

			utx := tt.utx()
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(t, ctrl, utx, tx.ID(), env.config),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorAddProposalTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	caminoStateConf := &state.CaminoConfig{
		VerifyNodeSignature: caminoGenesisConf.VerifyNodeSignature,
		LockModeBondDeposit: caminoGenesisConf.LockModeBondDeposit,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generateKeyAndOwner(t)
	bondOwnerKey, bondOwnerAddr, bondOwner := generateKeyAndOwner(t)
	proposerKey, proposerAddr, _ := generateKeyAndOwner(t)

	proposalBondAmt := uint64(100)
	feeUTXO := generateTestUTXO(ids.ID{1, 2, 3, 4, 5}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)
	bondUTXO := generateTestUTXO(ids.ID{1, 2, 3, 4, 6}, ctx.AVAXAssetID, proposalBondAmt, bondOwner, ids.Empty, ids.Empty)

	applicantAddress := ids.ShortID{1, 1, 1}

	proposalWrapper := &txs.ProposalWrapper{Proposal: &dac.BaseFeeProposal{
		Start: 100, End: 101, Options: []uint64{1},
	}}
	proposalBytes, err := txs.Codec.Marshal(txs.Version, proposalWrapper)
	require.NoError(t, err)

	baseTxWithBondAmt := func(bondAmt uint64) *txs.BaseTx {
		return &txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins: []*avax.TransferableInput{
				generateTestInFromUTXO(feeUTXO, []uint32{0}),
				generateTestInFromUTXO(bondUTXO, []uint32{0}),
			},
			Outs: []*avax.TransferableOutput{
				generateTestOut(ctx.AVAXAssetID, bondAmt, bondOwner, ids.Empty, locked.ThisTxID),
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
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.ProposerAddress}, nil)
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
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.ProposerAddress}, nil)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateEmpty, nil) // not AddressStateCaminoProposer
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
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.ProposerAddress}, nil)

				// * proposal verifier
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(false)
				proposalsIterator.EXPECT().Release()
				proposalsIterator.EXPECT().Error().Return(nil)

				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateCaminoProposer, nil)
				s.EXPECT().GetProposalIterator().Return(proposalsIterator, nil)
				// *

				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins,
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
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateBonded)
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
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.ProposerAddress}, nil)

				// * proposal verifier
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(false)
				proposalsIterator.EXPECT().Release()
				proposalsIterator.EXPECT().Error().Return(nil)

				s.EXPECT().GetAddressStates(applicantAddress).Return(as.AddressStateKYCVerified, nil)
				s.EXPECT().GetProposalIterator().Return(proposalsIterator, nil)
				// *

				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins,
					[]*avax.UTXO{feeUTXO, bondUTXO},
					[]ids.ShortID{
						feeOwnerAddr, bondOwnerAddr, // consumed
						bondOwnerAddr, // produced
					}, nil)
				s.EXPECT().AddProposal(txID, proposalState)
				s.EXPECT().AddProposalIDToFinish(txID)
				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceNewlyLockedUTXOs(t, s, utx.Outs, txID, 0, locked.StateBonded)
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }()

			env.config.CaminoConfig.DACProposalBondAmount = proposalBondAmt
			env.config.BerlinPhaseTime = proposalWrapper.StartTime()

			utx := tt.utx(env.config)
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(t, ctrl, utx, tx.ID(), env.config),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorAddVoteTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	caminoStateConf := &state.CaminoConfig{
		VerifyNodeSignature: caminoGenesisConf.VerifyNodeSignature,
		LockModeBondDeposit: caminoGenesisConf.LockModeBondDeposit,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generateKeyAndOwner(t)
	voterKey1, voterAddr1, _ := generateKeyAndOwner(t)
	voterKey2, voterAddr2, _ := generateKeyAndOwner(t)
	_, voterAddr3, _ := generateKeyAndOwner(t)
	voterKey4, voterAddr4, _ := generateKeyAndOwner(t)

	feeUTXO := generateTestUTXO(ids.ID{1, 2, 3, 4, 5}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)

	simpleVote := &txs.VoteWrapper{Vote: &dac.SimpleVote{OptionIndex: 0}}
	voteBytes, err := txs.Codec.Marshal(txs.Version, simpleVote)
	require.NoError(t, err)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins: []*avax.TransferableInput{
			generateTestInFromUTXO(feeUTXO, []uint32{0}),
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
			expectedErr: errProposalInactive,
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
			expectedErr: errProposalInactive,
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
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortiumMember, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
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
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortiumMember, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
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
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortiumMember, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
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
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortiumMember, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
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
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortiumMember, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
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
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortiumMember, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().ModifyProposal(utx.ProposalID, updatedProposal)
				expectConsumeUTXOs(t, s, utx.Ins)
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
				s.EXPECT().GetAddressStates(utx.VoterAddress).Return(as.AddressStateConsortiumMember, nil)
				expectVerifyMultisigPermission(t, s, []ids.ShortID{utx.VoterAddress}, nil)
				s.EXPECT().GetBaseFee().Return(defaultTxFee, nil)
				expectVerifyLock(t, s, utx.Ins, []*avax.UTXO{feeUTXO}, []ids.ShortID{feeOwnerAddr}, nil)
				s.EXPECT().ModifyProposal(utx.ProposalID, updatedProposal)
				s.EXPECT().AddProposalIDToFinish(utx.ProposalID)
				expectConsumeUTXOs(t, s, utx.Ins)
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }()

			env.config.BerlinPhaseTime = proposal.StartTime().Add(-1 * time.Second)

			utx := tt.utx(env.config)
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(t, ctrl, utx, env.config),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorFinishProposalsTx(t *testing.T) {
	ctx, _ := defaultCtx(nil)
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

	earlySuccessfulProposalUTXO := generateTestUTXOWithIndex(earlySuccessfulProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner1, ids.Empty, earlySuccessfulProposalID, true)
	earlyFailedProposalUTXO := generateTestUTXOWithIndex(earlyFailedProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner2, ids.Empty, earlyFailedProposalID, true)
	expiredSuccessfulProposalUTXO := generateTestUTXOWithIndex(expiredSuccessfulProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner3, ids.Empty, expiredSuccessfulProposalID, true)
	expiredFailedProposalUTXO := generateTestUTXOWithIndex(expiredFailedProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner4, ids.Empty, expiredFailedProposalID, true)
	activeSuccessfulProposalUTXO := generateTestUTXOWithIndex(activeSuccessfulProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner5, ids.Empty, activeSuccessfulProposalID, true)
	activeFailedProposalUTXO := generateTestUTXOWithIndex(activeFailedProposalID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner6, ids.Empty, activeFailedProposalID, true)
	earlySuccessfulProposalWithBondUTXO := generateTestUTXOWithIndex(earlySuccessfulProposalWithBondID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner7, ids.Empty, earlySuccessfulProposalWithBondID, true)
	expiredSuccessfulProposalWithBondUTXO := generateTestUTXOWithIndex(expiredSuccessfulProposalWithBondID, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner8, ids.Empty, expiredSuccessfulProposalWithBondID, true)
	additionalBondUTXO1 := generateTestUTXOWithIndex(validatorTxID1, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner9, ids.Empty, validatorTxID1, true)
	additionalBondUTXO2 := generateTestUTXOWithIndex(validatorTxID2, 0, ctx.AVAXAssetID, proposalBondAmt, bondOwner10, ids.Empty, validatorTxID2, true)

	pendingValidator1 := &state.Staker{TxID: validatorTxID1}
	pendingValidator2 := &state.Staker{TxID: validatorTxID2}

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
			earlySuccessfulProposalUTXO, earlyFailedProposalUTXO,
			expiredSuccessfulProposalUTXO, expiredFailedProposalUTXO,
		}, []uint32{}),
		Outs: []*avax.TransferableOutput{
			generateTestOutFromUTXO(earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
			generateTestOutFromUTXO(earlyFailedProposalUTXO, ids.Empty, ids.Empty),
			generateTestOutFromUTXO(expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
			generateTestOutFromUTXO(expiredFailedProposalUTXO, ids.Empty, ids.Empty),
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							activeSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(activeSuccessfulProposalUTXO, ids.Empty, ids.Empty),
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, lockTxIDs, []ids.ShortID{
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
							generateTestInFromUTXO(earlyFailedProposalUTXO, []uint32{}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlyFailedProposalUTXO, ids.Empty, ids.Empty),
							generateTestOutFromUTXO(expiredFailedProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, lockTxIDs, []ids.ShortID{
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
							generateTestInFromUTXO(earlySuccessfulProposalWithBondUTXO, []uint32{}),
							// generateTestInFromUTXO(additionalBondUTXO1, []uint32{}), // missing pending validator bond
						},
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlySuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							generateTestOutFromUTXO(additionalBondUTXO1, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, lockTxIDs, []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							earlyFailedProposalUTXO, expiredFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
							generateTestOut(ctx.AVAXAssetID, proposalBondAmt, bondOwner1, ids.Empty, ids.Empty), // expiredFailedProposalUTXO with different owner
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
				expectUnlock(t, s, lockTxIDs, []ids.ShortID{
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
							generateTestInFromUTXO(earlySuccessfulProposalWithBondUTXO, []uint32{}),
							generateTestInFromUTXO(additionalBondUTXO1, []uint32{}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlySuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							// generateTestOutFromUTXO(additionalBondUTXO1, ids.Empty, ids.Empty), // missing pending validator bond
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
				expectUnlock(t, s, lockTxIDs, []ids.ShortID{
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
							generateTestInFromUTXO(earlySuccessfulProposalWithBondUTXO, []uint32{}),
							// generateTestInFromUTXO(additionalBondUTXO1, []uint32{}), // missing pending validator bond
						},
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlySuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							// generateTestOutFromUTXO(additionalBondUTXO1, ids.Empty, ids.Empty), // missing pending validator bond
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							earlyFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlyFailedProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							earlySuccessfulProposalUTXO,
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
							generateTestOutFromUTXO(expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							earlySuccessfulProposalUTXO,
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
							generateTestOutFromUTXO(expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							activeSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(activeSuccessfulProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							activeFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(activeFailedProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							activeSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(activeSuccessfulProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							activeFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(activeFailedProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							expiredFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(expiredFailedProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							earlyFailedProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlyFailedProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							expiredSuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(expiredSuccessfulProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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
						Ins: generateInsFromUTXOsWithSigIndices([]*avax.UTXO{
							earlySuccessfulProposalUTXO,
						}, []uint32{}),
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlySuccessfulProposalUTXO, ids.Empty, ids.Empty),
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
				expectUnlock(t, s, utx.ProposalIDs(), []ids.ShortID{
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

				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)
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
				expectUnlock(t, s, lockTxIDs, []ids.ShortID{
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
					Return(as.AddressStateConsortiumMember, nil)
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
					Return(as.AddressStateConsortiumMember, nil)
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

				expectConsumeUTXOs(t, s, utx.Ins)
				expectProduceUTXOs(t, s, utx.Outs, txID, 0)

				return s
			},
			utx: func(cfg *config.Config) *txs.FinishProposalsTx {
				return &txs.FinishProposalsTx{
					BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generateTestInFromUTXO(earlySuccessfulProposalWithBondUTXO, []uint32{}),
							generateTestInFromUTXO(expiredSuccessfulProposalWithBondUTXO, []uint32{}),
							generateTestInFromUTXO(additionalBondUTXO1, []uint32{}),
							generateTestInFromUTXO(additionalBondUTXO2, []uint32{}),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOutFromUTXO(earlySuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							generateTestOutFromUTXO(expiredSuccessfulProposalWithBondUTXO, ids.Empty, ids.Empty),
							generateTestOutFromUTXO(additionalBondUTXO1, ids.Empty, ids.Empty),
							generateTestOutFromUTXO(additionalBondUTXO2, ids.Empty, ids.Empty),
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }()

			env.config.BerlinPhaseTime = earlySuccessfulProposal.StartTime().Add(-1 * time.Second)

			utx := tt.utx(env.config)
			avax.SortTransferableInputs(utx.Ins)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(t, ctrl, utx, tx.ID(), env.config),
					Tx:      tx,
				},
			})
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
