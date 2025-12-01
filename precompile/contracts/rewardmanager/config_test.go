// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rewardmanager_test

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/subnet-evm/precompile/contracts/rewardmanager"
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
			Config: rewardmanager.NewConfig(utils.NewUint64(3), admins, enableds, managers, &rewardmanager.InitialRewardConfig{
				AllowFeeRecipients: true,
				RewardAddress:      common.HexToAddress("0x01"),
			}),
			ExpectedError: rewardmanager.ErrCannotEnableBothRewards.Error(),
		},
	}
	allowlisttest.VerifyPrecompileWithAllowListTests(t, rewardmanager.Module, tests)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	managers := []common.Address{allowlisttest.TestManagerAddr}
	tests := map[string]precompiletest.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   rewardmanager.NewConfig(utils.NewUint64(3), admins, enableds, managers, nil),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   rewardmanager.NewConfig(utils.NewUint64(3), admins, enableds, managers, nil),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		"different timestamp": {
			Config:   rewardmanager.NewConfig(utils.NewUint64(3), admins, nil, nil, nil),
			Other:    rewardmanager.NewConfig(utils.NewUint64(4), admins, nil, nil, nil),
			Expected: false,
		},
		"non-nil initial config and nil initial config": {
			Config: rewardmanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &rewardmanager.InitialRewardConfig{
				AllowFeeRecipients: true,
			}),
			Other:    rewardmanager.NewConfig(utils.NewUint64(3), admins, nil, nil, nil),
			Expected: false,
		},
		"different initial config": {
			Config: rewardmanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &rewardmanager.InitialRewardConfig{
				RewardAddress: common.HexToAddress("0x01"),
			}),
			Other: rewardmanager.NewConfig(utils.NewUint64(3), admins, nil, nil,
				&rewardmanager.InitialRewardConfig{
					RewardAddress: common.HexToAddress("0x02"),
				}),
			Expected: false,
		},
		"same config": {
			Config: rewardmanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &rewardmanager.InitialRewardConfig{
				RewardAddress: common.HexToAddress("0x01"),
			}),
			Other: rewardmanager.NewConfig(utils.NewUint64(3), admins, nil, nil, &rewardmanager.InitialRewardConfig{
				RewardAddress: common.HexToAddress("0x01"),
			}),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, rewardmanager.Module, tests)
}
