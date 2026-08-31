// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestDynamicCalculator(t *testing.T) {
	calculator := NewDynamicCalculator(testDynamicWeights, testDynamicPrice)
	for _, test := range txTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			txBytes, err := hex.DecodeString(test.tx)
			require.NoError(err)

			tx, err := txs.Parse(txs.Codec, txBytes)
			require.NoError(err)

			fee, err := calculator.CalculateFee(tx.Unsigned)
			require.Equal(int(test.expectedDynamicFee), int(fee))
			require.ErrorIs(err, test.expectedDynamicFeeErr)
		})
	}
}

// A warp credential must cost more than the secp256k1 signatures the input
// pricing assumes; a secp credential must cost nothing extra.
func TestWithCredentials(t *testing.T) {
	require := require.New(t)
	calculator := NewDynamicCalculator(testDynamicWeights, testDynamicPrice)

	var unsigned txs.UnsignedTx
	for _, test := range txTests {
		if test.name == "BaseTx" {
			txBytes, err := hex.DecodeString(test.tx)
			require.NoError(err)
			tx, err := txs.Parse(txs.Codec, txBytes)
			require.NoError(err)
			unsigned = tx.Unsigned
		}
	}
	require.NotNil(unsigned)

	secpFee, err := calculator.CalculateFee(unsigned)
	require.NoError(err)

	sameFee, err := WithCredentials(
		calculator,
		[]verify.Verifiable{&secp256k1fx.Credential{}},
	).CalculateFee(unsigned)
	require.NoError(err)
	require.Equal(secpFee, sameFee)

	unsignedMsg, err := warp.NewUnsignedMessage(1, ids.GenerateTestID(), []byte("payload"))
	require.NoError(err)
	msg, err := warp.NewMessage(unsignedMsg, &warp.BitSetSignature{Signers: set.NewBits(0).Bytes()})
	require.NoError(err)

	warpFee, err := WithCredentials(
		calculator,
		[]verify.Verifiable{&secp256k1fx.WarpCredential{Message: msg.Bytes()}},
	).CalculateFee(unsigned)
	require.NoError(err)
	require.Greater(warpFee, secpFee)
	t.Logf("secp fee %d nAVAX, warp credential fee %d nAVAX", secpFee, warpFee)
}
