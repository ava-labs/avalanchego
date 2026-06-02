// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestRewardAutoRenewedValidatorTxSyntacticVerify(t *testing.T) {
	tests := []struct {
		name string
		tx   *RewardAutoRenewedValidatorTx
		want error
	}{
		{
			name: "nil",
			tx:   nil,
			want: ErrNilTx,
		},
		{
			name: "missing_timestamp",
			tx: &RewardAutoRenewedValidatorTx{
				TxID:      ids.GenerateTestID(),
				Timestamp: 0,
			},
			want: errMissingTimestamp,
		},
		{
			name: "missing_tx_id",
			tx: &RewardAutoRenewedValidatorTx{
				Timestamp: 1,
			},
			want: errMissingTxID,
		},
		{
			name: "valid",
			tx: &RewardAutoRenewedValidatorTx{
				TxID:      ids.GenerateTestID(),
				Timestamp: 1,
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tx.SyntacticVerify(nil)
			require.ErrorIs(t, got, tt.want)
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
