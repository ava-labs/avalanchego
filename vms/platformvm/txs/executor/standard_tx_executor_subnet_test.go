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
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// This tests that the math performed during TransformSubnetTx execution can
// never overflow
const _ time.Duration = math.MaxUint32 * time.Second

// TestStandardExecutorTransformSubnetTxErrors verifies the failure cases of
// [platform.TransformSubnetTx] execution.
func TestStandardExecutorTransformSubnetTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Durango)
	wallet := newWallet(t, env, walletConfig{})

	tests := []struct {
		name        string
		want        error
		updateTx    func(*platform.Tx)
		updateState func(*state.Diff)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.TransformSubnetTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "max_stake_duration_too_large",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.TransformSubnetTx).MaxStakeDuration = math.MaxUint32
			},
			want: errMaxStakeDurationTooLarge,
		},
		{
			name: "fail_subnet_authorization",
			updateTx: func(tx *platform.Tx) {
				tx.Creds = nil
			},
			want: errWrongNumberOfCredentials,
		},
		{
			name: "flow_checker_failed",
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
			want: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name: "invalid_after_subnet_conversion",
			updateState: func(diff *state.Diff) {
				diff.SetSubnetToL1Conversion(testSubnet1.ID(), state.SubnetToL1Conversion{
					ConversionID: ids.GenerateTestID(),
					ChainID:      ids.GenerateTestID(),
					Addr:         make([]byte, 20),
				})
			},
			want: errIsImmutable,
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

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, env.state)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorTransformSubnetTx verifies the successful execution of
// a [platform.TransformSubnetTx].
func TestStandardExecutorTransformSubnetTx(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Durango)
	wallet := newWallet(t, env, walletConfig{})

	stx, err := wallet.IssueTransformSubnetTx(
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
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionAllowed)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	tx := stx.Unsigned.(*platform.TransformSubnetTx)

	// assert that the subnet's transform info was set
	gotTx, err := diff.GetSubnetTransformation(tx.Subnet)
	require.NoError(err)
	require.Equal(stx, gotTx)

	// initial supply was updated
	supply, err := diff.GetCurrentSupply(tx.Subnet)
	require.NoError(err)
	require.Equal(tx.InitialSupply, supply)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)
}

