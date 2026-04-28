// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestRewardAutoRenewedValidatorTxSyntacticVerify(t *testing.T) {
	type test struct {
		name    string
		txFunc  func() *RewardAutoRenewedValidatorTx
		wantErr error
	}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func() *RewardAutoRenewedValidatorTx {
				return nil
			},
			wantErr: ErrNilTx,
		},
		{
			name: "missing timestamp",
			txFunc: func() *RewardAutoRenewedValidatorTx {
				return &RewardAutoRenewedValidatorTx{
					TxID:      ids.GenerateTestID(),
					Timestamp: 0,
				}
			},
			wantErr: errMissingTimestamp,
		},
		{
			name: "missing tx id",
			txFunc: func() *RewardAutoRenewedValidatorTx {
				return &RewardAutoRenewedValidatorTx{
					Timestamp: 1,
				}
			},
			wantErr: errMissingTxID,
		},
		{
			name: "valid auto-renewed validator",
			txFunc: func() *RewardAutoRenewedValidatorTx {
				return &RewardAutoRenewedValidatorTx{
					TxID:      ids.GenerateTestID(),
					Timestamp: 1,
				}
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := tt.txFunc()
			gotErr := tx.SyntacticVerify(nil)
			require.ErrorIs(t, gotErr, tt.wantErr)
		})
	}
}
