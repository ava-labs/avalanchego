// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/ava-labs/libevm/accounts/abi"
)

var mintSignature = contract.CalculateFunctionSelector("mintNativeCoin(address,uint256)") // address, amount

func FuzzPackMintNativeCoinTest(f *testing.F) {
	key, err := crypto.GenerateKey()
	require.NoError(f, err)
	addr := crypto.PubkeyToAddress(key.PublicKey)
	testAddrBytes := addr.Bytes()
	f.Add(testAddrBytes, common.Big0.Bytes())
	f.Add(testAddrBytes, common.Big1.Bytes())
	f.Add(testAddrBytes, abi.MaxUint256.Bytes())
	f.Add(testAddrBytes, new(big.Int).Sub(abi.MaxUint256, common.Big1).Bytes())
	f.Add(testAddrBytes, new(big.Int).Add(abi.MaxUint256, common.Big1).Bytes())
	f.Add(constants.BlackholeAddr.Bytes(), common.Big2.Bytes())
	f.Fuzz(func(t *testing.T, b []byte, bigIntBytes []byte) {
		bigIntVal := new(big.Int).SetBytes(bigIntBytes)
		// we can only check if outputs are correct if the value is less than MaxUint256
		// otherwise the value will be truncated when packed,
		// and thus unpacked output will not be equal to the value
		doCheckOutputs := bigIntVal.Cmp(abi.MaxUint256) <= 0
		testPackMintNativeCoin(t, common.BytesToAddress(b), bigIntVal, doCheckOutputs)
	})
}

func testPackMintNativeCoin(t *testing.T, addr common.Address, amount *big.Int, checkOutputs bool) {
	t.Helper()
	t.Run(fmt.Sprintf("TestPackMintNativeCoin, addr: %s, amount: %s", addr.String(), amount.String()), func(t *testing.T) {
		input, err := PackMintNativeCoin(addr, amount)
		if err != nil {
			return
		}

		input = input[4:]
		unpackedAddr, unpackedAmount, err := UnpackMintNativeCoinInput(input, true)
		require.NoError(t, err)
		if checkOutputs {
			require.Equal(t, addr, unpackedAddr)
			require.Equal(t, amount.Bytes(), unpackedAmount.Bytes())
		}
	})
}

func TestUnpackMintNativeCoinInput(t *testing.T) {
	testInputBytes, err := PackMintNativeCoin(constants.BlackholeAddr, common.Big2)
	require.NoError(t, err)
	// exclude 4 bytes for function selector
	testInputBytes = testInputBytes[4:]
	tests := []struct {
		name           string
		input          []byte
		strictMode     bool
		expectedErr    error
		expectedAddr   common.Address
		expectedAmount *big.Int
	}{
		{
			name:        "empty input strict mode",
			input:       []byte{},
			strictMode:  true,
			expectedErr: ErrInvalidLen,
		},
		{
			name:        "empty input",
			input:       []byte{},
			strictMode:  false,
			expectedErr: ErrUnpackInput,
		},
		{
			name:        "input with extra bytes strict mode",
			input:       append(testInputBytes, make([]byte, 32)...),
			strictMode:  true,
			expectedErr: ErrInvalidLen,
		},
		{
			name:           "input with extra bytes",
			input:          append(testInputBytes, make([]byte, 32)...),
			strictMode:     false,
			expectedErr:    nil,
			expectedAddr:   constants.BlackholeAddr,
			expectedAmount: common.Big2,
		},
		{
			name:        "input with extra bytes (not divisible by 32) strict mode",
			input:       append(testInputBytes, make([]byte, 33)...),
			strictMode:  true,
			expectedErr: ErrInvalidLen,
		},
		{
			name:           "input with extra bytes (not divisible by 32)",
			input:          append(testInputBytes, make([]byte, 33)...),
			strictMode:     false,
			expectedErr:    nil,
			expectedAddr:   constants.BlackholeAddr,
			expectedAmount: common.Big2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unpackedAddress, unpackedAmount, err := UnpackMintNativeCoinInput(test.input, test.strictMode)
			require.ErrorIs(t, err, test.expectedErr)
			if test.expectedErr == nil {
				require.Equal(t, test.expectedAddr, unpackedAddress)
				require.Equal(t, test.expectedAmount, unpackedAmount, "expected %s, got %s", test.expectedAmount.String(), unpackedAmount.String())
			}
		})
	}
}

func TestFunctionSignatures(t *testing.T) {
	// Test that the mintNativeCoin signature is correct
	abiMintNativeCoin := NativeMinterABI.Methods["mintNativeCoin"]
	require.Equal(t, mintSignature, abiMintNativeCoin.ID)
}
