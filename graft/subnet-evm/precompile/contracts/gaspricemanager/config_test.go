// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gaspricemanager_test

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
	"github.com/ava-labs/avalanchego/utils"
)

func TestVerify(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}

	// Field-level validation is covered in commontype.TestGasPriceConfigVerify.
	// Here we only test that Config.Verify delegates to InitialGasPriceConfig.Verify.
	tests := map[string]precompiletest.ConfigVerifyTest{
		"nil initialGasPriceConfig": {
			Config: gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
		},
		"valid initialGasPriceConfig": {
			Config: gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, utils.PointerTo(commontype.DefaultGasPriceConfig())),
		},
		"invalid initialGasPriceConfig": {
			Config: gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, &commontype.GasPriceConfig{
				TargetGas:    commontype.MinTargetGas,
				TimeToDouble: 60,
			}),
			ExpectedError: commontype.ErrMinGasPriceTooLow,
		},
	}
	allowlisttest.VerifyPrecompileWithAllowListTests(t, gaspricemanager.Module, tests)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	tests := map[string]precompiletest.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, enableds, nil, nil),
			Other:    nil,
			Expected: false,
		},
		"typed nil config and typed nil other": {
			Config:   (*gaspricemanager.Config)(nil),
			Other:    (*gaspricemanager.Config)(nil),
			Expected: false,
		},
		"typed nil config and nil other": {
			Config:   (*gaspricemanager.Config)(nil),
			Other:    nil,
			Expected: true,
		},
		"different type": {
			Config:   gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, enableds, nil, nil),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		"different timestamp": {
			Config:   gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			Other:    gaspricemanager.NewConfig(utils.PointerTo[uint64](4), admins, nil, nil, nil),
			Expected: false,
		},
		"non-nil initial config and nil initial config": {
			Config:   gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, utils.PointerTo(commontype.DefaultGasPriceConfig())),
			Other:    gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			Expected: false,
		},
		"different initial config": {
			Config: gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, utils.PointerTo(commontype.DefaultGasPriceConfig())),
			Other: gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				func() *commontype.GasPriceConfig {
					c := commontype.DefaultGasPriceConfig()
					c.TargetGas = 20_000_000
					return &c
				}()),
			Expected: false,
		},
		"same config": {
			Config:   gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, utils.PointerTo(commontype.DefaultGasPriceConfig())),
			Other:    gaspricemanager.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, utils.PointerTo(commontype.DefaultGasPriceConfig())),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, gaspricemanager.Module, tests)
}
