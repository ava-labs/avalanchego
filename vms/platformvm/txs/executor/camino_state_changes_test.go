// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestCaminoAdvanceTimeTo(t *testing.T) {
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}
	caminoVMConfig := config.CaminoConfig{
		ValidatorsRewardPeriod: 10,
	}

	baseState := func(c *gomock.Controller) *state.MockState {
		s := state.NewMockState(c)
		// shutdown
		s.EXPECT().SetHeight(uint64(math.MaxUint64))
		s.EXPECT().Commit()
		s.EXPECT().Close()
		return s
	}

	testCases := map[string]struct {
		state              func(*state.MockState) *state.MockState
		atomicUTXOsManager func(*gomock.Controller, []*avax.UTXO) *avax.MockAtomicUTXOManager
		importedUTXOs      []*avax.UTXO
		newChainTime       time.Time
		expectedChanges    func(*testing.T, []*avax.UTXO, time.Time) *stateChanges
		expectedErr        error
	}{
		"OK": {
			state: func(state *state.MockState) *state.MockState {
				state.EXPECT().GetTimestamp().Return(time.Unix(0, 0))
				return state
			},
			atomicUTXOsManager: func(c *gomock.Controller, importedUTXOs []*avax.UTXO) *avax.MockAtomicUTXOManager {
				atomicUTXOsManager := avax.NewMockAtomicUTXOManager(c)
				atomicUTXOsManager.EXPECT().GetAtomicUTXOs(
					cChainID, feeRewardAddrTraits, ids.ShortEmpty, ids.Empty, MaxPageSize,
				).Return(importedUTXOs, ids.ShortEmpty, ids.Empty, nil)
				return atomicUTXOsManager
			},
			importedUTXOs: []*avax.UTXO{
				{
					UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
					Asset:  avax.Asset{ID: avaxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 100,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{feeRewardAddr},
						},
					},
				},
				{
					UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 1},
					Asset:  avax.Asset{ID: avaxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 110,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{feeRewardAddr},
						},
					},
				},
			},
			expectedChanges: func(t *testing.T, importedUTXOs []*avax.UTXO, newChainTime time.Time) *stateChanges {
				atomicInputs := set.NewSet[ids.ID](len(importedUTXOs))
				utxoIDs := make([][]byte, len(importedUTXOs))
				for i, utxo := range importedUTXOs {
					utxoID := utxo.InputID()
					utxoIDs[i] = utxoID[:]
					atomicInputs.Add(utxoID)
				}
				unsignedBytes, err := txs.Codec.Marshal(txs.Version, utxoIDs)
				require.NoError(t, err)
				txID, err := ids.ToID(hashing.ComputeHash256(unsignedBytes))
				require.NoError(t, err)

				importedAmount := uint64(0)
				for _, utxo := range importedUTXOs {
					importedAmount += utxo.Out.(*secp256k1fx.TransferOutput).Amt
				}

				newChainTimestamp := uint64(newChainTime.Unix())

				return &stateChanges{
					caminoStateChanges: caminoStateChanges{
						AddedUTXOs: []*avax.UTXO{{
							UTXOID: avax.UTXOID{TxID: txID, OutputIndex: 0},
							Asset:  avax.Asset{ID: avaxAssetID},
							Out: &secp256k1fx.TransferOutput{
								Amt: importedAmount,
								OutputOwners: secp256k1fx.OutputOwners{
									Threshold: 1,
									Addrs:     []ids.ShortID{feeRewardAddr},
								},
							},
						}},
						LastRewardImportTimestamp: &newChainTimestamp,
						AtomicRequests: map[ids.ID]*atomic.Requests{
							cChainID: {RemoveRequests: utxoIDs},
						},
						AtomicInputs: atomicInputs,
					},
				}
			},
			newChainTime: time.Unix(int64(caminoVMConfig.ValidatorsRewardPeriod), 0),
		},
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newMockableCaminoEnvironment(
				true,
				false,
				caminoVMConfig,
				caminoGenesisConf,
				tt.state(baseState(ctrl)),
				tt.atomicUTXOsManager(ctrl, tt.importedUTXOs),
			)
			defer require.NoError(shutdownCaminoEnvironment(env))
			env.ctx.Lock.Lock()

			changes := &stateChanges{}

			err := caminoAdvanceTimeTo(
				&env.backend,
				env.state,
				tt.newChainTime,
				changes,
			)

			require.ErrorIs(err, tt.expectedErr)
			require.Equal(tt.expectedChanges(t, tt.importedUTXOs, tt.newChainTime), changes)
		})
	}
}

func TestCaminoStateChangesApply(t *testing.T) {
	testCases := map[string]struct {
		diff               func(*gomock.Controller, []*avax.UTXO) *state.MockDiff
		caminoStateChanges func([]*avax.UTXO) *caminoStateChanges
		utxos              []*avax.UTXO
	}{
		"OK": {
			diff: func(c *gomock.Controller, utxos []*avax.UTXO) *state.MockDiff {
				diff := state.NewMockDiff(c)
				for _, utxo := range utxos {
					diff.EXPECT().AddUTXO(utxo)
				}
				return diff
			},
			utxos: []*avax.UTXO{
				{
					UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
					Asset:  avax.Asset{ID: avaxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 100,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{feeRewardAddr},
						},
					},
				},
				{
					UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 1},
					Asset:  avax.Asset{ID: avaxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 110,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{feeRewardAddr},
						},
					},
				},
			},
			caminoStateChanges: func(utxos []*avax.UTXO) *caminoStateChanges {
				return &caminoStateChanges{
					AddedUTXOs: utxos,
				}
			},
		},
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			tt.caminoStateChanges(tt.utxos).Apply(tt.diff(ctrl, tt.utxos))
		})
	}
}
