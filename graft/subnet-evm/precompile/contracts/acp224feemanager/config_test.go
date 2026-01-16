// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/acp224feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
	"github.com/ava-labs/avalanchego/utils"
)

func TestVerify(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}

	// Create a value that exceeds hash length (32 bytes = 256 bits)
	maxHashValue := new(big.Int).Lsh(big.NewInt(1), 256) // 2^256

	tests := map[string]precompiletest.ConfigVerifyTest{
		// Nil field tests
		"nil targetGas": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         nil,
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrTargetGasNilACP224,
		},
		"nil minGasPrice": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       nil,
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrMinGasPriceNil,
		},
		"nil maxCapacityFactor": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: nil,
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrMaxCapacityFactorNil,
		},
		"nil timeToDouble": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      nil,
			}),
			ExpectedError: commontype.ErrTimeToDoubleNil,
		},
		// Boundary value tests - invalid values
		"targetGas zero": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(0),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrTargetGasTooLowACP224,
		},
		"targetGas negative": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(-1),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrTargetGasTooLowACP224,
		},
		"minGasPrice zero": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(0),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrMinGasPriceTooLow,
		},
		"minGasPrice negative": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(-1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrMinGasPriceTooLow,
		},
		"maxCapacityFactor negative": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(-1),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrMaxCapacityFactorTooLow,
		},
		"timeToDouble negative": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(-1),
			}),
			ExpectedError: commontype.ErrTimeToDoubleTooLow,
		},
		// Hash length exceeded tests
		"targetGas exceeds hash length": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         maxHashValue,
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrTargetGasExceedsHashLengthACP224,
		},
		"minGasPrice exceeds hash length": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       maxHashValue,
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrMinGasPriceExceedsHashLength,
		},
		"maxCapacityFactor exceeds hash length": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: maxHashValue,
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrMaxCapacityFactorExceedsHashLength,
		},
		"timeToDouble exceeds hash length": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      maxHashValue,
			}),
			ExpectedError: commontype.ErrTimeToDoubleExceedsHashLength,
		},
		// Valid boundary cases
		"targetGas minimum valid value": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: nil,
		},
		"minGasPrice minimum valid value": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: nil,
		},
		"maxCapacityFactor minimum valid value": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(1),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: nil,
		},
		"timeToDouble minimum valid value": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(1),
			}),
			ExpectedError: nil,
		},
		"maxCapacityFactor zero": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(0),
				TimeToDouble:      big.NewInt(200),
			}),
			ExpectedError: commontype.ErrMaxCapacityFactorTooLow,
		},
		"timeToDouble zero": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:         big.NewInt(1000),
				MinGasPrice:       big.NewInt(1),
				MaxCapacityFactor: big.NewInt(100),
				TimeToDouble:      big.NewInt(0),
			}),
			ExpectedError: commontype.ErrTimeToDoubleTooLow,
		},
		"valid fee config": {
			Config:        acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ValidTestACP224FeeConfig),
			ExpectedError: nil,
		},
	}
	allowlisttest.VerifyPrecompileWithAllowListTests(t, acp224feemanager.Module, tests)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	tests := map[string]precompiletest.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, enableds, nil, nil),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, enableds, nil, nil),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		"different timestamp": {
			Config:   acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			Other:    acp224feemanager.NewConfig(utils.PointerTo[uint64](4), admins, nil, nil, nil),
			Expected: false,
		},
		"non-nil initial config and nil initial config": {
			Config:   acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ValidTestACP224FeeConfig),
			Other:    acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			Expected: false,
		},
		"different initial config": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ValidTestACP224FeeConfig),
			Other: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				func() *commontype.ACP224FeeConfig {
					c := commontype.ValidTestACP224FeeConfig
					c.TargetGas = big.NewInt(123)
					return &c
				}()),
			Expected: false,
		},
		"same config": {
			Config:   acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ValidTestACP224FeeConfig),
			Other:    acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ValidTestACP224FeeConfig),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, acp224feemanager.Module, tests)
}
