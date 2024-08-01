// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestStaticCalculator(t *testing.T) {
	calculator := NewStaticCalculator(testStaticConfig)
	for _, test := range txTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			txBytes, err := hex.DecodeString(test.tx)
			require.NoError(err)

			tx, err := txs.Parse(txs.Codec, txBytes)
			require.NoError(err)

			fee, err := calculator.CalculateFee(tx.Unsigned)
			require.Equal(test.expectedStaticFee, fee)
			require.ErrorIs(err, test.expectedStaticFeeErr)
		})
	}
}
