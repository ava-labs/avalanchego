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

	// Field-level validation is covered in commontype.TestACP224FeeConfigVerify.
	// Here we only test that Config.Verify delegates to InitialFeeConfig.Verify.
	tests := map[string]precompiletest.ConfigVerifyTest{
		"nil initialFeeConfig": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
		},
		"valid initialFeeConfig": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.DefaultACP224FeeConfig),
		},
		"invalid initialFeeConfig": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.ACP224FeeConfig{
				TargetGas:    commontype.MinTargetGasACP224,
				TimeToDouble: 60,
			}),
			ExpectedError: commontype.ErrMinGasPriceTooLow,
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
			Config:   acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.DefaultACP224FeeConfig),
			Other:    acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			Expected: false,
		},
		"different initial config": {
			Config: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.DefaultACP224FeeConfig),
			Other: acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				func() *commontype.ACP224FeeConfig {
					c := commontype.DefaultACP224FeeConfig
					c.TargetGas = 20_000_000
					return &c
				}()),
			Expected: false,
		},
		"same config": {
			Config:   acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.DefaultACP224FeeConfig),
			Other:    acp224feemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.DefaultACP224FeeConfig),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, acp224feemanager.Module, tests)
}
