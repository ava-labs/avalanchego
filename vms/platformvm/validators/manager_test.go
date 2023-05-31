// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	dbmanager "github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	defaultTxFee              = uint64(100)
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour
	defaultMinValidatorStake  = 5 * units.MilliAvax
	defaultBalance            = 100 * defaultMinValidatorStake
)

// AVAX asset ID in tests
var (
	defaultRewardConfig = reward.Config{
		MaxConsumptionRate: .12 * reward.PercentDenominator,
		MinConsumptionRate: .10 * reward.PercentDenominator,
		MintingPeriod:      365 * 24 * time.Hour,
		SupplyCap:          720 * units.MegaAvax,
	}

	defaultGenesisTime = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	preFundedKeys      = secp256k1.TestKeys()
	avaxAssetID        = ids.ID{'y', 'e', 'e', 't'}
	xChainID           = ids.Empty.Prefix(0)
	cChainID           = ids.Empty.Prefix(1)
)

func TestVM_GetValidatorSet(t *testing.T) {
	// Populate the validator set to use below
	var (
		numVdrs       = 4
		vdrBaseWeight = uint64(1_000)
		vdrs          []*validators.Validator
	)

	for i := 0; i < numVdrs; i++ {
		sk, err := bls.NewSecretKey()
		require.NoError(t, err)

		vdrs = append(vdrs, &validators.Validator{
			NodeID:    ids.GenerateTestNodeID(),
			PublicKey: bls.PublicFromSecretKey(sk),
			Weight:    vdrBaseWeight + uint64(i),
		})
	}

	type test struct {
		name string
		// Height we're getting the diff at
		height             uint64
		lastAcceptedHeight uint64
		subnetID           ids.ID
		// Validator sets at tip
		currentPrimaryNetworkValidators []*validators.Validator
		currentSubnetValidators         []*validators.Validator
		// Diff at tip, block before tip, etc.
		// This must have [lastAcceptedHeight] - [height] elements
		weightDiffs []map[ids.NodeID]*state.ValidatorWeightDiff
		// Diff at tip, block before tip, etc.
		// This must have [lastAcceptedHeight] - [height] elements
		pkDiffs        []map[ids.NodeID]*bls.PublicKey
		expectedVdrSet map[ids.NodeID]*validators.GetValidatorOutput
		expectedErr    error
	}

	tests := []test{
		{
			name:               "after tip",
			height:             1,
			lastAcceptedHeight: 0,
			expectedVdrSet:     map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedErr:        database.ErrNotFound,
		},
		{
			name:               "at tip",
			height:             1,
			lastAcceptedHeight: 1,
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
			},
			currentSubnetValidators: []*validators.Validator{
				copySubnetValidator(vdrs[0]),
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "1 before tip",
			height:             2,
			lastAcceptedHeight: 3,
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
				copyPrimaryValidator(vdrs[1]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(vdrs[0]),
				copySubnetValidator(vdrs[1]),
			},
			weightDiffs: []map[ids.NodeID]*state.ValidatorWeightDiff{
				{
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: true,
						Amount:   vdrs[2].Weight,
					},
				},
			},
			pkDiffs: []map[ids.NodeID]*bls.PublicKey{
				{
					vdrs[2].NodeID: vdrs[2].PublicKey,
				},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight + 1,
				},
				vdrs[1].NodeID: {
					NodeID:    vdrs[1].NodeID,
					PublicKey: vdrs[1].PublicKey,
					Weight:    vdrs[1].Weight - 1,
				},
				vdrs[2].NodeID: {
					NodeID:    vdrs[2].NodeID,
					PublicKey: vdrs[2].PublicKey,
					Weight:    vdrs[2].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "2 before tip",
			height:             3,
			lastAcceptedHeight: 5,
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
				copyPrimaryValidator(vdrs[1]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(vdrs[0]),
				copySubnetValidator(vdrs[1]),
			},
			weightDiffs: []map[ids.NodeID]*state.ValidatorWeightDiff{
				{
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: true,
						Amount:   vdrs[2].Weight,
					},
				},
				{
					// At the block before tip vdrs[0] lost weight, vdrs[1] gained weight,
					// vdrs[2] joined
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: false,
						Amount:   vdrs[2].Weight,
					},
				},
			},
			pkDiffs: []map[ids.NodeID]*bls.PublicKey{
				{
					vdrs[2].NodeID: vdrs[2].PublicKey,
				},
				{},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight + 2,
				},
				vdrs[1].NodeID: {
					NodeID:    vdrs[1].NodeID,
					PublicKey: vdrs[1].PublicKey,
					Weight:    vdrs[1].Weight - 2,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "1 before tip; nil public key",
			height:             4,
			lastAcceptedHeight: 5,
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
				copyPrimaryValidator(vdrs[1]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(vdrs[0]),
				copySubnetValidator(vdrs[1]),
			},
			weightDiffs: []map[ids.NodeID]*state.ValidatorWeightDiff{
				{
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: true,
						Amount:   vdrs[2].Weight,
					},
				},
			},
			pkDiffs: []map[ids.NodeID]*bls.PublicKey{
				{},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight + 1,
				},
				vdrs[1].NodeID: {
					NodeID:    vdrs[1].NodeID,
					PublicKey: vdrs[1].PublicKey,
					Weight:    vdrs[1].Weight - 1,
				},
				vdrs[2].NodeID: {
					NodeID: vdrs[2].NodeID,
					Weight: vdrs[2].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "1 before tip; subnet",
			height:             5,
			lastAcceptedHeight: 6,
			subnetID:           ids.GenerateTestID(),
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
				copyPrimaryValidator(vdrs[1]),
				copyPrimaryValidator(vdrs[3]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(vdrs[0]),
				copySubnetValidator(vdrs[1]),
			},
			weightDiffs: []map[ids.NodeID]*state.ValidatorWeightDiff{
				{
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: true,
						Amount:   vdrs[2].Weight,
					},
				},
			},
			pkDiffs: []map[ids.NodeID]*bls.PublicKey{
				{},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight + 1,
				},
				vdrs[1].NodeID: {
					NodeID:    vdrs[1].NodeID,
					PublicKey: vdrs[1].PublicKey,
					Weight:    vdrs[1].Weight - 1,
				},
				vdrs[2].NodeID: {
					NodeID: vdrs[2].NodeID,
					Weight: vdrs[2].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "unrelated primary network key removal on subnet lookup",
			height:             4,
			lastAcceptedHeight: 5,
			subnetID:           ids.GenerateTestID(),
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
			},
			currentSubnetValidators: []*validators.Validator{
				copySubnetValidator(vdrs[0]),
			},
			weightDiffs: []map[ids.NodeID]*state.ValidatorWeightDiff{
				{},
			},
			pkDiffs: []map[ids.NodeID]*bls.PublicKey{
				{
					vdrs[1].NodeID: vdrs[1].PublicKey,
				},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight,
				},
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// setup validators set
			vdrs := validators.NewMockManager(ctrl)
			cfg := config.Config{
				Chains:                 chains.TestManager,
				UptimePercentage:       .2,
				RewardConfig:           defaultRewardConfig,
				Validators:             vdrs,
				UptimeLockedCalculator: uptime.NewLockedCalculator(),
				BanffTime:              mockable.MaxTime,
			}
			mockState := state.NewMockState(ctrl)

			metrics, err := metrics.New("", prometheus.NewRegistry(), cfg.TrackedSubnets)
			r.NoError(err)

			clk := &mockable.Clock{}
			validatorssSet := NewManager(cfg, mockState, metrics, clk)

			// Mock the VM's validators
			mockSubnetVdrSet := validators.NewMockSet(ctrl)
			mockSubnetVdrSet.EXPECT().List().Return(tt.currentSubnetValidators).AnyTimes()
			vdrs.EXPECT().Get(tt.subnetID).Return(mockSubnetVdrSet, true).AnyTimes()

			mockPrimaryVdrSet := mockSubnetVdrSet
			if tt.subnetID != constants.PrimaryNetworkID {
				mockPrimaryVdrSet = validators.NewMockSet(ctrl)
				vdrs.EXPECT().Get(constants.PrimaryNetworkID).Return(mockPrimaryVdrSet, true).AnyTimes()
			}
			for _, vdr := range tt.currentPrimaryNetworkValidators {
				mockPrimaryVdrSet.EXPECT().Get(vdr.NodeID).Return(vdr, true).AnyTimes()
			}

			// Tell state what diffs to report
			for _, weightDiff := range tt.weightDiffs {
				mockState.EXPECT().GetValidatorWeightDiffs(gomock.Any(), gomock.Any()).Return(weightDiff, nil)
			}

			for _, pkDiff := range tt.pkDiffs {
				mockState.EXPECT().GetValidatorPublicKeyDiffs(gomock.Any()).Return(pkDiff, nil)
			}

			// Tell state last accepted block to report
			mockTip := blocks.NewMockBlock(ctrl)
			mockTip.EXPECT().Height().Return(tt.lastAcceptedHeight)
			mockTipID := ids.GenerateTestID()
			mockState.EXPECT().GetLastAccepted().Return(mockTipID)
			mockState.EXPECT().GetStatelessBlock(mockTipID).Return(mockTip, choices.Accepted, nil)

			// Compute validator set at previous height
			gotVdrSet, err := validatorssSet.GetValidatorSet(context.Background(), tt.height, tt.subnetID)
			r.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			r.Equal(tt.expectedVdrSet, gotVdrSet)
		})
	}
}

