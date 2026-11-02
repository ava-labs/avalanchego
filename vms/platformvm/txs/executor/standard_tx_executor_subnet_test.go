// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestStandardExecutorTransformSubnetTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.Durango)
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{testSubnet1.ID()},
	})

	type test struct {
		name            string
		updateTx        func(*platform.Tx)
		updateState     func(*state.Diff)
		stateAssertions func(*testing.T, *state.Diff, *platform.Tx)
		err             error
	}

	tests := []test{
		{
			name: "tx fails syntactic verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned = (*platform.TransformSubnetTx)(nil)
			},
			err: platform.ErrNilTx,
		},
		{
			name: "max stake duration too large",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.TransformSubnetTx).MaxStakeDuration = math.MaxUint32
			},
			err: errMaxStakeDurationTooLarge,
		},
		{
			name: "fail subnet authorization",
			updateTx: func(tx *platform.Tx) {
				tx.Creds = nil
			},
			err: errWrongNumberOfCredentials,
		},
		{
			name: "flow checker failed",
			updateTx: func(tx *platform.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*platform.TransformSubnetTx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			err: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name: "invalid after subnet conversion",
			updateState: func(diff *state.Diff) {
				diff.SetSubnetToL1Conversion(testSubnet1.ID(), state.SubnetToL1Conversion{
					ConversionID: ids.GenerateTestID(),
					ChainID:      ids.GenerateTestID(),
					Addr:         make([]byte, 20),
				})
			},
			err: errIsImmutable,
		},
		{
			name: "valid tx",
			stateAssertions: func(t *testing.T, state *state.Diff, stx *platform.Tx) {
				tx := stx.Unsigned.(*platform.TransformSubnetTx)

				// assert that the subnet's transform info was set
				gotTx, err := state.GetSubnetTransformation(tx.Subnet)
				require.NoError(t, err)
				require.Equal(t, stx, gotTx)

				// initial supply was updated
				supply, err := state.GetCurrentSupply(tx.Subnet)
				require.NoError(t, err)

				require.Equal(t, tx.InitialSupply, supply)

				// assert the utxos inputs were deleted
				for _, utxo := range tx.Ins {
					_, err := state.GetUTXO(utxo.InputID())
					require.ErrorIs(t, err, database.ErrNotFound)
				}

				// assert the utxos outputs were created
				for _, expectedOut := range stx.UTXOs() {
					utxo, err := state.GetUTXO(expectedOut.InputID())
					require.NoError(t, err)

					require.Equal(t, expectedOut.Out, utxo.Out)
				}
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := wallet.IssueTransformSubnetTx(
				testSubnet1.ID(),
				ids.GenerateTestID(),
				10,
				10,
				0,
				reward.PercentDenominator,
				2,
				10,
				time.Minute,
				time.Hour,
				1,
				10,
				1,
				80,
			)
			require.NoError(t, err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, err)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)
			require.ErrorIs(t, err, tt.err)

			if tt.stateAssertions != nil {
				tt.stateAssertions(t, diff, tx)
			}
		})
	}
}

// Ensure Execute fails when there are not enough control sigs
func TestCreateChainTxInsufficientControlSigs(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	// Remove a signature
	tx.Creds[0].(*secp256k1fx.Credential).Sigs = tx.Creds[0].(*secp256k1fx.Credential).Sigs[1:]

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.ErrorIs(err, errUnauthorizedModification)
}

// Ensure Execute fails when an incorrect control signature is given
func TestCreateChainTxWrongControlSig(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	// Generate new, random key to sign tx with
	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// Replace a valid signature with one from another key
	sig, err := key.SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
	require.NoError(err)
	copy(tx.Creds[0].(*secp256k1fx.Credential).Sigs[0][:], sig)

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.ErrorIs(err, errUnauthorizedModification)
}

// Ensure Execute fails when the Subnet the blockchain specifies as
// its validator set doesn't exist
func TestCreateChainTxNoSuchSubnet(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	tx.Unsigned.(*platform.CreateChainTx).SubnetID = ids.GenerateTestID()

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	builderDiff, err := state.NewDiffOn(stateDiff, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, builderDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.ErrorIs(err, database.ErrNotFound)
}

// Ensure valid tx passes semanticVerify
func TestCreateChainTxValid(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Banff)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	builderDiff, err := state.NewDiffOn(stateDiff, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, builderDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.NoError(err)
}

func TestCreateChainTxAP3FeeChange(t *testing.T) {
	ap3Time := genesistest.DefaultValidatorStartTime.Add(time.Hour)
	tests := []struct {
		name          string
		time          time.Time
		fee           uint64
		expectedError error
	}{
		{
			name:          "pre-fork - correctly priced",
			time:          genesistest.DefaultValidatorStartTime,
			fee:           0,
			expectedError: nil,
		},
		{
			name:          "post-fork - correctly priced",
			time:          ap3Time,
			fee:           100 * defaultTxFee,
			expectedError: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, upgradetest.Banff)
			env.config.UpgradeConfig.ApricotPhase3Time = ap3Time

			addrs := set.NewSet[ids.ShortID](len(genesistest.DefaultFundedKeys))
			for _, key := range genesistest.DefaultFundedKeys {
				addrs.Add(key.Address())
			}

			env.state.SetTimestamp(test.time) // to duly set fee

			config := *env.config
			subnetID := testSubnet1.ID()
			wallet := newWallet(t, env, walletConfig{
				config:    &config,
				subnetIDs: []ids.ID{subnetID},
			})

			tx, err := wallet.IssueCreateChainTx(
				subnetID,
				nil,
				ids.GenerateTestID(),
				nil,
				"",
			)
			require.NoError(err)

			stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			stateDiff.SetTimestamp(test.time)

			feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
			_, _, _, err = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				stateDiff,
			)
			require.ErrorIs(err, test.expectedError)
		})
	}
}

func TestEtnaCreateChainTxInvalidWithManagedSubnet(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Etna)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	stateDiff, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	builderDiff, err := state.NewDiffOn(stateDiff, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	stateDiff.SetSubnetToL1Conversion(
		subnetID,
		state.SubnetToL1Conversion{
			ConversionID: ids.GenerateTestID(),
			ChainID:      ids.GenerateTestID(),
			Addr:         []byte("address"),
		},
	)

	feeCalculator := state.PickFeeCalculator(env.config, builderDiff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		tx,
		stateDiff,
	)
	require.ErrorIs(err, errIsImmutable)
}
