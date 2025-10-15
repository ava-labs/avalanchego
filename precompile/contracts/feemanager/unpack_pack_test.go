// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/precompile/contract"
)

var (
	setFeeConfigSignature              = contract.CalculateFunctionSelector("setFeeConfig(uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256)")
	getFeeConfigSignature              = contract.CalculateFunctionSelector("getFeeConfig()")
	getFeeConfigLastChangedAtSignature = contract.CalculateFunctionSelector("getFeeConfigLastChangedAt()")
)

func FuzzPackGetFeeConfigOutputEqualTest(f *testing.F) {
	f.Add([]byte{}, uint64(0))
	f.Add(big.NewInt(0).Bytes(), uint64(0))
	f.Add(big.NewInt(1).Bytes(), uint64(math.MaxUint64))
	f.Add(math.MaxBig256.Bytes(), uint64(0))
	f.Add(math.MaxBig256.Sub(math.MaxBig256, common.Big1).Bytes(), uint64(0))
	f.Add(math.MaxBig256.Add(math.MaxBig256, common.Big1).Bytes(), uint64(0))
	f.Fuzz(func(t *testing.T, bigIntBytes []byte, blockRate uint64) {
		bigIntVal := new(big.Int).SetBytes(bigIntBytes)
		feeConfig := commontype.FeeConfig{
			GasLimit:                 bigIntVal,
			TargetBlockRate:          blockRate,
			MinBaseFee:               bigIntVal,
			TargetGas:                bigIntVal,
			BaseFeeChangeDenominator: bigIntVal,
			MinBlockGasCost:          bigIntVal,
			MaxBlockGasCost:          bigIntVal,
			BlockGasCostStep:         bigIntVal,
		}
		// we can only check if outputs are correct if the value is less than MaxUint256
		// otherwise the value will be truncated when packed,
		// and thus unpacked output will not be equal to the value
		doCheckOutputs := bigIntVal.Cmp(abi.MaxUint256) <= 0
		testOldPackGetFeeConfigOutputEqual(t, feeConfig, doCheckOutputs)
	})
}

func TestOldPackGetFeeConfigOutputEqual(t *testing.T) {
	testOldPackGetFeeConfigOutputEqual(t, testFeeConfig, true)
}

func TestPackGetFeeConfigOutputPanic(t *testing.T) {
	require.Panics(t, func() {
		_, _ = OldPackFeeConfig(commontype.FeeConfig{})
	})
	require.Panics(t, func() {
		_, _ = PackGetFeeConfigOutput(commontype.FeeConfig{})
	})
}

func TestPackGetFeeConfigOutput(t *testing.T) {
	testInputBytes, err := PackGetFeeConfigOutput(testFeeConfig)
	require.NoError(t, err)
	tests := []struct {
		name           string
		input          []byte
		skipLenCheck   bool
		expectedErr    string
		expectedOldErr string
		expectedOutput commontype.FeeConfig
	}{
		{
			name:           "empty input",
			input:          []byte{},
			skipLenCheck:   false,
			expectedErr:    ErrInvalidLen.Error(),
			expectedOldErr: ErrInvalidLen.Error(),
		},
		{
			name:           "empty input skip len check",
			input:          []byte{},
			skipLenCheck:   true,
			expectedErr:    "attempting to unmarshal an empty string",
			expectedOldErr: ErrInvalidLen.Error(),
		},
		{
			name:           "input with extra bytes",
			input:          append(testInputBytes, make([]byte, 32)...),
			skipLenCheck:   false,
			expectedErr:    ErrInvalidLen.Error(),
			expectedOldErr: ErrInvalidLen.Error(),
		},
		{
			name:           "input with extra bytes skip len check",
			input:          append(testInputBytes, make([]byte, 32)...),
			skipLenCheck:   true,
			expectedErr:    "",
			expectedOldErr: ErrInvalidLen.Error(),
			expectedOutput: testFeeConfig,
		},
		{
			name:           "input with extra bytes (not divisible by 32)",
			input:          append(testInputBytes, make([]byte, 33)...),
			skipLenCheck:   false,
			expectedErr:    ErrInvalidLen.Error(),
			expectedOldErr: ErrInvalidLen.Error(),
		},
		{
			name:           "input with extra bytes (not divisible by 32) skip len check",
			input:          append(testInputBytes, make([]byte, 33)...),
			skipLenCheck:   true,
			expectedErr:    "improperly formatted output",
			expectedOldErr: ErrInvalidLen.Error(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unpacked, err := UnpackGetFeeConfigOutput(test.input, test.skipLenCheck)
			if test.expectedErr != "" {
				require.ErrorContains(t, err, test.expectedErr)
			} else {
				require.NoError(t, err)
				require.True(t, test.expectedOutput.Equal(&unpacked), "not equal: expectedOutput %v, unpacked %v", test.expectedOutput, unpacked)
			}
			oldUnpacked, oldErr := OldUnpackFeeConfig(test.input)
			if test.expectedOldErr != "" {
				require.ErrorContains(t, oldErr, test.expectedOldErr)
			} else {
				require.NoError(t, oldErr)
				require.True(t, test.expectedOutput.Equal(&oldUnpacked), "not equal: expectedOutput %v, oldUnpacked %v", test.expectedOutput, oldUnpacked)
			}
		})
	}
}

