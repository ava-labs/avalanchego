// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestSetAutoRenewedValidatorConfigTxSyntacticVerify(t *testing.T) {
	type test struct {
		name    string
		txFunc  func() *SetAutoRenewedValidatorConfigTx
		wantErr error
	}

	var (
		networkID = uint32(1337)
		chainID   = ids.GenerateTestID()
	)

	ctx := &snow.Context{
		ChainID:   chainID,
		NetworkID: networkID,
	}

	// A BaseTx that already passed syntactic verification.
	verifiedBaseTx := BaseTx{
		SyntacticallyVerified: true,
	}

	// A BaseTx that passes syntactic verification.
	validBaseTx := BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		},
	}

	// A BaseTx that fails syntactic verification.
	invalidBaseTx := BaseTx{}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func() *SetAutoRenewedValidatorConfigTx {
				return nil
			},
			wantErr: ErrNilTx,
		},
		{
			name: "already verified",
			txFunc: func() *SetAutoRenewedValidatorConfigTx {
				return &SetAutoRenewedValidatorConfigTx{
					BaseTx: verifiedBaseTx,
				}
			},
			wantErr: nil,
		},
		{
			name: "empty txID",
			txFunc: func() *SetAutoRenewedValidatorConfigTx {
				return &SetAutoRenewedValidatorConfigTx{
					BaseTx: validBaseTx,
				}
			},
			wantErr: errMissingTxID,
		},
		{
			name: "too many restake shares",
			txFunc: func() *SetAutoRenewedValidatorConfigTx {
				autoCompoundRewardShares := uint32(2_000_000)

				return &SetAutoRenewedValidatorConfigTx{
					BaseTx:                   validBaseTx,
					TxID:                     ids.GenerateTestID(),
					AutoCompoundRewardShares: autoCompoundRewardShares,
				}
			},
			wantErr: errTooManyAutoCompoundRewardShares,
		},
		{
			name: "invalid auth",
			txFunc: func() *SetAutoRenewedValidatorConfigTx {
				var invalidAuth *secp256k1fx.Input

				return &SetAutoRenewedValidatorConfigTx{
					BaseTx:                   validBaseTx,
					TxID:                     ids.GenerateTestID(),
					Auth:                     invalidAuth,
					AutoCompoundRewardShares: reward.PercentDenominator,
					Period:                   0,
				}
			},
			wantErr: secp256k1fx.ErrNilInput,
		},
		{
			name: "invalid BaseTx",
			txFunc: func() *SetAutoRenewedValidatorConfigTx {
				autoCompoundRewardShares := uint32(500_000)

				return &SetAutoRenewedValidatorConfigTx{
					BaseTx:                   invalidBaseTx,
					TxID:                     ids.GenerateTestID(),
					AutoCompoundRewardShares: autoCompoundRewardShares,
				}
			},
			wantErr: avax.ErrWrongNetworkID,
		},
		{
			name: "valid tx",
			txFunc: func() *SetAutoRenewedValidatorConfigTx {
				validAuth := &secp256k1fx.Input{SigIndices: []uint32{0}}

				return &SetAutoRenewedValidatorConfigTx{
					BaseTx:                   validBaseTx,
					TxID:                     ids.GenerateTestID(),
					Auth:                     validAuth,
					AutoCompoundRewardShares: reward.PercentDenominator,
					Period:                   0,
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := tt.txFunc()
			gotErr := tx.SyntacticVerify(ctx)
			require.ErrorIs(t, gotErr, tt.wantErr)

			if tx != nil {
				require.Equal(t, tt.wantErr == nil, tx.SyntacticallyVerified)
			}
		})
	}
}