// TestStandardExecutorCreateChainTxErrors verifies the failure cases of
// [platform.CreateChainTx] execution.
func TestStandardExecutorCreateChainTxErrors(t *testing.T) {
	var (
		env      = newEnvironment(t, upgradetest.Latest)
		wallet   = newWallet(t, env, walletConfig{})
		subnetID = testSubnet1.ID()
	)

	tests := []struct {
		name        string
		want        error
		updateTx    func(*platform.Tx)
		updateState func(*state.Diff)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.CreateChainTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "insufficient_control_signatures",
			updateTx: func(tx *platform.Tx) {
				// Remove a signature from the subnet auth credential
				cred := tx.Creds[len(tx.Creds)-1].(*secp256k1fx.Credential)
				cred.Sigs = cred.Sigs[1:]
			},
			want: errUnauthorizedModification,
		},
		{
			name: "wrong_control_signature",
			updateTx: func(tx *platform.Tx) {
				// Replace a valid signature with one from a new, random key
				key, err := secp256k1.NewPrivateKey()
				require.NoError(t, err)

				sig, err := key.SignHash(hashing.ComputeHash256(tx.Unsigned.Bytes()))
				require.NoError(t, err)

				cred := tx.Creds[len(tx.Creds)-1].(*secp256k1fx.Credential)
				copy(cred.Sigs[0][:], sig)
			},
			want: errUnauthorizedModification,
		},
		{
			name: "subnet_not_found",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.CreateChainTx).SubnetID = ids.GenerateTestID()
			},
			want: database.ErrNotFound,
		},
		{
			name: "invalid_after_subnet_conversion",
			updateState: func(diff *state.Diff) {
				diff.SetSubnetToL1Conversion(subnetID, state.SubnetToL1Conversion{
					ConversionID: ids.GenerateTestID(),
					ChainID:      ids.GenerateTestID(),
					Addr:         []byte("address"),
				})
			},
			want: errIsImmutable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := wallet.IssueCreateChainTx(
				subnetID,
				nil,
				constants.AVMID,
				nil,
				"chain name",
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorCreateChainTx verifies the successful execution of a
// [platform.CreateChainTx].
func TestStandardExecutorCreateChainTx(t *testing.T) {
	require := require.New(t)

	var (
		env      = newEnvironment(t, upgradetest.Latest)
		wallet   = newWallet(t, env, walletConfig{})
		subnetID = testSubnet1.ID()
	)

	stx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, diff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)

	// assert the chain was added to the subnet
	require.NoError(diff.Apply(env.state))

	chains, err := env.state.GetChains(subnetID)
	require.NoError(err)

	gotChainIDs := set.Of[ids.ID]()
	for _, chain := range chains {
		gotChainIDs.Add(chain.ID())
	}
	require.Contains(gotChainIDs, stx.ID())
}

// TestStandardExecutorCreateSubnetTxErrors verifies the failure cases of
// [platform.CreateSubnetTx] execution.
func TestStandardExecutorCreateSubnetTxErrors(t *testing.T) {
	var (
		env    = newEnvironment(t, upgradetest.Latest)
		wallet = newWallet(t, env, walletConfig{})
	)

	tests := []struct {
		name     string
		want     error
		updateTx func(*platform.Tx)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.CreateSubnetTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(tx *platform.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*platform.CreateSubnetTx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			want: utxo.ErrInsufficientUnlockedFunds,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := wallet.IssueCreateSubnetTx(
				newOwner(),
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorCreateSubnetTx verifies the successful execution of a
// [platform.CreateSubnetTx].
func TestStandardExecutorCreateSubnetTx(t *testing.T) {
	require := require.New(t)

	var (
		env    = newEnvironment(t, upgradetest.Latest)
		wallet = newWallet(t, env, walletConfig{})
		owner  = newOwner()
	)

	stx, err := wallet.IssueCreateSubnetTx(owner)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, diff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)

	// assert the subnet was created with the expected owner
	subnetID := stx.ID()
	gotOwner, err := diff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner, gotOwner)

	require.NoError(diff.Apply(env.state))

	subnetIDs, err := env.state.GetSubnetIDs()
	require.NoError(err)
	require.Contains(subnetIDs, subnetID)
}

// TestStandardExecutorTransferSubnetOwnershipTxErrors verifies the failure
// cases of [platform.TransferSubnetOwnershipTx] execution.
func TestStandardExecutorTransferSubnetOwnershipTxErrors(t *testing.T) {
	var (
		env      = newEnvironment(t, upgradetest.Latest)
		subnetID = testSubnet1.ID()
	)

	tests := []struct {
		name        string
		want        error
		updateTx    func(*platform.Tx)
		updateState func(*state.Diff)
	}{
		{
			name: "invalid_prior_to_durango",
			updateState: func(diff *state.Diff) {
				diff.SetTimestamp(env.config.UpgradeConfig.DurangoTime.Add(-1 * time.Second))
			},
			want: errDurangoUpgradeNotActive,
		},
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.TransferSubnetOwnershipTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "subnet_not_found",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.TransferSubnetOwnershipTx).Subnet = ids.GenerateTestID()
			},
			want: database.ErrNotFound,
		},
		{
			name: "tx_has_no_credentials",
			updateTx: func(tx *platform.Tx) {
				tx.Creds = nil
			},
			want: errWrongNumberOfCredentials,
		},
		{
			name: "insufficient_control_signatures",
			updateTx: func(tx *platform.Tx) {
				// Remove a signature from the subnet auth credential
				cred := tx.Creds[len(tx.Creds)-1].(*secp256k1fx.Credential)
				cred.Sigs = cred.Sigs[1:]
			},
			want: errUnauthorizedModification,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(tx *platform.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*platform.TransferSubnetOwnershipTx)
				unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64 / 2,
					},
				})
			},
			want: errFlowCheckFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The wallet tracks the subnet owner change once it issues the tx,
			// so issue each tx from a fresh wallet to keep the subnet auth
			// signable.
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueTransferSubnetOwnershipTx(
				subnetID,
				newOwner(),
			)
			require.NoError(t, err)

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			if tt.updateState != nil {
				tt.updateState(diff)
			}

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, _, _, got = StandardTx(
				&env.backend,
				feeCalculator,
				tx,
				diff,
			)

			require.ErrorIs(t, got, tt.want)
		})
	}
}

// TestStandardExecutorTransferSubnetOwnershipTx verifies the successful
// execution of a [platform.TransferSubnetOwnershipTx].
func TestStandardExecutorTransferSubnetOwnershipTx(t *testing.T) {
	require := require.New(t)

	var (
		env       = newEnvironment(t, upgradetest.Latest)
		wallet    = newWallet(t, env, walletConfig{})
		subnetID  = testSubnet1.ID()
		wantOwner = newOwner()
	)

	stx, err := wallet.IssueTransferSubnetOwnershipTx(subnetID, wantOwner)
	require.NoError(err)

	diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, diff)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		stx,
		diff,
	)
	require.NoError(err)

	requireBaseTxApplied(t, env, diff, feeCalculator, stx)

	// assert the subnet's owner was updated
	gotOwner, err := diff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(wantOwner, gotOwner)
}
