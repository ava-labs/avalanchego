// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

var fundedSharedMemoryCalls byte

// TestStandardExecutorImportTxErrors verifies the failure cases of
// [platform.ImportTx] execution.
func TestStandardExecutorImportTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	sourceKey, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	env.msm.SharedMemory = fundedSharedMemory(
		t,
		env,
		sourceKey,
		env.ctx.XChainID,
		map[ids.ID]uint64{
			env.ctx.AVAXAssetID: 1,
		},
		rand.NewSource(0),
	)

	tests := []struct {
		name     string
		want     error
		updateTx func(*platform.Tx)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.ImportTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "import_from_same_chain",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.ImportTx).SourceChain = env.ctx.ChainID
			},
			want: verify.ErrSameChainID,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(tx *platform.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*platform.ImportTx)
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
			wallet := newWallet(t, env, walletConfig{
				keys:     []*secp256k1.PrivateKey{sourceKey},
				chainIDs: []ids.ID{env.ctx.XChainID},
			})

			tx, err := wallet.IssueImportTx(
				env.ctx.XChainID,
				newOwner(),
			)
			require.NoError(t, err)

			if tt.updateTx != nil {
				tt.updateTx(tx)
			}

			diff, got := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(t, got)

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

// TestStandardExecutorImportTx verifies the successful execution of an
// [platform.ImportTx].
func TestStandardExecutorImportTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	sourceKey, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	customAssetID := ids.GenerateTestID()
	// generate a constant random source generator.
	randSrc := rand.NewSource(0)

	tests := []struct {
		name          string
		sourceChainID ids.ID
		assets        map[ids.ID]uint64
		timestamp     time.Time
	}{
		{
			name:          "can_barely_pay_fee",
			sourceChainID: env.ctx.XChainID,
			assets: map[ids.ID]uint64{
				env.ctx.AVAXAssetID: 1,
			},
		},
		{
			name:          "import_from_c_chain",
			sourceChainID: env.ctx.CChainID,
			assets: map[ids.ID]uint64{
				env.ctx.AVAXAssetID: 1,
			},
			timestamp: env.config.UpgradeConfig.ApricotPhase5Time,
		},
		{
			name:          "import_non_avax_from_x_chain",
			sourceChainID: env.ctx.XChainID,
			assets: map[ids.ID]uint64{
				customAssetID: 1,
			},
			timestamp: env.config.UpgradeConfig.ApricotPhase5Time,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			env.msm.SharedMemory = fundedSharedMemory(
				t,
				env,
				sourceKey,
				tt.sourceChainID,
				tt.assets,
				randSrc,
			)

			wallet := newWallet(t, env, walletConfig{
				keys:     []*secp256k1.PrivateKey{sourceKey},
				chainIDs: []ids.ID{tt.sourceChainID},
			})

			stx, err := wallet.IssueImportTx(
				tt.sourceChainID,
				newOwner(),
			)
			require.NoError(err)

			tx := stx.Unsigned.(*platform.ImportTx)
			require.NotEmpty(tx.ImportedInputs)
			numInputs := len(tx.Ins) + len(tx.ImportedInputs)
			require.Equal(len(stx.Creds), numInputs, "should have the same number of credentials as inputs")

			totalIn := uint64(0)
			for _, in := range tx.Ins {
				totalIn += in.Input().Amount()
			}
			for _, in := range tx.ImportedInputs {
				totalIn += in.Input().Amount()
			}
			totalOut := uint64(0)
			for _, out := range tx.Outs {
				totalOut += out.Out.Amount()
			}
			require.Equal(totalIn, totalOut)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			diff.SetTimestamp(tt.timestamp)

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			inputs, atomicRequests, _, err := StandardTx(
				&env.backend,
				feeCalculator,
				stx,
				diff,
			)
			require.NoError(err)

			requireBaseTxApplied(t, env, diff, feeCalculator, stx)

			// assert the imported utxos are consumed from the source chain's
			// shared memory
			wantInputs := set.NewSet[ids.ID](len(tx.ImportedInputs))
			for _, in := range tx.ImportedInputs {
				wantInputs.Add(in.InputID())
			}
			require.Equal(wantInputs, inputs)
			require.Len(atomicRequests[tt.sourceChainID].RemoveRequests, len(tx.ImportedInputs))
		})
	}
}

