// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
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
	msigKey, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)
	msigAlias := msigKey.PublicKey().Address()

	addr0 := caminoPreFundedKeys[0].Address()
	addr1 := caminoPreFundedKeys[1].Address()

	require.NoError(t, env.state.Commit())

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
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
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
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
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
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
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
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
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
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
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
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
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
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[1]},
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
		"Not signed by consortium member": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Unix() + 1),
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        nodeID,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[2]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr0)
			},
			expectedErr: errConsortiumSignatureMissing,
		},
		"Not enough sigs from msig consortium member": {
			generateArgs: func() args {
				return args{
					stakeAmount:   env.config.MinValidatorStake,
					startTime:     uint64(defaultValidateStartTime.Unix() + 1),
					endTime:       uint64(defaultValidateEndTime.Unix()),
					nodeID:        nodeID,
					rewardAddress: ids.ShortEmpty,
					shares:        reward.PercentDenominator,
					keys:          []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
					changeAddr:    ids.ShortEmpty,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &msigAlias)
				env.state.SetMultisigAlias(&multisig.Alias{
					ID: msigAlias,
					Owners: &secp256k1fx.OutputOwners{
						Threshold: 2,
						Addrs: []ids.ShortID{
							caminoPreFundedKeys[0].Address(),
							caminoPreFundedKeys[1].Address(),
						},
					},
				})
			},
			expectedErr: errConsortiumSignatureMissing,
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
			ins := make([]*avax.TransferableInput, len(tt.utxos))
			signers := make([][]*crypto.PrivateKeySECP256K1R, len(tt.utxos))
			for i, utxo := range tt.utxos {
				env.state.AddUTXO(utxo)
				ins[i] = generateTestInFromUTXO(utxo, sigIndices)
				signers[i] = inputSigners
			}

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

func TestCaminoLockedInsOrLockedOuts(t *testing.T) {
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}

	nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()

	now := time.Now()
	signers := [][]*crypto.PrivateKeySECP256K1R{{caminoPreFundedKeys[0]}}
	signers[len(signers)-1] = []*crypto.PrivateKeySECP256K1R{nodeKey}

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

			exportedOutputsTx := &txs.ExportTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins: []*avax.TransferableInput{
						generateTestIn(avaxAssetID, 10, ids.Empty, ids.Empty, sigIndices),
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
				State:   uint8(0),
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
				Validator: validator.SubnetValidator{
					Validator: validator.Validator{
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

		t.Run("RegisterNodeTx "+name, func(t *testing.T) {
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoConfig)
			env.ctx.Lock.Lock()
			defer func() {
				err := shutdownCaminoEnvironment(env)
				require.NoError(t, err)
			}()
			env.config.BanffTime = env.state.GetTimestamp()

			registerNodeTx := &txs.RegisterNodeTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    env.ctx.NetworkID,
					BlockchainID: env.ctx.ChainID,
					Ins:          tt.ins,
					Outs:         tt.outs,
				}},
				ConsortiumMemberAuth: &secp256k1fx.Input{},
			}

			executor := generateExecutor(registerNodeTx, env)

			err := executor.RegisterNodeTx(registerNodeTx)
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
		caminoConfig api.Camino
		nodeID       ids.NodeID
		nodeKey      *crypto.PrivateKeySECP256K1R
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
				generateTestUTXO(unlockedUTXOTxID, env.ctx.AVAXAssetID, defaultCaminoBalance, stakeOwners, ids.Empty, ids.Empty),
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

	signers := [][]*crypto.PrivateKeySECP256K1R{
		{preFundedKeys[0]},
	}

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}

	tests := map[string]struct {
		stateAddress  ids.ShortID
		targetAddress ids.ShortID
		txFlag        uint8
		existingState uint64
		expectedErr   error
		expectedState uint64
		remove        bool
	}{
		// Bob has Admin State, and he is trying to give himself Admin Role (again)
		"State: Admin, Flag: Admin, Add, Same Address": {
			stateAddress:  bob,
			targetAddress: bob,
			txFlag:        txs.AddressStateRoleAdmin,
			existingState: txs.AddressStateRoleAdminBit,
			expectedState: txs.AddressStateRoleAdminBit,
			remove:        false,
		},
		// Bob has KYC State, and he is trying to give himself KYC Role (again)
		"State: KYC, Flag: KYC, Add, Same Address": {
			stateAddress:  bob,
			targetAddress: bob,
			txFlag:        txs.AddressStateRoleKyc,
			existingState: txs.AddressStateRoleKycBit,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		// Bob has KYC Role, and he is trying to give himself Admin Role
		"State: KYC, Flag: Admin, Add, Same Address": {
			stateAddress:  bob,
			targetAddress: bob,
			txFlag:        txs.AddressStateRoleAdmin,
			existingState: txs.AddressStateRoleKycBit,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		// Bob has Admin State, and he is trying to give Alice Admin Role
		"State: Admin, Flag: Admin, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        txs.AddressStateRoleAdmin,
			existingState: txs.AddressStateRoleAdminBit,
			expectedState: txs.AddressStateRoleAdminBit,
			remove:        false,
		},
		// Bob has Admin State, and he is trying to give Alice KYC Role
		"State: Admin, Flag: kyc, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        txs.AddressStateRoleKyc,
			existingState: txs.AddressStateRoleAdminBit,
			expectedState: txs.AddressStateRoleKycBit,
			remove:        false,
		},
		// Bob has Admin State, and he is trying to remove from Alice the KYC Role
		"State: Admin, Flag: kyc, Remove, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        txs.AddressStateRoleKyc,
			existingState: txs.AddressStateRoleAdminBit,
			expectedState: 0,
			remove:        true,
		},
		// Bob has Admin State, and he is trying to give Alice the KYC Verified State
		"State: Admin, Flag: KYC Verified, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        txs.AddressStateKycVerified,
			existingState: txs.AddressStateRoleAdminBit,
			expectedState: txs.AddressStateKycVerifiedBit,
			remove:        false,
		},
		// Bob has Admin State, and he is trying to give Alice the KYC Expired State
		"State: Admin, Flag: KYC Expired, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        txs.AddressStateKycExpired,
			existingState: txs.AddressStateRoleAdminBit,
			expectedState: txs.AddressStateKycExpiredBit,
			remove:        false,
		},
		// Bob has Admin State, and he is trying to give Alice the Consortium State
		"State: Admin, Flag: Consortium, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        txs.AddressStateConsortium,
			existingState: txs.AddressStateRoleAdminBit,
			expectedState: txs.AddressStateConsortiumBit,
			remove:        false,
		},
		// Bob has KYC State, and he is trying to give Alice KYC Expired State
		"State: KYC, Flag: KYC Expired, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        txs.AddressStateKycExpired,
			existingState: txs.AddressStateRoleKycBit,
			expectedState: txs.AddressStateKycExpiredBit,
			remove:        false,
		},
		// Bob has KYC State, and he is trying to give Alice KYC Expired State
		"State: KYC, Flag: KYC Verified, Add, Different Address": {
			stateAddress:  bob,
			targetAddress: alice,
			txFlag:        txs.AddressStateKycVerified,
			existingState: txs.AddressStateRoleKycBit,
			expectedState: txs.AddressStateKycVerifiedBit,
			remove:        false,
		},
		// Some Address has Admin State, and he is trying to give Alice Admin Role
		"Wrong address": {
			stateAddress:  ids.GenerateTestShortID(),
			targetAddress: alice,
			txFlag:        txs.AddressStateRoleAdmin,
			existingState: txs.AddressStateRoleAdminBit,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		// An Empty Address has Admin State, and he is trying to give Alice Admin Role
		"Empty State Address": {
			stateAddress:  ids.ShortEmpty,
			targetAddress: alice,
			txFlag:        txs.AddressStateRoleAdmin,
			existingState: txs.AddressStateRoleAdminBit,
			expectedErr:   errInvalidRoles,
			remove:        false,
		},
		// Bob has Admin State, and he is trying to give Admin Role to an Empty Address
		"Empty Target Address": {
			stateAddress:  bob,
			targetAddress: ids.ShortEmpty,
			txFlag:        txs.AddressStateRoleAdmin,
			existingState: txs.AddressStateRoleAdminBit,
			expectedErr:   txs.ErrEmptyAddress,
			remove:        false,
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

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			addressStateTx := &txs.AddressStateTx{
				BaseTx:  baseTx,
				Address: tt.targetAddress,
				State:   tt.txFlag,
				Remove:  tt.remove,
			}

			tx, err := txs.NewSigned(addressStateTx, txs.Codec, signers)
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

			executor.State.SetAddressStates(tt.stateAddress, tt.existingState)

			err = addressStateTx.Visit(&executor)
			require.Equal(t, tt.expectedErr, err)

			if err == nil {
				targetStates, _ := executor.State.GetAddressStates(tt.targetAddress)
				require.Equal(t, targetStates, tt.expectedState)
			}
		})
	}
}