func TestGetFeeConfig(t *testing.T) {
	// Compare OldPackGetFeeConfigInput vs PackGetFeeConfig
	// to see if they are equivalent
	input := OldPackGetFeeConfigInput()

	input2, err := PackGetFeeConfig()
	require.NoError(t, err)

	require.Equal(t, input, input2)
}

func TestGetLastChangedAtInput(t *testing.T) {
	// Compare OldPackGetFeeConfigInput vs PackGetFeeConfigLastChangedAt
	// to see if they are equivalent

	input := OldPackGetLastChangedAtInput()

	input2, err := PackGetFeeConfigLastChangedAt()
	require.NoError(t, err)

	require.Equal(t, input, input2)
}

func FuzzPackGetLastChangedAtOutput(f *testing.F) {
	f.Add([]byte{})
	f.Add(big.NewInt(0).Bytes())
	f.Add(big.NewInt(1).Bytes())
	f.Add(math.MaxBig256.Bytes())
	f.Add(math.MaxBig256.Sub(math.MaxBig256, common.Big1).Bytes())
	f.Add(math.MaxBig256.Add(math.MaxBig256, common.Big1).Bytes())
	f.Fuzz(func(t *testing.T, bigIntBytes []byte) {
		bigIntVal := new(big.Int).SetBytes(bigIntBytes)
		// we can only check if outputs are correct if the value is less than MaxUint256
		// otherwise the value will be truncated when packed,
		// and thus unpacked output will not be equal to the value
		doCheckOutputs := bigIntVal.Cmp(abi.MaxUint256) <= 0
		testOldPackGetLastChangedAtOutputEqual(t, bigIntVal, doCheckOutputs)
	})
}

func FuzzPackSetFeeConfigEqualTest(f *testing.F) {
	f.Add([]byte{}, uint64(0))
	f.Add(big.NewInt(0).Bytes(), uint64(0))
	f.Add(big.NewInt(1).Bytes(), uint64(math.MaxUint64))
	f.Add(math.MaxBig256.Bytes(), uint64(0))
	f.Add(math.MaxBig256.Sub(math.MaxBig256, common.Big1).Bytes(), uint64(0))
	f.Add(math.MaxBig256.Add(math.MaxBig256, common.Big1).Bytes(), uint64(0))
	f.Fuzz(func(t *testing.T, bigIntBytes []byte, blockRate uint64) {
		bigIntVal := new(big.Int).SetBytes(bigIntBytes)
		feeConfig := commontype.FeeConfig{
			GasLimit:                 bigIntVal,
			TargetBlockRate:          blockRate,
			MinBaseFee:               bigIntVal,
			TargetGas:                bigIntVal,
			BaseFeeChangeDenominator: bigIntVal,
			MinBlockGasCost:          bigIntVal,
			MaxBlockGasCost:          bigIntVal,
			BlockGasCostStep:         bigIntVal,
		}
		// we can only check if outputs are correct if the value is less than MaxUint256
		// otherwise the value will be truncated when packed,
		// and thus unpacked output will not be equal to the value
		doCheckOutputs := bigIntVal.Cmp(abi.MaxUint256) <= 0
		testOldPackSetFeeConfigInputEqual(t, feeConfig, doCheckOutputs)
	})
}

func TestOldPackSetFeeConfigInputEqual(t *testing.T) {
	testOldPackSetFeeConfigInputEqual(t, testFeeConfig, true)
}

func TestPackSetFeeConfigInputPanic(t *testing.T) {
	require.Panics(t, func() {
		_, _ = OldPackSetFeeConfig(commontype.FeeConfig{})
	})
	require.Panics(t, func() {
		_, _ = PackSetFeeConfig(commontype.FeeConfig{})
	})
}

