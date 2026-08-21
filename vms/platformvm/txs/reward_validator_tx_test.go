// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestRewardValidatorTxSyntacticVerify(t *testing.T) {
	tests := []struct {
		name string
		tx   *RewardValidatorTx
		want error
	}{
		{
			name: "nil",
			tx:   nil,
			want: ErrNilTx,
		},
		{
			name: "missing_tx_id",
			tx:   &RewardValidatorTx{},
			want: errMissingStakerTxID,
		},
		{
			name: "valid",
			tx: &RewardValidatorTx{
				TxID: ids.GenerateTestID(),
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

func TestRewardValidatorTxSerialization(t *testing.T) {
	require := require.New(t)

	txID := ids.ID{
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	}

	rewardTx := &RewardValidatorTx{
		TxID: txID,
	}

	wantBytes := []byte{
		// Codec version
		0x00, 0x00,
		// RewardValidatorTx type ID
		0x00, 0x00, 0x00, 0x14,
		// Referenced validator TxID
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	}

	var unsignedTx UnsignedTx = rewardTx
	gotBytes, err := Codec.Marshal(CodecVersion, &unsignedTx)
	require.NoError(err)
	require.Equal(wantBytes, gotBytes)
}
