// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

func TestRewardAutoRenewedValidatorTxSyntacticVerify(t *testing.T) {
	type test struct {
		name   string
		txFunc func(*gomock.Controller) *RewardAutoRenewedValidatorTx
		err    error
	}

	ctx := &snow.Context{
		ChainID:   ids.GenerateTestID(),
		NetworkID: uint32(1337),
	}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func(*gomock.Controller) *RewardAutoRenewedValidatorTx {
				return nil
			},
			err: ErrNilTx,
		},
		{
			name: "missing timestamp",
			txFunc: func(*gomock.Controller) *RewardAutoRenewedValidatorTx {
				return &RewardAutoRenewedValidatorTx{
					TxID:      ids.GenerateTestID(),
					Timestamp: 0,
				}
			},
			err: errMissingTimestamp,
		},
		{
			name: "missing tx id",
			txFunc: func(*gomock.Controller) *RewardAutoRenewedValidatorTx {
				return &RewardAutoRenewedValidatorTx{
					Timestamp: 1,
				}
			},
			err: errMissingTxID,
		},
		{
			name: "valid auto-renewed validator",
			txFunc: func(*gomock.Controller) *RewardAutoRenewedValidatorTx {
				return &RewardAutoRenewedValidatorTx{
					TxID:      ids.GenerateTestID(),
					Timestamp: 1,
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
		})
	}
}

func TestAddAutoRenewedValidatorIsUnsignedTx(t *testing.T) {
	txIntf := any((*RewardAutoRenewedValidatorTx)(nil))
	_, ok := txIntf.(UnsignedTx)
	require.True(t, ok)
}
