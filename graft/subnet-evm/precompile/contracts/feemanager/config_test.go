// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils"
)

var validFeeConfig = commontype.FeeConfig{
	GasLimit:        big.NewInt(8_000_000),
	TargetBlockRate: 2, // in seconds

	MinBaseFee:               big.NewInt(25_000_000_000),
	TargetGas:                big.NewInt(15_000_000),
	BaseFeeChangeDenominator: big.NewInt(36),

	MinBlockGasCost:  big.NewInt(0),
	MaxBlockGasCost:  big.NewInt(1_000_000),
	BlockGasCostStep: big.NewInt(200_000),
}

func TestVerify(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	invalidFeeConfig := validFeeConfig
	invalidFeeConfig.GasLimit = big.NewInt(0)
	tests := map[string]precompiletest.ConfigVerifyTest{
		"invalid initial fee manager config": {
			Config:        feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &invalidFeeConfig),
			ExpectedError: commontype.ErrGasLimitTooLow,
		},
		"nil initial fee manager config": {
			Config:        feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &commontype.FeeConfig{}),
			ExpectedError: commontype.ErrGasLimitNil,
		},
	}
	allowlisttest.VerifyPrecompileWithAllowListTests(t, feemanager.Module, tests)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	tests := map[string]precompiletest.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   feemanager.NewConfig(utils.NewUint64(3), admins, enableds, nil, nil),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   feemanager.NewConfig(utils.NewUint64(3), admins, enableds, nil, nil),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		"different timestamp": {
			Config:   feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil, nil),
			Other:    feemanager.NewConfig(utils.NewUint64(4), admins, nil, nil, nil),
			Expected: false,
		},
		"non-nil initial config and nil initial config": {
			Config:   feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &validFeeConfig),
			Other:    feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil, nil),
			Expected: false,
		},
		"different initial config": {
			Config: feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &validFeeConfig),
			Other: feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil,
				func() *commontype.FeeConfig {
					c := validFeeConfig
					c.GasLimit = big.NewInt(123)
					return &c
				}()),
			Expected: false,
		},
		"same config": {
			Config:   feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &validFeeConfig),
			Other:    feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &validFeeConfig),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, feemanager.Module, tests)
}
