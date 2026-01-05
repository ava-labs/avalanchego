// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
)

var (
	setFeeConfigSignature              = contract.CalculateFunctionSelector("setFeeConfig(uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256)")
	getFeeConfigSignature              = contract.CalculateFunctionSelector("getFeeConfig()")
	getFeeConfigLastChangedAtSignature = contract.CalculateFunctionSelector("getFeeConfigLastChangedAt()")
	testFeeConfig                      = commontype.ValidTestFeeConfig
)

func FuzzPackGetFeeConfigOutputTest(f *testing.F) {
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
		testPackGetFeeConfigOutput(t, feeConfig, doCheckOutputs)
	})
}

func TestPackGetFeeConfigOutput(t *testing.T) {
	testPackGetFeeConfigOutput(t, testFeeConfig, true)
}

func testPackGetFeeConfigOutput(t *testing.T, feeConfig commontype.FeeConfig, checkOutputs bool) {
	t.Helper()
	t.Run(fmt.Sprintf("TestGetFeeConfigOutput, feeConfig %v", feeConfig), func(t *testing.T) {
		input, err := PackGetFeeConfigOutput(feeConfig)
		if err != nil {
			return
		}

		unpacked, err := UnpackGetFeeConfigOutput(input, false)
		require.NoError(t, err)
		if checkOutputs {
			require.True(t, feeConfig.Equal(&unpacked), "not equal: feeConfig %v, unpacked %v", feeConfig, unpacked)
		}
	})
}

func TestPackGetFeeConfigOutputPanic(t *testing.T) {
	require.Panics(t, func() {
		_, _ = PackGetFeeConfigOutput(commontype.FeeConfig{})
	})
}

func TestUnpackGetFeeConfigOutput(t *testing.T) {
	testInputBytes, err := PackGetFeeConfigOutput(testFeeConfig)
	require.NoError(t, err)
	tests := []struct {
		name           string
		input          []byte
		skipLenCheck   bool
		expectedErr    error
		expectedOutput commontype.FeeConfig
	}{
		{
			name:         "empty input",
			input:        []byte{},
			skipLenCheck: false,
			expectedErr:  ErrInvalidLen,
		},
		{
			name:         "empty input skip len check",
			input:        []byte{},
			skipLenCheck: true,
			expectedErr:  ErrUnpackOutput,
		},
		{
			name:         "input with extra bytes",
			input:        append(testInputBytes, make([]byte, 32)...),
			skipLenCheck: false,
			expectedErr:  ErrInvalidLen,
		},
		{
			name:           "input with extra bytes skip len check",
			input:          append(testInputBytes, make([]byte, 32)...),
			skipLenCheck:   true,
			expectedErr:    nil,
			expectedOutput: testFeeConfig,
		},
		{
			name:         "input with extra bytes (not divisible by 32)",
			input:        append(testInputBytes, make([]byte, 33)...),
			skipLenCheck: false,
			expectedErr:  ErrInvalidLen,
		},
		{
			name:         "input with extra bytes (not divisible by 32) skip len check",
			input:        append(testInputBytes, make([]byte, 33)...),
			skipLenCheck: true,
			expectedErr:  ErrUnpackOutput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unpacked, err := UnpackGetFeeConfigOutput(test.input, test.skipLenCheck)
			require.ErrorIs(t, err, test.expectedErr)
			if test.expectedErr == nil {
				require.True(t, test.expectedOutput.Equal(&unpacked), "not equal: expectedOutput %v, unpacked %v", test.expectedOutput, unpacked)
			}
		})
	}
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
		testPackGetLastChangedAtOutput(t, bigIntVal, doCheckOutputs)
	})
}

func testPackGetLastChangedAtOutput(t *testing.T, blockNumber *big.Int, checkOutputs bool) {
	t.Helper()
	t.Run(fmt.Sprintf("TestGetLastChangedAtOutput, blockNumber %v", blockNumber), func(t *testing.T) {
		input, err := PackGetFeeConfigLastChangedAtOutput(blockNumber)
		require.NoError(t, err)

		unpacked, err := UnpackGetFeeConfigLastChangedAtOutput(input)
		require.NoError(t, err)
		if checkOutputs {
			require.Zero(t, blockNumber.Cmp(unpacked), "not equal: blockNumber %v, unpacked %v", blockNumber, unpacked)
		}
	})
}

func FuzzPackSetFeeConfigTest(f *testing.F) {
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
		testPackSetFeeConfigInput(t, feeConfig, doCheckOutputs)
	})
}

func TestPackSetFeeConfigInput(t *testing.T) {
	testPackSetFeeConfigInput(t, testFeeConfig, true)
}

func testPackSetFeeConfigInput(t *testing.T, feeConfig commontype.FeeConfig, checkOutputs bool) {
	t.Helper()
	t.Run(fmt.Sprintf("TestSetFeeConfigInput, feeConfig %v", feeConfig), func(t *testing.T) {
		input, err := PackSetFeeConfig(feeConfig)
		if err != nil {
			return
		}

		// exclude 4 bytes for function selector
		unpacked, err := UnpackSetFeeConfigInput(input[4:], true)
		require.NoError(t, err)
		if checkOutputs {
			require.True(t, feeConfig.Equal(&unpacked), "not equal: feeConfig %v, unpacked %v", feeConfig, unpacked)
		}
	})
}

func TestPackSetFeeConfigInputPanic(t *testing.T) {
	require.Panics(t, func() {
		_, _ = PackSetFeeConfig(commontype.FeeConfig{})
	})
}

func TestUnpackSetFeeConfigInput(t *testing.T) {
	testInputBytes, err := PackSetFeeConfig(testFeeConfig)
	require.NoError(t, err)
	// exclude 4 bytes for function selector
	testInputBytes = testInputBytes[4:]
	tests := []struct {
		name       string
		input      []byte
		strictMode bool
		wantErr    error
		wantOutput commontype.FeeConfig
	}{
		{
			name:       "empty input strict mode",
			input:      []byte{},
			strictMode: true,
			wantErr:    ErrInvalidLen,
		},
		{
			name:       "empty input",
			input:      []byte{},
			strictMode: false,
			wantErr:    ErrUnpackInput,
		},
		{
			name:       "input with insufficient len strict mode",
			input:      []byte{123},
			strictMode: true,
			wantErr:    ErrInvalidLen,
		},
		{
			name:       "input with insufficient len",
			input:      []byte{123},
			strictMode: false,
			wantErr:    ErrUnpackInput,
		},
		{
			name:       "input with extra bytes strict mode",
			input:      append(testInputBytes, make([]byte, 32)...),
			strictMode: true,
			wantErr:    ErrInvalidLen,
		},
		{
			name:       "input with extra bytes",
			input:      append(testInputBytes, make([]byte, 32)...),
			strictMode: false,
			wantErr:    nil,
			wantOutput: testFeeConfig,
		},
		{
			name:       "input with extra bytes (not divisible by 32) strict mode",
			input:      append(testInputBytes, make([]byte, 33)...),
			strictMode: true,
			wantErr:    ErrInvalidLen,
		},
		{
			name:       "input with extra bytes (not divisible by 32)",
			input:      append(testInputBytes, make([]byte, 33)...),
			strictMode: false,
			wantOutput: testFeeConfig,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unpacked, err := UnpackSetFeeConfigInput(test.input, test.strictMode)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
				require.True(t, test.wantOutput.Equal(&unpacked), "not equal: expectedOutput %v, unpacked %v", test.wantOutput, unpacked)
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