func TestPackSetFeeConfigInput(t *testing.T) {
	testInputBytes, err := PackSetFeeConfig(testFeeConfig)
	require.NoError(t, err)
	// exclude 4 bytes for function selector
	testInputBytes = testInputBytes[4:]
	tests := []struct {
		name           string
		input          []byte
		strictMode     bool
		expectedErr    string
		expectedOldErr string
		expectedOutput commontype.FeeConfig
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
			name:           "input with insufficient len strict mode",
			input:          []byte{123},
			strictMode:     true,
			expectedErr:    ErrInvalidLen.Error(),
			expectedOldErr: ErrInvalidLen.Error(),
		},
		{
			name:           "input with insufficient len",
			input:          []byte{123},
			strictMode:     false,
			expectedErr:    "length insufficient",
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
			expectedOutput: testFeeConfig,
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
			expectedOutput: testFeeConfig,
			expectedOldErr: ErrInvalidLen.Error(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unpacked, err := UnpackSetFeeConfigInput(test.input, test.strictMode)
			if test.expectedErr != "" {
				require.ErrorContains(t, err, test.expectedErr)
			} else {
				require.NoError(t, err)
				require.True(t, test.expectedOutput.Equal(&unpacked), "not equal: expectedOutput %v, unpacked %v", test.expectedOutput, unpacked)
			}
			oldUnpacked, oldErr := OldUnpackFeeConfig(test.input)
			if test.expectedOldErr != "" {
				require.ErrorContains(t, oldErr, test.expectedOldErr)
			} else {
				require.NoError(t, oldErr)
				require.True(t, test.expectedOutput.Equal(&oldUnpacked), "not equal: expectedOutput %v, oldUnpacked %v", test.expectedOutput, oldUnpacked)
			}
		})
	}
}

func TestFunctionSignatures(t *testing.T) {
	abiSetFeeConfig := FeeManagerABI.Methods["setFeeConfig"]
	require.Equal(t, setFeeConfigSignature, abiSetFeeConfig.ID)

	abiGetFeeConfig := FeeManagerABI.Methods["getFeeConfig"]
	require.Equal(t, getFeeConfigSignature, abiGetFeeConfig.ID)

	abiGetFeeConfigLastChangedAt := FeeManagerABI.Methods["getFeeConfigLastChangedAt"]
	require.Equal(t, getFeeConfigLastChangedAtSignature, abiGetFeeConfigLastChangedAt.ID)
}

func testOldPackGetFeeConfigOutputEqual(t *testing.T, feeConfig commontype.FeeConfig, checkOutputs bool) {
	t.Helper()
	t.Run(fmt.Sprintf("TestGetFeeConfigOutput, feeConfig %v", feeConfig), func(t *testing.T) {
		input, err := OldPackFeeConfig(feeConfig)
		input2, err2 := PackGetFeeConfigOutput(feeConfig)
		if err != nil {
			require.ErrorContains(t, err2, err.Error())
			return
		}
		require.NoError(t, err2)
		require.Equal(t, input, input2)

		config, err := OldUnpackFeeConfig(input)
		unpacked, err2 := UnpackGetFeeConfigOutput(input, false)
		if err != nil {
			require.ErrorContains(t, err2, err.Error())
			return
		}
		require.NoError(t, err2)
		require.True(t, config.Equal(&unpacked), "not equal: config %v, unpacked %v", feeConfig, unpacked)
		if checkOutputs {
			require.True(t, feeConfig.Equal(&unpacked), "not equal: feeConfig %v, unpacked %v", feeConfig, unpacked)
		}
	})
}

func testOldPackGetLastChangedAtOutputEqual(t *testing.T, blockNumber *big.Int, checkOutputs bool) {
	t.Helper()
	t.Run(fmt.Sprintf("TestGetLastChangedAtOutput, blockNumber %v", blockNumber), func(t *testing.T) {
		input := OldPackGetLastChangedAtOutput(blockNumber)
		input2, err2 := PackGetFeeConfigLastChangedAtOutput(blockNumber)
		require.NoError(t, err2)
		require.Equal(t, input, input2)

		value, err := OldUnpackGetLastChangedAtOutput(input)
		unpacked, err2 := UnpackGetFeeConfigLastChangedAtOutput(input)
		if err != nil {
			require.ErrorContains(t, err2, err.Error())
			return
		}
		require.NoError(t, err2)
		require.True(t, value.Cmp(unpacked) == 0, "not equal: value %v, unpacked %v", value, unpacked)
		if checkOutputs {
			require.True(t, blockNumber.Cmp(unpacked) == 0, "not equal: blockNumber %v, unpacked %v", blockNumber, unpacked)
		}
	})
}