func Test_RegressionBLSKeyDiff(t *testing.T) {
	// setup
	require := require.New(t)
	cfg, ctx, clk, pState, txBuilder, err := buildSetup()
	require.NoError(err)

	currentHeight := uint64(1)
	currentTime := clk.Time().Add(time.Second)
	clk.Set(currentTime)

	// add a subnet
	subnetTx, err := txBuilder.NewCreateSubnetTx(
		1, // threshold
		[]ids.ShortID{ // control keys
			preFundedKeys[0].PublicKey().Address(),
		},
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		preFundedKeys[0].PublicKey().Address(),
	)
	require.NoError(err)
	pState.AddSubnet(subnetTx)
	pState.AddTx(subnetTx, status.Committed)
	pState.SetHeight(currentHeight)
	pState.SetTimestamp(clk.Time())
	require.NoError(pState.Commit())

	subnetID := subnetTx.ID()
	cfg.TrackedSubnets.Add(subnetID)
	subnetValidators := validators.NewSet()
	require.NoError(pState.ValidatorSet(subnetID, subnetValidators))
	require.True(cfg.Validators.Add(subnetID, subnetValidators))

	// A subnet validator should stake twice before its primary network counterpart stops staking
	var (
		primaryStartTime   = currentTime.Add(time.Second)
		primaryStartHeight = currentHeight + 1

		subnetStartTime1   = primaryStartTime.Add(2 * time.Second)
		subnetStartHeight1 = primaryStartHeight + 1

		subnetEndTime1   = subnetStartTime1.Add(defaultMinStakingDuration)
		subnetEndHeight1 = subnetStartHeight1 + 1

		subnetStartTime2   = subnetEndTime1.Add(2 * time.Second)
		subnetStartHeight2 = subnetEndHeight1 + 1

		subnetEndTime2   = subnetStartTime2.Add(defaultMinStakingDuration)
		subnetEndHeight2 = subnetStartHeight2 + 1

		primaryEndTime   = subnetEndTime2.Add(time.Second)
		primaryEndHeight = subnetEndHeight2 + 1
	)

	// move time/height ahead
	currentHeight = primaryStartHeight
	currentTime = primaryStartTime
	clk.Set(currentTime)

	// insert primary network validator
	nodeID := ids.GenerateTestNodeID()
	addr := preFundedKeys[0].PublicKey().Address()
	skBytes, err := hex.DecodeString("6668fecd4595b81e4d568398c820bbf3f073cb222902279fa55ebb84764ed2e3")
	require.NoError(err)
	sk, err := bls.SecretKeyFromBytes(skBytes)
	require.NoError(err)

	uPrimaryTx := &txs.AddPermissionlessValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Ins:          []*avax.TransferableInput{},
			Outs:         []*avax.TransferableOutput{},
		}},
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  uint64(primaryStartTime.Unix()),
			End:    uint64(primaryEndTime.Unix()),
			Wght:   2000000000000,
		},
		Subnet: constants.PrimaryNetworkID,
		Signer: signer.NewProofOfPossession(sk),
		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: avaxAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2 * units.KiloAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs: []ids.ShortID{
							addr,
						},
					},
				},
			},
		},
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				addr,
			},
		},
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				addr,
			},
		},
		DelegationShares: reward.PercentDenominator,
	}
	primaryTx, err := txs.NewSigned(uPrimaryTx, txs.Codec, nil)
	require.NoError(err)
	require.NoError(primaryTx.SyntacticVerify(ctx))

	require.NoError(err)
	dummyBlock, err := blocks.NewBanffStandardBlock(
		currentTime,
		pState.GetLastAccepted(),
		currentHeight,
		[]*txs.Tx{primaryTx},
	)
	require.NoError(err)

	primaryValidator, err := state.NewCurrentStaker(
		primaryTx.ID(),
		uPrimaryTx,
		0,
	)
	require.NoError(err)

	pState.PutCurrentValidator(primaryValidator)
	pState.SetLastAccepted(dummyBlock.ID())
	pState.AddStatelessBlock(dummyBlock, choices.Accepted)
	pState.AddTx(primaryTx, status.Committed)
	pState.SetHeight(currentHeight)
	pState.SetTimestamp(currentTime)
	require.NoError(pState.Commit())

	// move time/height ahead
	currentHeight = subnetStartHeight1
	currentTime = subnetStartTime1
	clk.Set(currentTime)

	// insert first subnet validator
	subnetFirstTx, err := txBuilder.NewAddSubnetValidatorTx(
		1,                               // Weight
		uint64(subnetStartTime1.Unix()), // Start time
		uint64(subnetEndTime1.Unix()),   // end time
		nodeID,                          // Node ID
		subnetID,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		addr,
	)
	require.NoError(err)
	dummyBlock, err = blocks.NewBanffStandardBlock(
		currentTime,
		pState.GetLastAccepted(),
		currentHeight,
		[]*txs.Tx{subnetFirstTx},
	)
	require.NoError(err)

	firstSubnetValidator, err := state.NewCurrentStaker(
		subnetFirstTx.ID(),
		subnetFirstTx.Unsigned.(*txs.AddSubnetValidatorTx),
		0,
	)
	require.NoError(err)

	pState.PutCurrentValidator(firstSubnetValidator)
	pState.SetLastAccepted(dummyBlock.ID())
	pState.AddStatelessBlock(dummyBlock, choices.Accepted)
	pState.AddTx(subnetFirstTx, status.Committed)
	pState.SetHeight(currentHeight)
	pState.SetTimestamp(currentTime)
	require.NoError(pState.Commit())

	// move time/height ahead
	currentHeight = subnetEndHeight1
	currentTime = subnetEndTime1
	clk.Set(currentTime)

	// drop first subnet validator
	dummyBlock, err = blocks.NewBanffStandardBlock(
		currentTime,
		pState.GetLastAccepted(),
		currentHeight,
		[]*txs.Tx{},
	)
	require.NoError(err)

	pState.DeleteCurrentValidator(firstSubnetValidator)
	pState.SetLastAccepted(dummyBlock.ID())
	pState.AddStatelessBlock(dummyBlock, choices.Accepted)
	pState.SetHeight(currentHeight)
	pState.SetTimestamp(currentTime)
	require.NoError(pState.Commit())

	// move time/height ahead
	currentHeight = subnetStartHeight2
	currentTime = subnetStartTime2
	clk.Set(currentTime)

	// insert second subnet validator
	subnetSecondTx, err := txBuilder.NewAddSubnetValidatorTx(
		1,                               // Weight
		uint64(subnetStartTime2.Unix()), // Start time
		uint64(subnetEndTime2.Unix()),   // end time
		nodeID,                          // Node ID
		subnetID,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
		addr,
	)
	require.NoError(err)
	dummyBlock, err = blocks.NewBanffStandardBlock(
		currentTime,
		pState.GetLastAccepted(),
		currentHeight,
		[]*txs.Tx{subnetSecondTx},
	)
	require.NoError(err)

	secondSubnetValidator, err := state.NewCurrentStaker(
		subnetSecondTx.ID(),
		subnetSecondTx.Unsigned.(*txs.AddSubnetValidatorTx),
		0,
	)
	require.NoError(err)

	pState.PutCurrentValidator(secondSubnetValidator)
	pState.SetLastAccepted(dummyBlock.ID())
	pState.AddStatelessBlock(dummyBlock, choices.Accepted)
	pState.AddTx(subnetSecondTx, status.Committed)
	pState.SetHeight(currentHeight)
	pState.SetTimestamp(currentTime)
	require.NoError(pState.Commit())

	// move time/height ahead
	currentHeight = subnetEndHeight2
	currentTime = subnetEndTime2
	clk.Set(currentTime)

	// drop second subnet validator
	dummyBlock, err = blocks.NewBanffStandardBlock(
		currentTime,
		pState.GetLastAccepted(),
		currentHeight,
		[]*txs.Tx{},
	)
	require.NoError(err)

	pState.DeleteCurrentValidator(secondSubnetValidator)
	pState.SetLastAccepted(dummyBlock.ID())
	pState.AddStatelessBlock(dummyBlock, choices.Accepted)
	pState.SetHeight(currentHeight)
	pState.SetTimestamp(currentTime)
	require.NoError(pState.Commit())

	// move time/height ahead
	currentHeight = primaryEndHeight
	currentTime = primaryEndTime
	clk.Set(currentTime)

	// drop primary network validator
	dummyBlock, err = blocks.NewBanffStandardBlock(
		currentTime,
		pState.GetLastAccepted(),
		currentHeight,
		[]*txs.Tx{},
	)
	require.NoError(err)

	pState.DeleteCurrentValidator(primaryValidator)
	pState.SetLastAccepted(dummyBlock.ID())
	pState.AddStatelessBlock(dummyBlock, choices.Accepted)
	pState.SetHeight(currentHeight)
	pState.SetTimestamp(currentTime)
	require.NoError(pState.Commit())

	valMan := NewManager(*cfg, pState, metrics.Noop, clk)
	_, err = valMan.GetValidatorSet(context.Background(), primaryStartHeight+1, constants.PrimaryNetworkID)
	require.NoError(err)
}

