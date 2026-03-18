// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager_test

import (
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

	tests := map[string]precompiletest.ConfigVerifyTest{
		// Boundary value tests - invalid values
		"targetGas below minimum when validatorTargetGas is false": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:    999_999,
				MinGasPrice:  1,
				TimeToDouble: 200,
			}),
			ExpectedError: commontype.ErrTargetGasTooLowACP224,
		},
		"minGasPrice zero": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:    1_000_000,
				MinGasPrice:  0,
				TimeToDouble: 200,
			}),
			ExpectedError: commontype.ErrMinGasPriceTooLow,
		},
		"timeToDouble zero when staticPricing is false": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:    1_000_000,
				MinGasPrice:  1,
				TimeToDouble: 0,
			}),
			ExpectedError: commontype.ErrTimeToDoubleTooLow,
		},
		// ValidatorTargetGas/StaticPricing interaction tests
		"validatorTargetGas true with non-zero targetGas": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				ValidatorTargetGas: true,
				TargetGas:          1_000_000,
				MinGasPrice:        1,
				TimeToDouble:       200,
			}),
			ExpectedError: commontype.ErrTargetGasMustBeZero,
		},
		"staticPricing true with non-zero timeToDouble": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:     1_000_000,
				StaticPricing: true,
				MinGasPrice:   1,
				TimeToDouble:  200,
			}),
			ExpectedError: commontype.ErrTimeToDoubleMustBeZero,
		},
		"validatorTargetGas and staticPricing both true": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				ValidatorTargetGas: true,
				StaticPricing:      true,
				TargetGas:          0,
				MinGasPrice:        1,
				TimeToDouble:       0,
			}),
		},
		// Valid cases
		"valid fee config": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ValidTestACP224FeeConfig),
		},
		"valid with validatorTargetGas true": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				ValidatorTargetGas: true,
				TargetGas:          0,
				MinGasPrice:        1,
				TimeToDouble:       60,
			}),
		},
		"valid with staticPricing true": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				StaticPricing: true,
				TargetGas:     1_000_000,
				MinGasPrice:   1,
				TimeToDouble:  0,
			}),
		},
		"targetGas at minimum valid value": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:    1_000_000,
				MinGasPrice:  1,
				TimeToDouble: 1,
			}),
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
					c.TargetGas = 20_000_000
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
