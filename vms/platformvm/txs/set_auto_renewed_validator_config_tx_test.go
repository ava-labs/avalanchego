// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify/verifymock"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var errInvalidAuth = errors.New("invalid auth")

func TestSetAutoRenewedValidatorConfigTxSyntacticVerify(t *testing.T) {
	type test struct {
		name   string
		txFunc func(*gomock.Controller) *SetAutoRenewedValidatorConfigTx
		err    error
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
			txFunc: func(*gomock.Controller) *SetAutoRenewedValidatorConfigTx {
				return nil
			},
			err: ErrNilTx,
		},
		{
			name: "already verified",
			txFunc: func(*gomock.Controller) *SetAutoRenewedValidatorConfigTx {
				return &SetAutoRenewedValidatorConfigTx{
					BaseTx: verifiedBaseTx,
				}
			},
			err: nil,
		},
		{
			name: "empty txID",
			txFunc: func(*gomock.Controller) *SetAutoRenewedValidatorConfigTx {
				return &SetAutoRenewedValidatorConfigTx{
					BaseTx: validBaseTx,
				}
			},
			err: errMissingTxID,
		},
		{
			name: "too many restake shares",
			txFunc: func(*gomock.Controller) *SetAutoRenewedValidatorConfigTx {
				autoRestakeShares := uint32(2_000_000)

				return &SetAutoRenewedValidatorConfigTx{
					BaseTx:                   validBaseTx,
					TxID:                     ids.GenerateTestID(),
					AutoCompoundRewardShares: autoRestakeShares,
				}
			},
			err: errTooManyAutoCompoundRewardShares,
		},
		{
			name: "invalid auth",
			txFunc: func(ctrl *gomock.Controller) *SetAutoRenewedValidatorConfigTx {
				invalidAuth := verifymock.NewVerifiable(ctrl)
				invalidAuth.EXPECT().Verify().Return(errInvalidAuth)

				return &SetAutoRenewedValidatorConfigTx{
					BaseTx:                   validBaseTx,
					TxID:                     ids.GenerateTestID(),
					Auth:                     invalidAuth,
					AutoCompoundRewardShares: reward.PercentDenominator,
					Period:                   0,
				}
			},
			err: errInvalidAuth,
		},
		{
			name: "invalid BaseTx",
			txFunc: func(*gomock.Controller) *SetAutoRenewedValidatorConfigTx {
				autoRestakeShares := uint32(500_000)

				return &SetAutoRenewedValidatorConfigTx{
					BaseTx:                   invalidBaseTx,
					TxID:                     ids.GenerateTestID(),
					AutoCompoundRewardShares: autoRestakeShares,
				}
			},
			err: avax.ErrWrongNetworkID,
		},
		{
			name: "valid tx",
			txFunc: func(ctrl *gomock.Controller) *SetAutoRenewedValidatorConfigTx {
				validAuth := verifymock.NewVerifiable(ctrl)
				validAuth.EXPECT().Verify().Return(nil)

				return &SetAutoRenewedValidatorConfigTx{
					BaseTx:                   validBaseTx,
					TxID:                     ids.GenerateTestID(),
					Auth:                     validAuth,
					AutoCompoundRewardShares: reward.PercentDenominator,
					Period:                   0,
				}
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			tx := tt.txFunc(ctrl)
			err := tx.SyntacticVerify(ctx)
			require.ErrorIs(t, err, tt.err)

			if tx != nil {
				require.Equal(t, tt.err == nil, tx.SyntacticallyVerified)
			}
		})
	}
}
