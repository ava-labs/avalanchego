// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter_test

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
)

func TestVerify(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	managers := []common.Address{allowlisttest.TestManagerAddr}
	tests := map[string]precompiletest.ConfigVerifyTest{
		"valid config": {
			Config: nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, enableds, managers, nil),
			ChainConfig: func() precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(gomock.NewController(t))
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			}(),
			ExpectedError: nil,
		},
		"invalid allow list config in native minter allowlisttest": {
			Config:        nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, admins, nil, nil),
			ExpectedError: allowlist.ErrAdminAndEnabledAddress,
		},
		"duplicate admins in config in native minter allowlisttest": {
			Config:        nativeminter.NewConfig(utils.PointerTo[uint64](3), append(admins, admins[0]), enableds, managers, nil),
			ExpectedError: allowlist.ErrDuplicateAdminAddress,
		},
		"duplicate enableds in config in native minter allowlisttest": {
			Config:        nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, append(enableds, enableds[0]), managers, nil),
			ExpectedError: allowlist.ErrDuplicateEnabledAddress,
		},
		"nil amount in native minter config": {
			Config: nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(123),
					common.HexToAddress("0x02"): nil,
				}),
			ExpectedError: nativeminter.ErrInitialMintNilAmount,
		},
		"negative amount in native minter config": {
			Config: nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(123),
					common.HexToAddress("0x02"): math.NewHexOrDecimal256(-1),
				}),
			ExpectedError: nativeminter.ErrInitialMintInvalidAmount,
		},
	}
	allowlisttest.VerifyPrecompileWithAllowListTests(t, nativeminter.Module, tests)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	managers := []common.Address{allowlisttest.TestManagerAddr}
	tests := map[string]precompiletest.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, enableds, managers, nil),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, enableds, managers, nil),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		"different timestamp": {
			Config:   nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil, nil),
			Other:    nativeminter.NewConfig(utils.PointerTo[uint64](4), admins, nil, nil, nil),
			Expected: false,
		},
		"different initial mint amounts": {
			Config: nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			Other: nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(2),
				}),
			Expected: false,
		},
		"different initial mint addresses": {
			Config: nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			Other: nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x02"): math.NewHexOrDecimal256(1),
				}),
			Expected: false,
		},
		"same config": {
			Config: nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			Other: nativeminter.NewConfig(utils.PointerTo[uint64](3), admins, nil, nil,
				map[common.Address]*math.HexOrDecimal256{
					common.HexToAddress("0x01"): math.NewHexOrDecimal256(1),
				}),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, nativeminter.Module, tests)
}
