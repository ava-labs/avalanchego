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

func TestRewardAutoRenewedValidatorTxSerialization(t *testing.T) {
	require := require.New(t)

	txID := ids.ID{
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	}

	rewardTx := &RewardAutoRenewedValidatorTx{
		TxID:      txID,
		Timestamp: 1234567890,
	}

	wantBytes := []byte{
		// Codec version
		0x00, 0x00,
		// RewardAutoRenewedValidatorTx type ID
		0x00, 0x00, 0x00, 0x2a,
		// Referenced auto-renewed validator TxID
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		// Timestamp (1234567890)
		0x00, 0x00, 0x00, 0x00, 0x49, 0x96, 0x02, 0xd2,
	}

	var unsignedTx UnsignedTx = rewardTx
	gotBytes, err := Codec.Marshal(CodecVersion, &unsignedTx)
	require.NoError(err)
	require.Equal(wantBytes, gotBytes)
}
