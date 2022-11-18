// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestCaminoAddValidatorTxSyntacticVerify(t *testing.T) {
	ctx := snow.DefaultContextTest()
	ctx.AVAXAssetID = ids.GenerateTestID()
	nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()
	signers := [][]*crypto.PrivateKeySECP256K1R{{caminoPreFundedKeys[0]}, {nodeKey}}
	outputOwners := secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{caminoPreFundedKeys[0].PublicKey().Address()},
	}
	sigIndices := []uint32{0}

	tests := map[string]struct {
		preExecute          func(*testing.T, *CaminoAddValidatorTx) *CaminoAddValidatorTx
		expectedSpecificErr error
		// In some checks, avalanche implementation is not returning a specific error or this error is private
		// So in order not to change avalanche files we should just assert that we have some error
		expectedGeneralErr bool
	}{
		"Happy path": {
			preExecute:          func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx { return utx },
			expectedSpecificErr: nil,
		},
		"Tx is nil": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				return nil
			},
			expectedSpecificErr: errSignedTxNotInitialized,
		},
		"Wrong networkID": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				utx.NetworkID++
				return utx
			},
			expectedSpecificErr: avax.ErrWrongNetworkID,
		},
		"Too many shares": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				utx.DelegationShares++
				return utx
			},
			expectedSpecificErr: errTooManyShares,
		},
		"Weight mismatch": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				utx.Validator.Wght++
				return utx
			},
			expectedSpecificErr: errValidatorWeightMismatch,
		},
		"Outputs asset is not AVAX": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				for _, out := range utx.Outs {
					out.Asset = avax.Asset{ID: ids.GenerateTestID()}
				}
				avax.SortTransferableOutputs(utx.Outs, Codec)
				return utx
			},
			expectedSpecificErr: errAssetNotAVAX,
		},
		"Stake outputs are not empty": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				utx.StakeOuts = append(utx.StakeOuts, generateTestStakeableOut(ctx.AVAXAssetID, defaultCaminoValidatorWeight, uint64(defaultMinStakingDuration), outputOwners))
				return utx
			},
			expectedSpecificErr: errStakeOutsNotEmpty,
		},
		"Lock owner has no addresses": {
			preExecute: func(t *testing.T, utx *CaminoAddValidatorTx) *CaminoAddValidatorTx {
				utx.Outs[1].Out.(*locked.Out).TransferableOut.(*secp256k1fx.TransferOutput).Addrs = nil
				return utx
			},
			expectedGeneralErr: true,
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
							generateTestIn(ctx.AVAXAssetID, defaultCaminoValidatorWeight*2, ids.Empty, ids.Empty, sigIndices),
						},
						Outs: []*avax.TransferableOutput{
							generateTestOut(ctx.AVAXAssetID, defaultCaminoValidatorWeight-defaultTxFee, outputOwners, ids.Empty, ids.Empty),
							generateTestOut(ctx.AVAXAssetID, defaultCaminoValidatorWeight, outputOwners, ids.Empty, locked.ThisTxID),
						},
					}},
					Validator: validator.Validator{
						NodeID: nodeID,
						Start:  uint64(defaultValidateStartTime.Unix()) + 1,
						End:    uint64(defaultValidateEndTime.Unix()),
						Wght:   defaultCaminoValidatorWeight,
					},
					RewardsOwner: &secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.ShortEmpty},
					},
					DelegationShares: reward.PercentDenominator,
				},
			}

			utx = tt.preExecute(t, utx)
			tx, _ := NewSigned(utx, Codec, signers)
			err := tx.SyntacticVerify(ctx)
			if tt.expectedGeneralErr {
				require.Error(t, err)
			} else {
				require.ErrorIs(t, err, tt.expectedSpecificErr)
			}
		})
	}
}
