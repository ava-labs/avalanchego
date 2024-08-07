// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/generate"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestCaminoAddValidatorTxSyntacticVerify(t *testing.T) {
	ctx := defaultContext()
	nodeID := ids.NodeID{1, 1, 1}
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{{1}},
	}
	fee := uint64(100)

	tests := map[string]struct {
		preExecute  func(*testing.T, *CaminoAddValidatorTx) *CaminoAddValidatorTx
		expectedErr error
	}{
		"Happy path": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				return utx
			},
			expectedErr: nil,
		},
		"Tx is nil": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				return nil
			},
			expectedErr: ErrNilTx,
		},
		"Wrong networkID": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				utx.NetworkID++
				return utx
			},
			expectedErr: avax.ErrWrongNetworkID,
		},
		"Too many shares": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				utx.DelegationShares++
				return utx
			},
			expectedErr: errTooManyShares,
		},
		"Weight mismatch": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				utx.Validator.Wght++
				return utx
			},
			expectedErr: errValidatorWeightMismatch,
		},
		"Outputs asset is not AVAX": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				for _, out := range utx.Outs {
					out.Asset = avax.Asset{ID: ids.GenerateTestID()}
				}
				avax.SortTransferableOutputs(utx.Outs, Codec)
				return utx
			},
			expectedErr: errAssetNotAVAX,
		},
		"Stake outputs are not empty": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				utx.StakeOuts = append(utx.StakeOuts, generate.StakeableOut(ctx.AVAXAssetID, defaultWeight, 100, outputOwners))
				return utx
			},
			expectedErr: errStakeOutsNotEmpty,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			utx := &CaminoAddValidatorTx{
				AddValidatorTx: AddValidatorTx{
					BaseTx: BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
						Ins: []*avax.TransferableInput{
							generate.In(ctx.AVAXAssetID, defaultWeight*2, ids.Empty, ids.Empty, []uint32{0}),
						},
						Outs: []*avax.TransferableOutput{
							generate.Out(ctx.AVAXAssetID, defaultWeight-fee, outputOwners, ids.Empty, ids.Empty),
							generate.Out(ctx.AVAXAssetID, defaultWeight, outputOwners, ids.Empty, locked.ThisTxID),
						},
					}},
					Validator: Validator{
						NodeID: nodeID,
						Start:  100,
						End:    200,
						Wght:   defaultWeight,
					},
					RewardsOwner: &secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.ShortEmpty},
					},
				},
				NodeOwnerAuth: &secp256k1fx.Input{},
			}

			utx = tt.preExecute(t, utx)
			err := utx.SyntacticVerify(ctx)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
