// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/constants"
	"github.com/ava-labs/subnet-evm/precompile/contract"
)

var mintSignature = contract.CalculateFunctionSelector("mintNativeCoin(address,uint256)") // address, amount

func FuzzPackMintNativeCoinEqualTest(f *testing.F) {
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
		doCheckOutputs := true
		// we can only check if outputs are correct if the value is less than MaxUint256
		// otherwise the value will be truncated when packed,
		// and thus unpacked output will not be equal to the value
		if bigIntVal.Cmp(abi.MaxUint256) > 0 {
			doCheckOutputs = false
		}
		testOldPackMintNativeCoinEqual(t, common.BytesToAddress(b), bigIntVal, doCheckOutputs)
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
		expectedErr    string
		expectedOldErr string
		expectedAddr   common.Address
		expectedAmount *big.Int
	}{
		{
			name:           "empty input strict mode",
			input:          []byte{},
			strictMode:     true,
			expectedErr:    ErrInvalidLen.Error(),
			expectedOldErr: ErrInvalidLen.Error(),
		},
		{
			name:           "empty input",
			input:          []byte{},
			strictMode:     false,
			expectedErr:    "attempting to unmarshal an empty string",
			expectedOldErr: ErrInvalidLen.Error(),
		},
		{
			name:           "input with extra bytes strict mode",
			input:          append(testInputBytes, make([]byte, 32)...),
			strictMode:     true,
			expectedErr:    ErrInvalidLen.Error(),
			expectedOldErr: ErrInvalidLen.Error(),
		},
		{
			name:           "input with extra bytes",
			input:          append(testInputBytes, make([]byte, 32)...),
			strictMode:     false,
			expectedErr:    "",
			expectedOldErr: ErrInvalidLen.Error(),
			expectedAddr:   constants.BlackholeAddr,
			expectedAmount: common.Big2,
		},
		{
			name:           "input with extra bytes (not divisible by 32) strict mode",
			input:          append(testInputBytes, make([]byte, 33)...),
			strictMode:     true,
			expectedErr:    ErrInvalidLen.Error(),
			expectedOldErr: ErrInvalidLen.Error(),
		},
		{
			name:           "input with extra bytes (not divisible by 32)",
			input:          append(testInputBytes, make([]byte, 33)...),
			strictMode:     false,
			expectedAddr:   constants.BlackholeAddr,
			expectedAmount: common.Big2,
			expectedOldErr: ErrInvalidLen.Error(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unpackedAddress, unpackedAmount, err := UnpackMintNativeCoinInput(test.input, test.strictMode)
			if test.expectedErr != "" {
				require.ErrorContains(t, err, test.expectedErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.expectedAddr, unpackedAddress)
				require.True(t, test.expectedAmount.Cmp(unpackedAmount) == 0, "expected %s, got %s", test.expectedAmount.String(), unpackedAmount.String())
			}
			oldUnpackedAddress, oldUnpackedAmount, oldErr := OldUnpackMintNativeCoinInput(test.input)
			if test.expectedOldErr != "" {
				require.ErrorContains(t, oldErr, test.expectedOldErr)
			} else {
				require.NoError(t, oldErr)
				require.Equal(t, test.expectedAddr, oldUnpackedAddress)
				require.True(t, test.expectedAmount.Cmp(oldUnpackedAmount) == 0, "expected %s, got %s", test.expectedAmount.String(), oldUnpackedAmount.String())
			}
		})
	}
}

func TestFunctionSignatures(t *testing.T) {
	// Test that the mintNativeCoin signature is correct
	abiMintNativeCoin := NativeMinterABI.Methods["mintNativeCoin"]
	require.Equal(t, mintSignature, abiMintNativeCoin.ID)
}

func testOldPackMintNativeCoinEqual(t *testing.T, addr common.Address, amount *big.Int, checkOutputs bool) {
	t.Helper()
	t.Run(fmt.Sprintf("TestUnpackAndPacks, addr: %s, amount: %s", addr.String(), amount.String()), func(t *testing.T) {
		input, err := OldPackMintNativeCoinInput(addr, amount)
		input2, err2 := PackMintNativeCoin(addr, amount)
		if err != nil {
			require.ErrorContains(t, err2, err.Error())
			return
		}
		require.NoError(t, err2)
		require.Equal(t, input, input2)

		input = input[4:]
		to, assetAmount, err := OldUnpackMintNativeCoinInput(input)
		unpackedAddr, unpackedAmount, err2 := UnpackMintNativeCoinInput(input, true)
		if err != nil {
			require.ErrorContains(t, err2, err.Error())
			return
		}
		require.NoError(t, err2)
		require.Equal(t, to, unpackedAddr)
		require.Equal(t, assetAmount.Bytes(), unpackedAmount.Bytes())
		if checkOutputs {
			require.Equal(t, addr, to)
			require.Equal(t, amount.Bytes(), assetAmount.Bytes())
		}
	})
}

func OldPackMintNativeCoinInput(address common.Address, amount *big.Int) ([]byte, error) {
	// function selector (4 bytes) + input(hash for address + hash for amount)
	res := make([]byte, contract.SelectorLen+mintInputLen)
	err := contract.PackOrderedHashesWithSelector(res, mintSignature, []common.Hash{
		common.BytesToHash(address[:]),
		common.BigToHash(amount),
	})

	return res, err
}

func OldUnpackMintNativeCoinInput(input []byte) (common.Address, *big.Int, error) {
	mintInputAddressSlot := 0
	mintInputAmountSlot := 1
	if len(input) != mintInputLen {
		return common.Address{}, nil, fmt.Errorf("%w: %d", ErrInvalidLen, len(input))
	}
	to := common.BytesToAddress(contract.PackedHash(input, mintInputAddressSlot))
	assetAmount := new(big.Int).SetBytes(contract.PackedHash(input, mintInputAmountSlot))
	return to, assetAmount, nil
}