func buildSetup() (
	*config.Config,
	*snow.Context,
	*mockable.Clock,
	state.State,
	builder.Builder,
	error,
) {
	// build platformVM config
	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	trackedSubnets := set.NewSet[ids.ID](1) // will add subnet later on
	cfg := &config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		Validators:             vdrs,
		TrackedSubnets:         trackedSubnets,
		TxFee:                  defaultTxFee,
		CreateSubnetTxFee:      100 * defaultTxFee,
		CreateBlockchainTxFee:  100 * defaultTxFee,
		MinValidatorStake:      5 * units.MilliAvax,
		MaxValidatorStake:      500 * units.MilliAvax,
		MinDelegatorStake:      1 * units.MilliAvax,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
	}

	// build context
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = 10
	ctx.XChainID = xChainID
	ctx.CChainID = cChainID
	ctx.AVAXAssetID = avaxAssetID
	ctx.ValidatorState = &validators.TestState{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constants.PlatformChainID: constants.PrimaryNetworkID,
				xChainID:                  constants.PrimaryNetworkID,
				cChainID:                  constants.PrimaryNetworkID,
			}[chainID]
			if !ok {
				return ids.Empty, errors.New("missing")
			}
			return subnetID, nil
		},
	}
	clk := &mockable.Clock{}
	clk.Set(defaultGenesisTime)

	baseDBManager := dbmanager.NewMemDB(version.Semantic1_0_0)
	db := versiondb.New(baseDBManager.Current().Database)
	rewardsCalc := reward.NewCalculator(cfg.RewardConfig)
	genesisBytes := buildGenesisTest(ctx)
	pState, err := state.New(
		db,
		genesisBytes,
		prometheus.NewRegistry(),
		cfg,
		ctx,
		metrics.Noop,
		rewardsCalc,
		&utils.Atomic[bool]{},
	)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("could not build pState, %w", err)
	}

	// build fx
	fxVMInt := &fxVMInt{
		registry: linearcodec.NewDefault(),
		clk:      clk,
		log:      ctx.Log,
	}
	fx := &secp256k1fx.Fx{}
	if err := fx.Initialize(fxVMInt); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("could not build feature extension, %w", err)
	}
	if err := fx.Bootstrapped(); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("could not bootstrap feature extension, %w", err)
	}

	atomicUTXOs := avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec)
	utxoHandler := utxo.NewHandler(ctx, clk, fx)
	txBuilder := builder.New(
		ctx,
		cfg,
		clk,
		fx,
		pState,
		atomicUTXOs,
		utxoHandler,
	)
	return cfg, ctx, clk, pState, txBuilder, nil
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

func buildGenesisTest(ctx *snow.Context) []byte {
	genesisUTXOs := make([]api.UTXO, len(preFundedKeys))
	for i, key := range preFundedKeys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
		if err != nil {
			panic(err)
		}
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	// No validators in this genesis. We want to control in test which stakers are added and when
	genesisValidators := make([]api.PermissionlessValidator, 0)
	defaultGenesisTime := time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)

	buildGenesisArgs := api.BuildGenesisArgs{
		NetworkID:     json.Uint32(constants.UnitTestID),
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
		panic(fmt.Errorf("problem while building platform chain's genesis state: %w", err))
	}

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	if err != nil {
		panic(err)
	}

	return genesisBytes
}

func copyPrimaryValidator(vdr *validators.Validator) *validators.Validator {
	newVdr := *vdr
	return &newVdr
}

func copySubnetValidator(vdr *validators.Validator) *validators.Validator {
	newVdr := *vdr
	newVdr.PublicKey = nil
	return &newVdr
}
