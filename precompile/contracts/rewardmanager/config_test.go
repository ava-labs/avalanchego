// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rewardmanager

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ava-labs/subnet-evm/utils"
	"go.uber.org/mock/gomock"
)

func TestVerify(t *testing.T) {
	admins := []common.Address{allowlist.TestAdminAddr}
	enableds := []common.Address{allowlist.TestEnabledAddr}
	managers := []common.Address{allowlist.TestManagerAddr}
	tests := map[string]testutils.ConfigVerifyTest{
		"both reward mechanisms should not be activated at the same time in reward manager": {
			Config: NewConfig(utils.NewUint64(3), admins, enableds, managers, &InitialRewardConfig{
				AllowFeeRecipients: true,
				RewardAddress:      common.HexToAddress("0x01"),
			}),
			ExpectedError: ErrCannotEnableBothRewards.Error(),
		},
	}
	allowlist.VerifyPrecompileWithAllowListTests(t, Module, tests)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlist.TestAdminAddr}
	enableds := []common.Address{allowlist.TestEnabledAddr}
	managers := []common.Address{allowlist.TestManagerAddr}
	tests := map[string]testutils.ConfigEqualTest{
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
	allowlist.EqualPrecompileWithAllowListTests(t, Module, tests)
}
