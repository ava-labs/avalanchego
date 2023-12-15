// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package abi

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// Note: This file contains tests in addition to those found in go-ethereum.

const TEST_ABI = `[{"type":"function","name":"receive","inputs":[{"name":"sender","type":"address"},{"name":"amount","type":"uint256"},{"name":"memo","type":"bytes"}],"outputs":[{"internalType":"bool","name":"isAllowed","type":"bool"}]}]`

func TestUnpackInputIntoInterface(t *testing.T) {
	abi, err := JSON(strings.NewReader(TEST_ABI))
	require.NoError(t, err)

	type inputType struct {
		Sender common.Address
		Amount *big.Int
		Memo   []byte
	}
	input := inputType{
		Sender: common.HexToAddress("0x02"),
		Amount: big.NewInt(100),
		Memo:   []byte("hello"),
	}

	rawData, err := abi.Pack("receive", input.Sender, input.Amount, input.Memo)
	require.NoError(t, err)

	abi, err = JSON(strings.NewReader(TEST_ABI))
	require.NoError(t, err)

	for _, test := range []struct {
		name                   string
		extraPaddingBytes      int
		strictMode             bool
		expectedErrorSubstring string
	}{
		{
			name:       "No extra padding to input data",
			strictMode: true,
		},
		{
			name:              "Valid input data with 32 extra padding(%32) ",
			extraPaddingBytes: 32,
			strictMode:        true,
		},
		{
			name:              "Valid input data with 64 extra padding(%32)",
			extraPaddingBytes: 64,
			strictMode:        true,
		},
		{
			name:                   "Valid input data with extra padding indivisible by 32",
			extraPaddingBytes:      33,
			strictMode:             true,
			expectedErrorSubstring: "abi: improperly formatted input:",
		},
		{
			name:              "Valid input data with extra padding indivisible by 32, no strict mode",
			extraPaddingBytes: 33,
			strictMode:        false,
		},
	} {
		{
			t.Run(test.name, func(t *testing.T) {
				// skip 4 byte selector
				data := rawData[4:]
				// Add extra padding to data
				data = append(data, make([]byte, test.extraPaddingBytes)...)

				// Unpack into interface
				var v inputType
				err = abi.UnpackInputIntoInterface(&v, "receive", data, test.strictMode) // skips 4 byte selector

				if test.expectedErrorSubstring != "" {
					require.Error(t, err)
					require.ErrorContains(t, err, test.expectedErrorSubstring)
				} else {
					require.NoError(t, err)
					// Verify unpacked values match input
					require.Equal(t, v.Amount, input.Amount)
					require.EqualValues(t, v.Amount, input.Amount)
					require.True(t, bytes.Equal(v.Memo, input.Memo))
				}
			})
		}
	}
}

func TestPackOutput(t *testing.T) {
	abi, err := JSON(strings.NewReader(TEST_ABI))
	require.NoError(t, err)

	bytes, err := abi.PackOutput("receive", true)
	require.NoError(t, err)

	vals, err := abi.Methods["receive"].Outputs.Unpack(bytes)
	require.NoError(t, err)

	require.Len(t, vals, 1)
	require.True(t, vals[0].(bool))
}