func TestCaminoStandardTxExecutorDepositTx(t *testing.T) {
	currentTime := time.Now()

	testDepositOffer := &deposit.Offer{
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
		caminoGenesisConf api.Camino
		utxos             []*avax.UTXO
		generateIns       func([]*avax.UTXO) []*avax.TransferableInput
		signers           [][]*crypto.PrivateKeySECP256K1R
		outs              []*avax.TransferableOutput
		depositOfferID    func(caminoEnvironment) ids.ID
		expectedErr       error
	}{
		"Wrong lockModeBondDeposit flag": {
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: false,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
			},
			utxos:          []*avax.UTXO{},
			generateIns:    noInputs,
			outs:           []*avax.TransferableOutput{},
			depositOfferID: noOffers,
			expectedErr:    errWrongLockMode,
		},
		"Stakeable ins": {
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []*deposit.Offer{{
					InterestRateNominator:   0,
					Start:                   uint64(currentTime.Add(+60 * time.Hour).Unix()),
					End:                     uint64(currentTime.Add(+60 * time.Hour).Unix()),
					MinAmount:               1,
					MinDuration:             60,
					MaxDuration:             60,
					UnlockPeriodDuration:    60,
					NoRewardsPeriodDuration: 0,
				}},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []*deposit.Offer{{
					InterestRateNominator:   0,
					Start:                   uint64(currentTime.Add(-60 * time.Hour).Unix()),
					End:                     uint64(currentTime.Add(-60 * time.Hour).Unix()),
					MinAmount:               1,
					MinDuration:             60,
					MaxDuration:             60,
					UnlockPeriodDuration:    60,
					NoRewardsPeriodDuration: 0,
				}},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []*deposit.Offer{{
					InterestRateNominator:   0,
					Start:                   uint64(currentTime.Add(-60 * time.Hour).Unix()),
					End:                     uint64(currentTime.Add(+60 * time.Hour).Unix()),
					MinAmount:               1,
					MinDuration:             100,
					MaxDuration:             100,
					UnlockPeriodDuration:    60,
					NoRewardsPeriodDuration: 40,
				}},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []*deposit.Offer{{
					InterestRateNominator:   0,
					Start:                   uint64(currentTime.Add(-60 * time.Hour).Unix()),
					End:                     uint64(currentTime.Add(+60 * time.Hour).Unix()),
					MinAmount:               1,
					MinDuration:             60,
					MaxDuration:             30,
					UnlockPeriodDuration:    60,
					NoRewardsPeriodDuration: 0,
				}},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []*deposit.Offer{{
					InterestRateNominator:   0,
					Start:                   uint64(currentTime.Add(-60 * time.Hour).Unix()),
					End:                     uint64(currentTime.Add(+60 * time.Hour).Unix()),
					MinAmount:               defaultCaminoValidatorWeight * 2,
					MinDuration:             60,
					MaxDuration:             60,
					UnlockPeriodDuration:    60,
					NoRewardsPeriodDuration: 0,
				}},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers: []*deposit.Offer{{
					InterestRateNominator:   1000 * units.MegaAvax,
					Start:                   uint64(currentTime.Add(-60 * time.Hour).Unix()),
					End:                     uint64(currentTime.Add(+60 * time.Hour).Unix()),
					MinAmount:               1,
					MinDuration:             60,
					MaxDuration:             60,
					UnlockPeriodDuration:    60,
					NoRewardsPeriodDuration: 0,
				}},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			caminoGenesisConf: api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				DepositOffers:       []*deposit.Offer{testDepositOffer},
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
			env := newCaminoEnvironment( /*postBanff*/ true, false, tt.caminoGenesisConf)
			env.ctx.Lock.Lock()
			defer func() {
				if err := shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			env.config.BanffTime = env.state.GetTimestamp()
			env.state.SetTimestamp(currentTime)

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
				DepositOfferID:  tt.depositOfferID(*env),
				DepositDuration: 60,
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
	ctx, _ := defaultCtx(nil)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	feeOwnerKey, feeOwnerAddr, feeOwner := generateKeyAndOwner(t)
	owner1Key, owner1Addr, owner1 := generateKeyAndOwner(t)
	_, _, owner2 := generateKeyAndOwner(t)
	// _, _, deposit1RewardsOwner := generateKeyAndOwner(t)
	bondTxID := ids.GenerateTestID()
	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()
	// deposit1RewardOwnerID, err := txs.GetOwnerID(deposit1RewardsOwner)
	// require.NoError(t, err)

	depositOffer := &deposit.Offer{
		MinAmount:            1,
		MinDuration:          60,
		MaxDuration:          100,
		UnlockPeriodDuration: 50,
	}
	require.NoError(t, depositOffer.SetID())
	deposit1 := &deposit.Deposit{
		Duration:       depositOffer.MinDuration,
		Amount:         defaultCaminoValidatorWeight,
		DepositOfferID: depositOffer.ID,
	}
	deposit2 := &deposit.Deposit{
		Start:          deposit1.Start,
		Duration:       depositOffer.MaxDuration,
		Amount:         defaultCaminoValidatorWeight,
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

	// depositTx1, err := txs.NewSigned(&txs.DepositTx{
	// 	BaseTx:       txs.BaseTx{BaseTx: avax.BaseTx{}},
	// 	RewardsOwner: &deposit1RewardsOwner,
	// }, txs.Codec, nil)
	// require.NoError(t, err)

	baseState := func(c *gomock.Controller) *state.MockState {
		s := state.NewMockState(c)
		// shutdown
		s.EXPECT().SetHeight(uint64(math.MaxUint64))
		s.EXPECT().Commit()
		s.EXPECT().Close()
		return s
	}

	tests := map[string]struct {
		baseState   func(c *gomock.Controller) *state.MockState
		state       func(*gomock.Controller, *txs.UnlockDepositTx, ids.ID, []*avax.UTXO) *state.MockDiff
		utx         func([]*avax.UTXO) *txs.UnlockDepositTx
		signers     [][]*crypto.PrivateKeySECP256K1R
		utxos       []*avax.UTXO
		expectedErr error
	}{
		"Stakeable ins": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				return s
			},
			utx: func(u []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: []*avax.TransferableInput{
						generateTestStakeableIn(ctx.AVAXAssetID, 1, 0, []uint32{0}),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{},
			expectedErr: locked.ErrWrongInType,
		},
		"Stakeable outs": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				return s
			},
			utx: func(u []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Outs: []*avax.TransferableOutput{
						generateTestStakeableOut(ctx.AVAXAssetID, 1, 0, feeOwner),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{},
			expectedErr: locked.ErrWrongOutType,
		},
		"Unlock bonded UTXOs": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1.StartTime())
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, bondTxID),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{owner1Key}, {owner1Key}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock deposited UTXOs but with unlocked ins": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1.StartTime())
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: []*avax.TransferableInput{
						{
							UTXOID: utxos[0].UTXOID,
							Asset:  utxos[0].Asset,
							In: &secp256k1fx.TransferInput{
								Amt:   utxos[0].Out.(avax.Amounter).Amount(),
								Input: secp256k1fx.Input{SigIndices: []uint32{0}},
							},
						},
						generateTestInFromUTXO(utxos[1], []uint32{0}),
					},
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {owner1Key}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock deposited UTXOs but with bonded ins": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1.StartTime())
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: []*avax.TransferableInput{
						{
							UTXOID: utxos[0].UTXOID,
							Asset:  utxos[0].Asset,
							In: &locked.In{
								IDs: locked.IDs{BondTxID: bondTxID},
								TransferableIn: &secp256k1fx.TransferInput{
									Amt:   utxos[0].Out.(avax.Amounter).Amount(),
									Input: secp256k1fx.Input{SigIndices: []uint32{0}},
								},
							},
						},
						generateTestInFromUTXO(utxos[1], []uint32{0}),
					},
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {owner1Key}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount, before deposit's unlock period": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1StartUnlockTime.Add(-1 * time.Second))
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount-1, owner1, depositTxID1, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {owner1Key}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount, deposit expired": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount-1, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 1, owner1, depositTxID1, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount of not owned utxos as owned, deposit is still unlocking": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner2, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount-1, owner1, depositTxID1, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {feeOwnerKey}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount, utxos and input amount mismatch, deposit is still unlocking": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: []*avax.TransferableInput{
						{
							UTXOID: utxos[0].UTXOID,
							Asset:  utxos[0].Asset,
							In: &locked.In{
								IDs: utxos[0].Out.(*locked.Out).IDs,
								TransferableIn: &secp256k1fx.TransferInput{
									Amt:   utxos[0].Out.(avax.Amounter).Amount() + 1,
									Input: secp256k1fx.Input{SigIndices: []uint32{0}},
								},
							},
						},
					},
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount-1, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 1, owner1, depositTxID1, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock some amount, deposit is still unlocking": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				unlockableAmount := deposit1.UnlockableAmount(depositOffer, uint64(deposit1HalfUnlockTime.Unix()))
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, unlockableAmount+1, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount-unlockableAmount-1, owner1, depositTxID1, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {feeOwnerKey}},
			expectedErr: errFlowCheckFailed,
		},
		"Deposit is still unlocking, 2 utxos with diff owners, consumed 1.5 utxo < unlockable, all produced as owner1": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1Expired.Add(-time.Second))
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount/2, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, deposit1.Amount/2, owner2, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount*3/4, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount-deposit1.Amount*3/4, owner2, depositTxID1, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {}, {feeOwnerKey}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock all amount of not owned utxos, deposit expired": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner2, depositTxID1, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock all amount, utxos and input amount mismatch, deposit expired": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: []*avax.TransferableInput{{
						UTXOID: utxos[0].UTXOID,
						Asset:  utxos[0].Asset,
						In: &locked.In{
							IDs: utxos[0].Out.(*locked.Out).IDs,
							TransferableIn: &secp256k1fx.TransferInput{
								Amt:   utxos[0].Out.(avax.Amounter).Amount() + 1,
								Input: secp256k1fx.Input{SigIndices: []uint32{0}},
							},
						},
					}},
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock all amount but also consume bonded utxo, deposit expired": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, 10, owner1, ids.Empty, bondTxID),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 10, owner1, ids.Empty, bondTxID),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock deposit, one expired-not-owned and one active deposit": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner2, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, deposit2.Amount, owner1, depositTxID2, ids.Empty),
				generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit2.Amount-1, owner1, depositTxID2, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {}, {feeOwnerKey}},
			expectedErr: errFlowCheckFailed,
		},
		"Unlock deposit, one expired and one active-not-owned deposit": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, deposit2.Amount, owner2, depositTxID2, ids.Empty),
				generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit2.Amount-1, owner1, depositTxID2, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {}, {feeOwnerKey}},
			expectedErr: errFlowCheckFailed,
		},
		"Producing more than consumed": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 1, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 2, owner1, ids.Empty, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}, {feeOwnerKey}},
			expectedErr: errFlowCheckFailed,
		},
		"No fee burning, inputs are unlocked": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				s.EXPECT().GetMultisigAlias(owner1Addr).Return(nil, database.ErrNotFound)
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{owner1Key}},
			expectedErr: errFlowCheckFailed,
		},
		"No fee burning inputs are deposited": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount-1, owner1, depositTxID1, ids.Empty),
					},
				}}}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{}},
			expectedErr: errFlowCheckFailed,
		},
		"OK: only burn fees": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				s.EXPECT().GetMultisigAlias(owner1Addr).Return(nil, database.ErrNotFound)
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				// state update: ins/outs/utxos
				expectConsumeUTXOs(s, utx.Ins)
				expectProduceUTXOs(s, utx.Outs, txID, 0)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
					},
				}}}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{owner1Key}, {feeOwnerKey}},
		},
		"OK: unlock all amount, deposit expired": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				// state update: deposit1
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil).Times(2)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				// s.EXPECT().GetTx(depositTxID1).Return(depositTx1, status.Committed, nil)
				// s.EXPECT().GetClaimable(deposit1RewardOwnerID).Return(&state.Claimable{}, nil)
				// s.EXPECT().SetClaimable(deposit1RewardOwnerID, &state.Claimable{})
				s.EXPECT().SetDeposit(depositTxID1, nil)
				// rewardUTXO := &avax.UTXO{
				// 	UTXOID: avax.UTXOID{
				// 		TxID:        txID,
				// 		OutputIndex: uint32(len(utx.Outs)),
				// 	},
				// 	Asset: avax.Asset{ID: ctx.AVAXAssetID},
				// 	Out: &secp256k1fx.TransferOutput{
				// 		Amt:          deposit1.TotalReward(depositOffer) - deposit1.ClaimedRewardAmount,
				// 		OutputOwners: deposit1RewardsOwner,
				// 	},
				// }
				// s.EXPECT().AddUTXO(rewardUTXO)
				// s.EXPECT().AddRewardUTXO(rewardUTXO.TxID, rewardUTXO)
				// state update: ins/outs/utxos
				expectConsumeUTXOs(s, utx.Ins)
				expectProduceUTXOs(s, utx.Outs, txID, 0)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
					},
				}}}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}},
		},
		"OK: unlock some amount, deposit is still unlocking": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				// state update: deposit1
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				unlockableAmount := deposit1.UnlockableAmount(depositOffer, uint64(deposit1HalfUnlockTime.Unix()))
				s.EXPECT().SetDeposit(depositTxID1, &deposit.Deposit{
					DepositOfferID:      deposit1.DepositOfferID,
					UnlockedAmount:      deposit1.UnlockedAmount + unlockableAmount,
					ClaimedRewardAmount: deposit1.ClaimedRewardAmount,
					Start:               deposit1.Start,
					Duration:            deposit1.Duration,
					Amount:              deposit1.Amount,
				})
				// state update: ins/outs/utxos
				expectConsumeUTXOs(s, utx.Ins)
				expectProduceUTXOs(s, utx.Outs, txID, 0)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				unlockableAmount := deposit1.UnlockableAmount(depositOffer, uint64(deposit1HalfUnlockTime.Unix()))
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, unlockableAmount, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount-unlockableAmount, owner1, depositTxID1, ids.Empty),
					},
				}}}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, {feeOwnerKey}},
		},
		"OK: unlock some amount, deposit is still unlocking, fee change to new address": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1HalfUnlockTime)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				// state update: deposit1
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				unlockableAmount := deposit1.UnlockableAmount(depositOffer, uint64(deposit1HalfUnlockTime.Unix()))
				s.EXPECT().SetDeposit(depositTxID1, &deposit.Deposit{
					DepositOfferID:      deposit1.DepositOfferID,
					UnlockedAmount:      deposit1.UnlockedAmount + unlockableAmount,
					ClaimedRewardAmount: deposit1.ClaimedRewardAmount,
					Start:               deposit1.Start,
					Duration:            deposit1.Duration,
					Amount:              deposit1.Amount,
				})
				// state update: ins/outs/utxos
				expectConsumeUTXOs(s, utx.Ins)
				expectProduceUTXOs(s, utx.Outs, txID, 0)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, defaultTxFee+10, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				unlockableAmount := deposit1.UnlockableAmount(depositOffer, uint64(deposit1HalfUnlockTime.Unix()))
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 10, owner2, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, unlockableAmount, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount-unlockableAmount, owner1, depositTxID1, ids.Empty),
					},
				}}}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, {feeOwnerKey}},
		},
		"OK: unlock deposit, one expired deposit and one active": {
			baseState: func(c *gomock.Controller) *state.MockState {
				s := baseState(c)
				s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
				// utxo handler, used in fx VerifyMultisigTransfer method for verify unlock deposit flowcheck
				return s
			},
			state: func(c *gomock.Controller, utx *txs.UnlockDepositTx, txID ids.ID, utxos []*avax.UTXO) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				// verify unlock deposit flowcheck
				expectGetUTXOsFromInputs(s, utx.Ins, utxos)
				s.EXPECT().GetTimestamp().Return(deposit1Expired)
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				s.EXPECT().GetDeposit(depositTxID2).Return(deposit2, nil)
				s.EXPECT().GetDepositOffer(deposit2.DepositOfferID).Return(depositOffer, nil)
				// state update: deposit1
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil).Times(2)
				s.EXPECT().GetDepositOffer(deposit1.DepositOfferID).Return(depositOffer, nil)
				// s.EXPECT().GetTx(depositTxID1).Return(depositTx1, status.Committed, nil)
				// s.EXPECT().GetClaimable(deposit1RewardOwnerID).Return(&state.Claimable{}, nil)
				// s.EXPECT().SetClaimable(deposit1RewardOwnerID, &state.Claimable{})
				s.EXPECT().SetDeposit(depositTxID1, nil)
				// rewardUTXO := &avax.UTXO{
				// 	UTXOID: avax.UTXOID{
				// 		TxID:        txID,
				// 		OutputIndex: uint32(len(utx.Outs)),
				// 	},
				// 	Asset: avax.Asset{ID: ctx.AVAXAssetID},
				// 	Out: &secp256k1fx.TransferOutput{
				// 		Amt:          deposit1.TotalReward(depositOffer) - deposit1.ClaimedRewardAmount,
				// 		OutputOwners: deposit1RewardsOwner,
				// 	},
				// }
				// s.EXPECT().AddUTXO(rewardUTXO)
				// s.EXPECT().AddRewardUTXO(rewardUTXO.TxID, rewardUTXO)
				// state update: deposit2
				s.EXPECT().GetDeposit(depositTxID2).Return(deposit2, nil)
				s.EXPECT().SetDeposit(depositTxID2, &deposit.Deposit{
					DepositOfferID:      deposit2.DepositOfferID,
					UnlockedAmount:      deposit2.UnlockedAmount + 1,
					ClaimedRewardAmount: deposit2.ClaimedRewardAmount,
					Start:               deposit2.Start,
					Duration:            deposit2.Duration,
					Amount:              deposit2.Amount,
				})
				// state update: ins/outs/utxos
				expectConsumeUTXOs(s, utx.Ins)
				expectProduceUTXOs(s, utx.Outs, txID, 0)
				return s
			},
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, deposit1.Amount, owner1, depositTxID1, ids.Empty),
				generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, deposit2.Amount, owner1, depositTxID2, ids.Empty),
				generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty),
			},
			utx: func(utxos []*avax.UTXO) *txs.UnlockDepositTx {
				return &txs.UnlockDepositTx{BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					Ins: generateInsFromUTXOs(utxos),
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit1.Amount, owner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, deposit2.Amount-1, owner1, depositTxID2, ids.Empty),
					},
				}}}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{}, {}, {feeOwnerKey}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(true, false, nil, caminoGenesisConf, tt.baseState(ctrl), nil)
			defer require.NoError(shutdownCaminoEnvironment(env))
			env.ctx.Lock.Lock()

			utx := tt.utx(tt.utxos)
			utx.BlockchainID = env.ctx.ChainID
			utx.NetworkID = env.ctx.NetworkID
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(err)

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(ctrl, utx, tx.ID(), tt.utxos),
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

	feeUTXO := generateTestUTXO(ids.GenerateTestID(), ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)

	depositOfferID := ids.GenerateTestID()
	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()
	claimableOwnerID1 := ids.GenerateTestID()
	claimableOwnerID2 := ids.GenerateTestID()
	timestamp := time.Now()

	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	baseState := func(c *gomock.Controller) *state.MockState {
		s := state.NewMockState(c)
		// utxo handler, used in fx VerifyMultisigTransfer method for baseTx ins verification
		s.EXPECT().GetMultisigAlias(feeOwnerAddr).Return(nil, database.ErrNotFound)
		// shutdown
		s.EXPECT().SetHeight(uint64(math.MaxUint64))
		s.EXPECT().Commit()
		s.EXPECT().Close()
		return s
	}

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins:          []*avax.TransferableInput{generateTestInFromUTXO(feeUTXO, []uint32{0})},
	}}

	tests := map[string]struct {
		state         func(*gomock.Controller, *txs.ClaimTx, ids.ID, []*avax.UTXO, []*state.Claimable) *state.MockDiff
		utx           func([]*avax.UTXO, []*state.Claimable) *txs.ClaimTx
		signers       [][]*crypto.PrivateKeySECP256K1R
		treasuryUTXOs []*avax.UTXO
		claimables    []*state.Claimable
		expectedErr   error
	}{
		"Deposit not found": {
			state: func(c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID, treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				expectVerifyLock(s, utx.Ins, []*avax.UTXO{feeUTXO})
				s.EXPECT().GetTimestamp().Return(timestamp)
				// deposit
				s.EXPECT().GetTx(depositTxID1).Return(nil, status.Unknown, database.ErrNotFound)
				return s
			},
			utx: func([]*avax.UTXO, []*state.Claimable) *txs.ClaimTx {
				return &txs.ClaimTx{
					BaseTx:              baseTx,
					DepositTxs:          []ids.ID{depositTxID1},
					DepositRewardsOwner: &secp256k1fx.OutputOwners{},
				}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{feeOwnerKey}, {}},
			expectedErr: errDepositNotFound,
		},
		"Bad deposit credential": {
			state: func(c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID, treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				expectVerifyLock(s, utx.Ins, []*avax.UTXO{feeUTXO})
				s.EXPECT().GetTimestamp().Return(timestamp)
				// deposit
				s.EXPECT().GetTx(depositTxID1).Return(
					&txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &depositRewardOwner}},
					status.Committed,
					nil,
				)
				expectVerifyMultisigPermission(s, depositRewardOwner.Addrs, nil)
				return s
			},
			utx: func([]*avax.UTXO, []*state.Claimable) *txs.ClaimTx {
				return &txs.ClaimTx{
					BaseTx:              baseTx,
					DepositTxs:          []ids.ID{depositTxID1},
					DepositRewardsOwner: &secp256k1fx.OutputOwners{},
				}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{feeOwnerKey}, {}},
			expectedErr: errDepositCredentialMissmatch,
		},
		"Bad claimable credential": {
			state: func(c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID, treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				expectVerifyLock(s, utx.Ins, []*avax.UTXO{feeUTXO})
				s.EXPECT().GetTimestamp().Return(timestamp)
				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimables[0], nil)
				expectVerifyMultisigPermission(s, claimableOwner1.Addrs, nil)
				return s
			},
			utx: func([]*avax.UTXO, []*state.Claimable) *txs.ClaimTx {
				return &txs.ClaimTx{
					BaseTx:              baseTx,
					DepositRewardsOwner: &secp256k1fx.OutputOwners{},
					ClaimableOwnerIDs:   []ids.ID{claimableOwnerID1},
					ClaimedAmount:       []uint64{1},
					ClaimableIns: []*avax.TransferableInput{
						generateTestIn(ctx.AVAXAssetID, 2, ids.Empty, ids.Empty, []uint32{}),
					},
					ClaimableOuts: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, 1, claimableOwner1, ids.Empty, ids.Empty),
					},
				}
			},
			signers:     [][]*crypto.PrivateKeySECP256K1R{{feeOwnerKey}, {}},
			claimables:  []*state.Claimable{{Owner: &claimableOwner1}},
			expectedErr: errClaimableCredentialMissmatch,
		},
		"Claimable ins not owned by treasury": {
			state: func(c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID, treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				expectVerifyLock(s, utx.Ins, []*avax.UTXO{feeUTXO})
				s.EXPECT().GetTimestamp().Return(timestamp)

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimables[0], nil)
				expectVerifyMultisigPermission(s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, nil)
				expectGetUTXOsFromInputs(s, utx.ClaimableIns, treasuryUTXOs)
				return s
			},
			utx: func(treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *txs.ClaimTx {
				return &txs.ClaimTx{
					BaseTx:              baseTx,
					DepositRewardsOwner: &secp256k1fx.OutputOwners{},
					ClaimedAmount:       []uint64{claimables[0].ValidatorReward},
					ClaimableOwnerIDs:   []ids.ID{claimableOwnerID1},
					ClaimableIns: []*avax.TransferableInput{
						generateTestInFromUTXO(treasuryUTXOs[0], []uint32{0}),
					},
					ClaimableOuts: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, claimables[0].ValidatorReward, claimableOwner1, ids.Empty, ids.Empty),
					},
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{feeOwnerKey}, {claimableOwnerKey1}},
			treasuryUTXOs: []*avax.UTXO{{
				Asset: avax.Asset{ID: ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt:          10,
					OutputOwners: depositRewardOwner, // not treasury owner
				},
			}},
			claimables: []*state.Claimable{{
				Owner:           &claimableOwner1,
				ValidatorReward: 10,
			}},
			expectedErr: errClaimingNonTreasuryUTXO,
		},
		"Claimed more than available": {
			state: func(c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID, treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				expectVerifyLock(s, utx.Ins, []*avax.UTXO{feeUTXO})
				s.EXPECT().GetTimestamp().Return(timestamp)

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimables[0], nil)
				expectVerifyMultisigPermission(s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, &state.Claimable{
					Owner:           claimables[0].Owner,
					ValidatorReward: claimables[0].ValidatorReward - utx.ClaimedAmount[0],
				})
				s.EXPECT().GetClaimable(claimableOwnerID2).Return(claimables[1], nil)
				expectVerifyMultisigPermission(s, claimableOwner2.Addrs, nil)
				return s
			},
			utx: func(treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *txs.ClaimTx {
				return &txs.ClaimTx{
					BaseTx:              baseTx,
					DepositRewardsOwner: &secp256k1fx.OutputOwners{},
					ClaimableOwnerIDs:   []ids.ID{claimableOwnerID1, claimableOwnerID2},
					ClaimedAmount:       []uint64{claimables[0].ValidatorReward - 1, claimables[1].ValidatorReward + 1},
					ClaimableIns: []*avax.TransferableInput{
						generateTestInFromUTXO(treasuryUTXOs[0], []uint32{0}),
					},
					ClaimableOuts: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, claimables[0].ValidatorReward-1, claimableOwner1, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, claimables[1].ValidatorReward+1, claimableOwner2, ids.Empty, ids.Empty),
					},
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{feeOwnerKey}, {claimableOwnerKey1, claimableOwnerKey2}},
			treasuryUTXOs: []*avax.UTXO{{
				Asset: avax.Asset{ID: ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt:          20,
					OutputOwners: *treasury.Owner,
				},
			}},
			claimables: []*state.Claimable{
				{
					Owner:           &claimableOwner1,
					ValidatorReward: 10,
				},
				{
					Owner:           &claimableOwner2,
					ValidatorReward: 10,
				},
			},
			expectedErr: errWrongClaimedAmount,
		},
		"OK, claimable and 2 deposits": {
			state: func(c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID, treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				expectVerifyLock(s, utx.Ins, []*avax.UTXO{feeUTXO})
				s.EXPECT().GetTimestamp().Return(timestamp)
				s.EXPECT().DeleteUTXO(feeUTXO.InputID())

				// deposit1
				s.EXPECT().GetTx(depositTxID1).Return(
					&txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &depositRewardOwner}},
					status.Committed,
					nil,
				)
				expectVerifyMultisigPermission(s, depositRewardOwner.Addrs, nil)
				deposit1 := &deposit.Deposit{
					DepositOfferID: depositOfferID,
					Start:          uint64(timestamp.Unix()) - 365*24*60*60/2, // 0.5 year ago
					Duration:       365 * 24 * 60 * 60,                        // 1 year
					Amount:         10,
				}
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(depositOfferID).Return(&deposit.Offer{
					InterestRateNominator: 1_000_000, // 100%
				}, nil)
				claimedRewardAmount := uint64(5) // expected claimable reward amount
				depositRewardUTXO1 := &avax.UTXO{
					UTXOID: avax.UTXOID{
						TxID:        txID,
						OutputIndex: uint32(len(utx.Outs) + len(utx.ClaimableOuts)),
					},
					Asset: avax.Asset{ID: ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          claimedRewardAmount,
						OutputOwners: depositRewardOwner,
					},
				}
				s.EXPECT().AddUTXO(depositRewardUTXO1)
				s.EXPECT().AddRewardUTXO(depositTxID1, depositRewardUTXO1)
				s.EXPECT().SetDeposit(depositTxID1, &deposit.Deposit{
					DepositOfferID:      deposit1.DepositOfferID,
					UnlockedAmount:      deposit1.UnlockedAmount,
					ClaimedRewardAmount: deposit1.ClaimedRewardAmount + claimedRewardAmount,
					Start:               deposit1.Start,
					Duration:            deposit1.Duration,
					Amount:              deposit1.Amount,
				})

				// deposit2
				s.EXPECT().GetTx(depositTxID2).Return(
					&txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &depositRewardOwner}},
					status.Committed,
					nil,
				)
				expectVerifyMultisigPermission(s, depositRewardOwner.Addrs, nil)
				deposit2 := &deposit.Deposit{
					DepositOfferID: depositOfferID,
					Start:          uint64(timestamp.Unix()) - 365*24*60*60/2, // 0.5 year ago
					Duration:       365 * 24 * 60 * 60,                        // 1 year
					Amount:         10,
				}
				s.EXPECT().GetDeposit(depositTxID2).Return(deposit2, nil)
				s.EXPECT().GetDepositOffer(depositOfferID).Return(&deposit.Offer{
					InterestRateNominator: 1_000_000, // 100%
				}, nil)
				claimedRewardAmount = 5 // expected claimable reward amount
				depositRewardUTXO2 := &avax.UTXO{
					UTXOID: avax.UTXOID{
						TxID:        txID,
						OutputIndex: uint32(len(utx.Outs)+len(utx.ClaimableOuts)) + 1,
					},
					Asset: avax.Asset{ID: ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          claimedRewardAmount,
						OutputOwners: depositRewardOwner,
					},
				}
				s.EXPECT().AddUTXO(depositRewardUTXO2)
				s.EXPECT().AddRewardUTXO(depositTxID2, depositRewardUTXO2)
				s.EXPECT().SetDeposit(depositTxID2, &deposit.Deposit{
					DepositOfferID:      deposit2.DepositOfferID,
					UnlockedAmount:      deposit2.UnlockedAmount,
					ClaimedRewardAmount: deposit2.ClaimedRewardAmount + claimedRewardAmount,
					Start:               deposit2.Start,
					Duration:            deposit2.Duration,
					Amount:              deposit2.Amount,
				})

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimables[0], nil)
				expectVerifyMultisigPermission(s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, nil)
				expectVerifyLock(s, utx.ClaimableIns, treasuryUTXOs)
				s.EXPECT().DeleteUTXO(treasuryUTXOs[0].InputID())
				expectProduceUTXOs(s, utx.ClaimableOuts, txID, len(utx.Outs))
				return s
			},
			utx: func(treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *txs.ClaimTx {
				utxoAmt := treasuryUTXOs[0].Out.(*secp256k1fx.TransferOutput).Amt
				claimedAmt := claimables[0].ValidatorReward + claimables[0].DepositReward
				return &txs.ClaimTx{
					BaseTx:              baseTx,
					DepositTxs:          []ids.ID{depositTxID1, depositTxID2},
					DepositRewardsOwner: &secp256k1fx.OutputOwners{},
					ClaimableOwnerIDs:   []ids.ID{claimableOwnerID1},
					ClaimedAmount:       []uint64{claimedAmt},
					ClaimableIns: []*avax.TransferableInput{
						generateTestInFromUTXO(treasuryUTXOs[0], []uint32{0}),
					},
					ClaimableOuts: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, utxoAmt-claimedAmt, *treasury.Owner, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, claimedAmt, claimableOwner1, ids.Empty, ids.Empty),
					},
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{feeOwnerKey}, {depositRewardOwnerKey, claimableOwnerKey1}},
			treasuryUTXOs: []*avax.UTXO{{
				Asset: avax.Asset{ID: ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt:          50,
					OutputOwners: *treasury.Owner,
				},
			}},
			claimables: []*state.Claimable{{
				Owner:           &claimableOwner1,
				ValidatorReward: 10,
				DepositReward:   20,
			}},
		},
		"OK, deposit with new DepositRewardsOwner, non-zero already claimed reward and no rewards period": {
			state: func(c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID, treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				expectVerifyLock(s, utx.Ins, []*avax.UTXO{feeUTXO})
				s.EXPECT().GetTimestamp().Return(timestamp)
				s.EXPECT().DeleteUTXO(feeUTXO.InputID())

				// deposit
				s.EXPECT().GetTx(depositTxID1).Return(
					&txs.Tx{Unsigned: &txs.DepositTx{RewardsOwner: &depositRewardOwner}},
					status.Committed,
					nil,
				)
				expectVerifyMultisigPermission(s, depositRewardOwner.Addrs, nil)
				deposit1 := &deposit.Deposit{
					DepositOfferID:      depositOfferID,
					Start:               uint64(timestamp.Unix()) - 365*24*60*60/12*6, // 6 month
					Duration:            365 * 24 * 60 * 60 / 12 * 14,                 // 14 month
					Amount:              10,
					ClaimedRewardAmount: 1,
				}
				s.EXPECT().GetDeposit(depositTxID1).Return(deposit1, nil)
				s.EXPECT().GetDepositOffer(depositOfferID).Return(&deposit.Offer{
					NoRewardsPeriodDuration: 365 * 24 * 60 * 60 / 12 * 2, // 2 month
					InterestRateNominator:   1_000_000,                   // 100%
				}, nil)
				claimedRewardAmount := uint64(4) // expected claimable reward amount: 10 * (6m / (14m - 2m)) - 1 = 10 * 0.5 - 1 = 5 - 1 = 4
				depositRewardUTXO := &avax.UTXO{
					UTXOID: avax.UTXOID{
						TxID:        txID,
						OutputIndex: uint32(len(utx.Outs) + len(utx.ClaimableOuts)),
					},
					Asset: avax.Asset{ID: ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          claimedRewardAmount,
						OutputOwners: feeOwner, // not depositTx.RewardsOwner
					},
				}
				s.EXPECT().AddUTXO(depositRewardUTXO)
				s.EXPECT().AddRewardUTXO(depositTxID1, depositRewardUTXO)
				s.EXPECT().SetDeposit(depositTxID1, &deposit.Deposit{
					DepositOfferID:      deposit1.DepositOfferID,
					UnlockedAmount:      deposit1.UnlockedAmount,
					ClaimedRewardAmount: deposit1.ClaimedRewardAmount + claimedRewardAmount,
					Start:               deposit1.Start,
					Duration:            deposit1.Duration,
					Amount:              deposit1.Amount,
				})
				return s
			},
			utx: func([]*avax.UTXO, []*state.Claimable) *txs.ClaimTx {
				return &txs.ClaimTx{
					BaseTx:              baseTx,
					DepositTxs:          []ids.ID{depositTxID1},
					DepositRewardsOwner: &feeOwner, // not depositTx.RewardsOwner
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{feeOwnerKey}, {depositRewardOwnerKey}},
		},
		"OK, partial claim": {
			state: func(c *gomock.Controller, utx *txs.ClaimTx, txID ids.ID, treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *state.MockDiff {
				s := state.NewMockDiff(c)
				// common checks and fee
				s.EXPECT().CaminoConfig().Return(&state.CaminoConfig{LockModeBondDeposit: true}, nil)
				expectVerifyLock(s, utx.Ins, []*avax.UTXO{feeUTXO})
				s.EXPECT().GetTimestamp().Return(timestamp)
				s.EXPECT().DeleteUTXO(feeUTXO.InputID())

				// claimable
				s.EXPECT().GetClaimable(claimableOwnerID1).Return(claimables[0], nil)
				expectVerifyMultisigPermission(s, claimableOwner1.Addrs, nil)
				s.EXPECT().SetClaimable(claimableOwnerID1, &state.Claimable{
					Owner:         claimables[0].Owner,
					DepositReward: claimables[0].ValidatorReward + claimables[0].DepositReward - utx.ClaimedAmount[0],
				})
				expectVerifyLock(s, utx.ClaimableIns, treasuryUTXOs)
				s.EXPECT().DeleteUTXO(treasuryUTXOs[0].InputID())
				expectProduceUTXOs(s, utx.ClaimableOuts, txID, len(utx.Outs))
				return s
			},
			utx: func(treasuryUTXOs []*avax.UTXO, claimables []*state.Claimable) *txs.ClaimTx {
				utxoAmt := treasuryUTXOs[0].Out.(*secp256k1fx.TransferOutput).Amt
				claimedAmt := claimables[0].ValidatorReward + claimables[0].DepositReward/2
				return &txs.ClaimTx{
					BaseTx:              baseTx,
					DepositRewardsOwner: &secp256k1fx.OutputOwners{},
					ClaimableOwnerIDs:   []ids.ID{claimableOwnerID1},
					ClaimedAmount:       []uint64{claimedAmt},
					ClaimableIns: []*avax.TransferableInput{
						generateTestInFromUTXO(treasuryUTXOs[0], []uint32{0}),
					},
					ClaimableOuts: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, utxoAmt-claimedAmt, *treasury.Owner, ids.Empty, ids.Empty),
						generateTestOut(ctx.AVAXAssetID, claimedAmt, claimableOwner1, ids.Empty, ids.Empty),
					},
				}
			},
			signers: [][]*crypto.PrivateKeySECP256K1R{{feeOwnerKey}, {depositRewardOwnerKey, claimableOwnerKey1}},
			treasuryUTXOs: []*avax.UTXO{{
				Asset: avax.Asset{ID: ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt:          50,
					OutputOwners: *treasury.Owner,
				},
			}},
			claimables: []*state.Claimable{{
				Owner:           &claimableOwner1,
				ValidatorReward: 10,
				DepositReward:   20,
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks( /*postBanff*/ true, false, nil, caminoGenesisConf, baseState(ctrl), nil)
			defer require.NoError(shutdownCaminoEnvironment(env))
			env.ctx.Lock.Lock()

			// ensuring that ins and outs from test case are sorted, signing tx

			utx := tt.utx(tt.treasuryUTXOs, tt.claimables)

			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			avax.SortTransferableInputsWithSigners(utx.ClaimableIns, tt.signers)
			avax.SortTransferableOutputs(utx.ClaimableOuts, txs.Codec)

			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(err)

			// testing

			err = tx.Unsigned.Visit(&CaminoStandardTxExecutor{
				StandardTxExecutor{
					Backend: &env.backend,
					State:   tt.state(ctrl, utx, tx.ID(), tt.treasuryUTXOs, tt.claimables),
					Tx:      tx,
				},
			})
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func TestCaminoStandardTxExecutorRegisterNodeTx(t *testing.T) {
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
	testKey, err := testKeyfactory.NewPrivateKey()
	require.NoError(t, err)

	factory := crypto.FactorySECP256K1R{}
	key, err := factory.NewPrivateKey()
	require.NoError(t, err)
	nodeID := key.PublicKey().Address()
	newNodeKey, ok := key.(*crypto.PrivateKeySECP256K1R)
	require.True(t, ok)
	newNodeID := ids.NodeID(nodeID)

	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{testKey.PublicKey().Address()},
	}

	type args struct {
		oldNodeID               ids.NodeID
		newNodeID               ids.NodeID
		consortiumMemberAddress ids.ShortID
		keys                    []*crypto.PrivateKeySECP256K1R
		change                  *secp256k1fx.OutputOwners
	}

	linkNode := func(addr ids.ShortID, node ids.NodeID) {
		link := ids.ShortID(node)
		env.state.SetShortIDLink(addr, state.ShortLinkKeyRegisterNode, &link)
		env.state.SetShortIDLink(link, state.ShortLinkKeyRegisterNode, &addr)
	}

	unlinkNode := func(addr ids.ShortID, node ids.NodeID) {
		env.state.SetShortIDLink(addr, state.ShortLinkKeyRegisterNode, nil)
		env.state.SetShortIDLink(ids.ShortID(node), state.ShortLinkKeyRegisterNode, nil)
	}

	_, testNodeID := nodeid.GenerateCaminoNodeKeyAndID()

	tests := map[string]struct {
		generateArgs   func() args
		preExecute     func(*testing.T, *txs.Tx)
		expectedErr    error
		expectedNodeID ids.NodeID
	}{
		"not consortium member": {
			generateArgs: func() args {
				return args{
					oldNodeID:               ids.EmptyNodeID,
					newNodeID:               caminoPreFundedNodeIDs[0],
					consortiumMemberAddress: caminoPreFundedKeys[0].PublicKey().Address(),
					keys:                    []*crypto.PrivateKeySECP256K1R{caminoPreFundedNodeKeys[0], caminoPreFundedKeys[0]},
					change:                  &outputOwners,
				}
			},
			expectedErr: errNotConsortiumMember,
		},
		"addr has already registered node": {
			generateArgs: func() args {
				return args{
					oldNodeID:               ids.EmptyNodeID,
					newNodeID:               caminoPreFundedNodeIDs[0],
					consortiumMemberAddress: caminoPreFundedKeys[4].PublicKey().Address(),
					keys:                    []*crypto.PrivateKeySECP256K1R{caminoPreFundedNodeKeys[0], caminoPreFundedKeys[4]},
					change:                  &outputOwners,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetAddressStates(caminoPreFundedKeys[4].Address(), txs.AddressStateConsortiumBit)
				linkNode(caminoPreFundedKeys[4].Address(), newNodeID)
			},
			expectedErr: errConsortiumMemberHasNode,
		},
		"addr is consortium member and changes node in current validator's set": {
			generateArgs: func() args {
				return args{
					oldNodeID:               caminoPreFundedNodeIDs[4],
					newNodeID:               newNodeID,
					consortiumMemberAddress: caminoPreFundedKeys[4].PublicKey().Address(),
					keys:                    []*crypto.PrivateKeySECP256K1R{newNodeKey, caminoPreFundedKeys[4]},
					change:                  &outputOwners,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetAddressStates(caminoPreFundedKeys[4].Address(), txs.AddressStateConsortiumBit)
				linkNode(caminoPreFundedKeys[4].Address(), caminoPreFundedNodeIDs[4])
			},
			expectedErr: errValidatorExists,
		},
		"addr is consortium member and changes node in pending validator's set": {
			generateArgs: func() args {
				return args{
					oldNodeID:               caminoPreFundedNodeIDs[3],
					newNodeID:               newNodeID,
					consortiumMemberAddress: caminoPreFundedKeys[4].PublicKey().Address(),
					keys:                    []*crypto.PrivateKeySECP256K1R{newNodeKey, caminoPreFundedKeys[4]},
					change:                  &outputOwners,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetAddressStates(caminoPreFundedKeys[4].Address(), txs.AddressStateConsortiumBit)
				linkNode(caminoPreFundedKeys[4].Address(), caminoPreFundedNodeIDs[3])
				staker, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, caminoPreFundedNodeIDs[3])
				require.NoError(t, err)
				env.state.DeleteCurrentValidator(staker)
				env.state.PutPendingValidator(staker)
			},
			expectedErr: errValidatorExists,
		},
		"addr is consortium member and changes node in deferred validator's set": {
			generateArgs: func() args {
				return args{
					oldNodeID:               caminoPreFundedNodeIDs[2],
					newNodeID:               newNodeID,
					consortiumMemberAddress: caminoPreFundedKeys[4].PublicKey().Address(),
					keys:                    []*crypto.PrivateKeySECP256K1R{newNodeKey, caminoPreFundedKeys[4]},
					change:                  &outputOwners,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetAddressStates(caminoPreFundedKeys[4].Address(), txs.AddressStateConsortiumBit)
				linkNode(caminoPreFundedKeys[4].Address(), caminoPreFundedNodeIDs[2])
				staker, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, caminoPreFundedNodeIDs[2])
				require.NoError(t, err)
				env.state.DeleteCurrentValidator(staker)
				env.state.PutDeferredValidator(staker)
			},
			expectedErr: errValidatorExists,
		},
		"Happy path - addr is consortium member and changes registered node (not in any validator's set)": {
			generateArgs: func() args {
				return args{
					oldNodeID:               testNodeID,
					newNodeID:               newNodeID,
					consortiumMemberAddress: caminoPreFundedKeys[4].PublicKey().Address(),
					keys:                    []*crypto.PrivateKeySECP256K1R{newNodeKey, caminoPreFundedKeys[4]},
					change:                  &outputOwners,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetAddressStates(caminoPreFundedKeys[4].Address(), txs.AddressStateConsortiumBit)
				linkNode(caminoPreFundedKeys[4].Address(), testNodeID)
			},
			expectedNodeID: newNodeID,
		},
		"Happy path - addr is consortium member and has not yet registered a node": {
			generateArgs: func() args {
				return args{
					oldNodeID:               ids.EmptyNodeID,
					newNodeID:               newNodeID,
					consortiumMemberAddress: caminoPreFundedKeys[4].PublicKey().Address(),
					keys:                    []*crypto.PrivateKeySECP256K1R{newNodeKey, caminoPreFundedKeys[4]},
					change:                  &outputOwners,
				}
			},
			preExecute: func(t *testing.T, tx *txs.Tx) {
				env.state.SetAddressStates(caminoPreFundedKeys[4].Address(), txs.AddressStateConsortiumBit)
				unlinkNode(caminoPreFundedKeys[4].Address(), newNodeID)
			},
			expectedNodeID: newNodeID,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			args := tt.generateArgs()
			tx, err := env.txBuilder.NewRegisterNodeTx(
				args.oldNodeID, args.newNodeID, args.consortiumMemberAddress, args.keys, args.change)
			require.NoError(t, err)

			if tt.preExecute != nil {
				tt.preExecute(t, tx)
			}

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

			if tt.expectedNodeID != ids.EmptyNodeID {
				registeredNode, err := onAcceptState.GetShortIDLink(args.consortiumMemberAddress, state.ShortLinkKeyRegisterNode)
				require.NoError(t, err)
				require.Equal(t, tt.expectedNodeID, ids.NodeID(registeredNode))
			}
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

	baseState := func(c *gomock.Controller) *state.MockState {
		s := state.NewMockState(c)
		// shutdown
		s.EXPECT().SetHeight(uint64(math.MaxUint64))
		s.EXPECT().Commit()
		s.EXPECT().Close()
		return s
	}

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
		baseState              func(*gomock.Controller) *state.MockState
		state                  func(*gomock.Controller, *txs.RewardsImportTx, ids.ID) *state.MockDiff
		sharedMemory           func(*testing.T, *gomock.Controller, []*avax.TimedUTXO) *atomic.MockSharedMemory
		utx                    func([]*avax.TimedUTXO) *txs.RewardsImportTx
		signers                [][]*crypto.PrivateKeySECP256K1R
		utxos                  []*avax.TimedUTXO // sorted by txID
		expectedAtomicInputs   func([]*avax.TimedUTXO) set.Set[ids.ID]
		expectedAtomicRequests func([]*avax.TimedUTXO) map[ids.ID]*atomic.Requests
		expectedErr            error
	}{
		"Imported inputs don't contain all reward utxos that are ready to be imported": {
			baseState: baseState,
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
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 2, *treasury.Owner, ids.Empty, ids.Empty),
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
			baseState: baseState,
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
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					},
				}}}
			},
			utxos: []*avax.TimedUTXO{{
				UTXO:      *generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
				Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
			}},
			expectedErr: errImportedUTXOMissmatch,
		},
		"Input & utxo amount missmatch": {
			baseState: baseState,
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
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 2, *treasury.Owner, ids.Empty, ids.Empty),
					},
				}}}
			},
			utxos: []*avax.TimedUTXO{{
				UTXO:      *generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
				Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
			}},
			expectedErr: errInputAmountMissmatch,
		},
		"OK": {
			baseState: baseState,
			state: func(c *gomock.Controller, utx *txs.RewardsImportTx, txID ids.ID) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().CaminoConfig().Return(caminoStateConf, nil)
				s.EXPECT().GetTimestamp().Return(blockTime)

				nodeID1 := ids.GenerateTestNodeID()
				nodeID2 := ids.GenerateTestNodeID()
				nodeID3 := ids.GenerateTestNodeID()
				nodeID4 := ids.GenerateTestNodeID()
				_, validatorAddr1, validatorOwner1 := generateKeyAndOwner(t)
				_, validatorAddr2, validatorOwner2 := generateKeyAndOwner(t)
				_, validatorAddr4, validatorOwner4 := generateKeyAndOwner(t)

				currentStakerIterator := state.NewMockStakerIterator(c)
				currentStakerIterator.EXPECT().Next().Return(true)
				currentStakerIterator.EXPECT().Value().Return(&state.Staker{
					NodeID:   nodeID1,
					SubnetID: constants.PrimaryNetworkID,
				})
				currentStakerIterator.EXPECT().Next().Return(true)
				currentStakerIterator.EXPECT().Value().Return(&state.Staker{
					NodeID:   nodeID2,
					SubnetID: constants.PrimaryNetworkID,
				})
				currentStakerIterator.EXPECT().Next().Return(true)
				currentStakerIterator.EXPECT().Value().Return(&state.Staker{
					NodeID:   nodeID3,
					SubnetID: ids.GenerateTestID(),
				})
				currentStakerIterator.EXPECT().Next().Return(true)
				currentStakerIterator.EXPECT().Value().Return(&state.Staker{
					NodeID:   nodeID4,
					SubnetID: constants.PrimaryNetworkID,
				})
				currentStakerIterator.EXPECT().Next().Return(false)
				currentStakerIterator.EXPECT().Release()

				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIterator, nil)
				s.EXPECT().GetShortIDLink(ids.ShortID(nodeID1), state.ShortLinkKeyRegisterNode).Return(validatorAddr1, nil)
				s.EXPECT().GetShortIDLink(ids.ShortID(nodeID2), state.ShortLinkKeyRegisterNode).Return(validatorAddr2, nil)
				s.EXPECT().GetShortIDLink(ids.ShortID(nodeID4), state.ShortLinkKeyRegisterNode).Return(validatorAddr4, nil)
				s.EXPECT().GetNotDistributedValidatorReward().Return(uint64(1), nil) // old
				s.EXPECT().SetNotDistributedValidatorReward(uint64(2))               // new
				validatorOwnerID1, err := txs.GetOwnerID(&validatorOwner1)
				require.NoError(t, err)
				validatorOwnerID2, err := txs.GetOwnerID(&validatorOwner2)
				require.NoError(t, err)
				validatorOwnerID4, err := txs.GetOwnerID(&validatorOwner4)
				require.NoError(t, err)

				s.EXPECT().GetClaimable(validatorOwnerID1).Return(&state.Claimable{
					Owner:           &validatorOwner1,
					ValidatorReward: 10,
					DepositReward:   100,
				}, nil)
				s.EXPECT().GetClaimable(validatorOwnerID2).Return(&state.Claimable{
					Owner:           &validatorOwner2,
					ValidatorReward: 20,
					DepositReward:   200,
				}, nil)
				s.EXPECT().GetClaimable(validatorOwnerID4).Return(nil, database.ErrNotFound)

				s.EXPECT().SetClaimable(validatorOwnerID1, &state.Claimable{
					Owner:           &validatorOwner1,
					ValidatorReward: 11,
					DepositReward:   100,
				})
				s.EXPECT().SetClaimable(validatorOwnerID2, &state.Claimable{
					Owner:           &validatorOwner2,
					ValidatorReward: 21,
					DepositReward:   200,
				})
				s.EXPECT().SetClaimable(validatorOwnerID4, &state.Claimable{
					Owner:           &validatorOwner4,
					ValidatorReward: 1,
				})

				s.EXPECT().AddUTXO(&avax.UTXO{
					UTXOID: avax.UTXOID{TxID: txID},
					Asset:  utx.Outs[0].Asset,
					Out:    utx.Outs[0].Out,
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
					Outs: []*avax.TransferableOutput{
						generateTestOut(ctx.AVAXAssetID, 4, *treasury.Owner, ids.Empty, ids.Empty),
					},
				}}}
			},
			utxos: []*avax.TimedUTXO{
				{
					UTXO:      *generateTestUTXO(ids.ID{1}, ctx.AVAXAssetID, 3, *treasury.Owner, ids.Empty, ids.Empty),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
					UTXO: *generateTestUTXO(ids.ID{2}, ctx.AVAXAssetID, 5, *treasury.Owner, ids.Empty, ids.Empty),
				},
				{
					UTXO:      *generateTestUTXO(ids.ID{3}, ctx.AVAXAssetID, 1, *treasury.Owner, ids.Empty, ids.Empty),
					Timestamp: uint64(blockTime.Unix()) - atomic.SharedMemorySyncBound,
				},
				{
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
			env := newCaminoEnvironmentWithMocks(
				true,
				false,
				nil,
				caminoGenesisConf,
				tt.baseState(ctrl),
				tt.sharedMemory(t, ctrl, tt.utxos),
			)
			defer require.NoError(shutdownCaminoEnvironment(env))
			env.ctx.Lock.Lock()

			utx := tt.utx(tt.utxos)
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

func TestCaminoStandardTxExecutorSuspendValidator(t *testing.T) {
	consortiumMemberAddress := caminoPreFundedKeys[0].Address()
	consortiumMemberKey := caminoPreFundedKeys[0]
	nodeID := caminoPreFundedNodeIDs[0]
	outputOwners := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{consortiumMemberAddress},
	}

	type args struct {
		address    ids.ShortID
		remove     bool
		keys       []*crypto.PrivateKeySECP256K1R
		changeAddr *secp256k1fx.OutputOwners
	}
	tests := map[string]struct {
		generateArgs func() args
		preExecute   func(*testing.T, *txs.Tx, state.State)
		expectedErr  error
		assert       func(*testing.T)
	}{
		"Happy path set state to deferred": {
			generateArgs: func() args {
				return args{
					address:    consortiumMemberAddress,
					keys:       []*crypto.PrivateKeySECP256K1R{consortiumMemberKey},
					changeAddr: outputOwners,
				}
			},
			preExecute:  func(t *testing.T, tx *txs.Tx, state state.State) {},
			expectedErr: nil,
		},
		"Happy path set state to active": {
			generateArgs: func() args {
				return args{
					address:    consortiumMemberAddress,
					remove:     true,
					keys:       []*crypto.PrivateKeySECP256K1R{consortiumMemberKey},
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
					keys:       []*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
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
				InitialAdmin:        consortiumMemberAddress,
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
				txs.AddressStateNodeDeferred,
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