func testOldPackSetFeeConfigInputEqual(t *testing.T, feeConfig commontype.FeeConfig, checkOutputs bool) {
	t.Helper()
	t.Run(fmt.Sprintf("TestSetFeeConfigInput, feeConfig %v", feeConfig), func(t *testing.T) {
		input, err := OldPackSetFeeConfig(feeConfig)
		input2, err2 := PackSetFeeConfig(feeConfig)
		if err != nil {
			require.ErrorContains(t, err2, err.Error())
			return
		}
		require.NoError(t, err2)
		require.Equal(t, input, input2)

		value, err := OldUnpackFeeConfig(input)
		unpacked, err2 := UnpackSetFeeConfigInput(input, true)
		if err != nil {
			require.ErrorContains(t, err2, err.Error())
			return
		}
		require.NoError(t, err2)
		require.True(t, value.Equal(&unpacked), "not equal: value %v, unpacked %v", value, unpacked)
		if checkOutputs {
			require.True(t, feeConfig.Equal(&unpacked), "not equal: feeConfig %v, unpacked %v", feeConfig, unpacked)
		}
	})
}

func OldPackFeeConfig(feeConfig commontype.FeeConfig) ([]byte, error) {
	return packFeeConfigHelper(feeConfig, false)
}

func OldUnpackFeeConfig(input []byte) (commontype.FeeConfig, error) {
	if len(input) != feeConfigInputLen {
		return commontype.FeeConfig{}, fmt.Errorf("%w: %d", ErrInvalidLen, len(input))
	}
	feeConfig := commontype.FeeConfig{}
	for i := minFeeConfigFieldKey; i <= numFeeConfigField; i++ {
		listIndex := i - 1
		packedElement := contract.PackedHash(input, listIndex)
		switch i {
		case gasLimitKey:
			feeConfig.GasLimit = new(big.Int).SetBytes(packedElement)
		case targetBlockRateKey:
			feeConfig.TargetBlockRate = new(big.Int).SetBytes(packedElement).Uint64()
		case minBaseFeeKey:
			feeConfig.MinBaseFee = new(big.Int).SetBytes(packedElement)
		case targetGasKey:
			feeConfig.TargetGas = new(big.Int).SetBytes(packedElement)
		case baseFeeChangeDenominatorKey:
			feeConfig.BaseFeeChangeDenominator = new(big.Int).SetBytes(packedElement)
		case minBlockGasCostKey:
			feeConfig.MinBlockGasCost = new(big.Int).SetBytes(packedElement)
		case maxBlockGasCostKey:
			feeConfig.MaxBlockGasCost = new(big.Int).SetBytes(packedElement)
		case blockGasCostStepKey:
			feeConfig.BlockGasCostStep = new(big.Int).SetBytes(packedElement)
		default:
			// This should never encounter an unknown fee config key
			panic(fmt.Sprintf("unknown fee config key: %d", i))
		}
	}
	return feeConfig, nil
}

func packFeeConfigHelper(feeConfig commontype.FeeConfig, useSelector bool) ([]byte, error) {
	hashes := []common.Hash{
		common.BigToHash(feeConfig.GasLimit),
		common.BigToHash(new(big.Int).SetUint64(feeConfig.TargetBlockRate)),
		common.BigToHash(feeConfig.MinBaseFee),
		common.BigToHash(feeConfig.TargetGas),
		common.BigToHash(feeConfig.BaseFeeChangeDenominator),
		common.BigToHash(feeConfig.MinBlockGasCost),
		common.BigToHash(feeConfig.MaxBlockGasCost),
		common.BigToHash(feeConfig.BlockGasCostStep),
	}

	if useSelector {
		res := make([]byte, len(setFeeConfigSignature)+feeConfigInputLen)
		err := contract.PackOrderedHashesWithSelector(res, setFeeConfigSignature, hashes)
		return res, err
	}

	res := make([]byte, len(hashes)*common.HashLength)
	err := contract.PackOrderedHashes(res, hashes)
	return res, err
}

// PackGetFeeConfigInput packs the getFeeConfig signature
func OldPackGetFeeConfigInput() []byte {
	return getFeeConfigSignature
}

// PackGetLastChangedAtInput packs the getFeeConfigLastChangedAt signature
func OldPackGetLastChangedAtInput() []byte {
	return getFeeConfigLastChangedAtSignature
}

func OldPackGetLastChangedAtOutput(lastChangedAt *big.Int) []byte {
	return common.BigToHash(lastChangedAt).Bytes()
}

func OldUnpackGetLastChangedAtOutput(input []byte) (*big.Int, error) {
	return new(big.Int).SetBytes(input), nil
}

func OldPackSetFeeConfig(feeConfig commontype.FeeConfig) ([]byte, error) {
	// function selector (4 bytes) + input(feeConfig)
	return packFeeConfigHelper(feeConfig, true)
}