// TestNewImportTxInsufficientFunds verifies that the wallet can't build an
// [platform.ImportTx] when there is nothing to import.
func TestNewImportTxInsufficientFunds(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	sourceKey, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	env.msm.SharedMemory = fundedSharedMemory(
		t,
		env,
		sourceKey,
		env.ctx.XChainID,
		map[ids.ID]uint64{},
		rand.NewSource(0),
	)

	wallet := newWallet(t, env, walletConfig{
		keys:     []*secp256k1.PrivateKey{sourceKey},
		chainIDs: []ids.ID{env.ctx.XChainID},
	})
	_, err = wallet.IssueImportTx(
		env.ctx.XChainID,
		newOwner(),
	)
	require.ErrorIs(t, err, builder.ErrInsufficientFunds)
}

// Returns a shared memory where GetDatabase returns a database
// where recipientKey has a balance of [amt]
func fundedSharedMemory(
	t *testing.T,
	env *environment,
	sourceKey *secp256k1.PrivateKey,
	peerChain ids.ID,
	assets map[ids.ID]uint64,
	randSrc rand.Source,
) atomic.SharedMemory {
	fundedSharedMemoryCalls++
	m := atomic.NewMemory(prefixdb.New([]byte{fundedSharedMemoryCalls}, env.baseDB))

	sm := m.NewSharedMemory(env.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(peerChain)

	for assetID, amt := range assets {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: uint32(randSrc.Int63()),
			},
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amt,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{sourceKey.Address()},
					Threshold: 1,
				},
			},
		}
		utxoBytes, err := platform.Codec.Marshal(platform.CodecVersion, utxo)
		require.NoError(t, err)

		inputID := utxo.InputID()
		require.NoError(t, peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
			env.ctx.ChainID: {
				PutRequests: []*atomic.Element{
					{
						Key:   inputID[:],
						Value: utxoBytes,
						Traits: [][]byte{
							sourceKey.Address().Bytes(),
						},
					},
				},
			},
		}))
	}

	return sm
}

// TestStandardExecutorExportTxErrors verifies the failure cases of
// [platform.ExportTx] execution.
func TestStandardExecutorExportTxErrors(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	tests := []struct {
		name     string
		want     error
		updateTx func(*platform.Tx)
	}{
		{
			name: "tx_fails_syntactic_verification",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.ExportTx).BaseTx.BlockchainID = ids.GenerateTestID()
			},
			want: avax.ErrWrongChainID,
		},
		{
			name: "export_to_same_chain",
			updateTx: func(tx *platform.Tx) {
				tx.Unsigned.(*platform.ExportTx).DestinationChain = env.ctx.ChainID
			},
			want: verify.ErrSameChainID,
		},
		{
			name: "flow_checker_failed",
			updateTx: func(tx *platform.Tx) {
				// Produce more AVAX than the tx consumes
				unsignedTx := tx.Unsigned.(*platform.ExportTx)
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
			// The tx spends AVAX, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{})
			tx, err := wallet.IssueExportTx(
				env.ctx.XChainID,
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          units.Avax,
						OutputOwners: *newOwner(),
					},
				}},
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

// TestStandardExecutorExportTx verifies the successful execution of an
// [platform.ExportTx].
func TestStandardExecutorExportTx(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)

	tests := []struct {
		name               string
		destinationChainID ids.ID
		timestamp          time.Time
	}{
		{
			name:               "p_to_x_export",
			destinationChainID: env.ctx.XChainID,
			timestamp:          genesistest.DefaultValidatorStartTime,
		},
		{
			name:               "p_to_c_export",
			destinationChainID: env.ctx.CChainID,
			timestamp:          env.config.UpgradeConfig.ApricotPhase5Time,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// The tx spends AVAX, so issue each tx from a fresh wallet to keep
			// the wallet's UTXO view consistent with env.state.
			wallet := newWallet(t, env, walletConfig{})
			stx, err := wallet.IssueExportTx(
				tt.destinationChainID,
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          genesistest.DefaultInitialBalance - defaultTxFee,
						OutputOwners: *newOwner(),
					},
				}},
			)
			require.NoError(err)

			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			diff.SetTimestamp(tt.timestamp)

			feeCalculator := state.PickFeeCalculator(env.config, diff)
			_, atomicRequests, _, err := StandardTx(
				&env.backend,
				feeCalculator,
				stx,
				diff,
			)
			require.NoError(err)

			requireBaseTxApplied(t, env, diff, feeCalculator, stx)

			// assert the exported outputs are put into the destination chain's
			// shared memory
			tx := stx.Unsigned.(*platform.ExportTx)
			require.Len(atomicRequests[tt.destinationChainID].PutRequests, len(tx.ExportedOutputs))
		})
	}
}
