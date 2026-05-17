// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils"
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
			Config:        feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &invalidFeeConfig),
			ExpectedError: commontype.ErrGasLimitTooLow,
		},
		"nil initial fee manager config": {
			Config:        feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.FeeConfig{}),
			ExpectedError: commontype.ErrGasLimitNil,
		},
	}
	allowlisttest.VerifyPrecompileWithAllowListTests(t, feemanager.Module, tests)
}

// TestVerifyHeliconRetirement covers the rule that the legacy feeManager
// precompile cannot be ENABLED at or after Helicon.
func TestVerifyHeliconRetirement(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}

	mkChainConfig := func(t *testing.T, isHelicon bool) precompileconfig.ChainConfig {
		t.Helper()
		ctrl := gomock.NewController(t)
		c := precompileconfig.NewMockChainConfig(ctrl)
		c.EXPECT().GetFeeConfig().AnyTimes().Return(commontype.ValidTestFeeConfig)
		c.EXPECT().AllowedFeeRecipients().AnyTimes().Return(false)
		c.EXPECT().IsDurango(gomock.Any()).AnyTimes().Return(true)
		c.EXPECT().IsHelicon(gomock.Any()).AnyTimes().Return(isHelicon)
		return c
	}

	tests := map[string]precompiletest.ConfigVerifyTest{
		"enable pre-Helicon is allowed": {
			Config:        feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			ChainConfig:   mkChainConfig(t, false),
			ExpectedError: nil,
		},
		"enable at-or-after Helicon is rejected": {
			Config:        feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			ChainConfig:   mkChainConfig(t, true),
			ExpectedError: feemanager.ErrFeeManagerEnabledAfterHelicon,
		},
		"disable at-or-after Helicon is allowed": {
			Config:        feemanager.NewDisableConfig(utils.PointerTo[uint64](3)),
			ChainConfig:   mkChainConfig(t, true),
			ExpectedError: nil,
		},
	}
	precompiletest.RunVerifyTests(t, tests)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	tests := map[string]precompiletest.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   feemanager.NewConfig(utils.PointerTo[uint64](3), admins, enableds, nil, nil),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   feemanager.NewConfig(utils.PointerTo[uint64](3), admins, enableds, nil, nil),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		"different timestamp": {
			Config:   feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			Other:    feemanager.NewConfig(utils.PointerTo[uint64](4), admins, nil, nil, nil),
			Expected: false,
		},
		"non-nil initial config and nil initial config": {
			Config:   feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &validFeeConfig),
			Other:    feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			Expected: false,
		},
		"different initial config": {
			Config: feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &validFeeConfig),
			Other: feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				func() *commontype.FeeConfig {
					c := validFeeConfig
					c.GasLimit = big.NewInt(123)
					return &c
				}()),
			Expected: false,
		},
		"same config": {
			Config:   feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &validFeeConfig),
			Other:    feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &validFeeConfig),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, feemanager.Module, tests)
}
