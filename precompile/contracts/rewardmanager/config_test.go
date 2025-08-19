// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rewardmanager

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/precompiletest"
	"github.com/ava-labs/subnet-evm/utils"
)

func TestVerify(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	managers := []common.Address{allowlisttest.TestManagerAddr}
	tests := map[string]precompiletest.ConfigVerifyTest{
		"both reward mechanisms should not be activated at the same time in reward manager": {
			Config: NewConfig(utils.NewUint64(3), admins, enableds, managers, &InitialRewardConfig{
				AllowFeeRecipients: true,
				RewardAddress:      common.HexToAddress("0x01"),
			}),
			ExpectedError: ErrCannotEnableBothRewards.Error(),
		},
	}
	allowlisttest.VerifyPrecompileWithAllowListTests(t, Module, tests)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	managers := []common.Address{allowlisttest.TestManagerAddr}
	tests := map[string]precompiletest.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   NewConfig(utils.NewUint64(3), admins, enableds, managers, nil),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   NewConfig(utils.NewUint64(3), admins, enableds, managers, nil),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		"different timestamp": {
			Config:   NewConfig(utils.NewUint64(3), admins, nil, nil, nil),
			Other:    NewConfig(utils.NewUint64(4), admins, nil, nil, nil),
			Expected: false,
		},
		"non-nil initial config and nil initial config": {
			Config: NewConfig(utils.NewUint64(3), admins, nil, nil, &InitialRewardConfig{
				AllowFeeRecipients: true,
			}),
			Other:    NewConfig(utils.NewUint64(3), admins, nil, nil, nil),
			Expected: false,
		},
		"different initial config": {
			Config: NewConfig(utils.NewUint64(3), admins, nil, nil, &InitialRewardConfig{
				RewardAddress: common.HexToAddress("0x01"),
			}),
			Other: NewConfig(utils.NewUint64(3), admins, nil, nil,
				&InitialRewardConfig{
					RewardAddress: common.HexToAddress("0x02"),
				}),
			Expected: false,
		},
		"same config": {
			Config: NewConfig(utils.NewUint64(3), admins, nil, nil, &InitialRewardConfig{
				RewardAddress: common.HexToAddress("0x01"),
			}),
			Other: NewConfig(utils.NewUint64(3), admins, nil, nil, &InitialRewardConfig{
				RewardAddress: common.HexToAddress("0x01"),
			}),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, Module, tests)
}
