// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rewardmanager

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ethereum/go-ethereum/common"
)

func TestVerify(t *testing.T) {
	admins := []common.Address{allowlist.TestAdminAddr}
	enableds := []common.Address{allowlist.TestEnabledAddr}
	tests := map[string]testutils.ConfigVerifyTest{
		"both reward mechanisms should not be activated at the same time in reward manager": {
			Config: NewConfig(big.NewInt(3), admins, enableds, &InitialRewardConfig{
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
	tests := map[string]testutils.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   NewConfig(big.NewInt(3), admins, enableds, nil),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   NewConfig(big.NewInt(3), admins, enableds, nil),
			Other:    precompileconfig.NewNoopStatefulPrecompileConfig(),
			Expected: false,
		},
		"different timestamp": {
			Config:   NewConfig(big.NewInt(3), admins, nil, nil),
			Other:    NewConfig(big.NewInt(4), admins, nil, nil),
			Expected: false,
		},
		"non-nil initial config and nil initial config": {
			Config: NewConfig(big.NewInt(3), admins, nil, &InitialRewardConfig{
				AllowFeeRecipients: true,
			}),
			Other:    NewConfig(big.NewInt(3), admins, nil, nil),
			Expected: false,
		},
		"different initial config": {
			Config: NewConfig(big.NewInt(3), admins, nil, &InitialRewardConfig{
				RewardAddress: common.HexToAddress("0x01"),
			}),
			Other: NewConfig(big.NewInt(3), admins, nil,
				&InitialRewardConfig{
					RewardAddress: common.HexToAddress("0x02"),
				}),
			Expected: false,
		},
		"same config": {
			Config: NewConfig(big.NewInt(3), admins, nil, &InitialRewardConfig{
				RewardAddress: common.HexToAddress("0x01"),
			}),
			Other: NewConfig(big.NewInt(3), admins, nil, &InitialRewardConfig{
				RewardAddress: common.HexToAddress("0x01"),
			}),
			Expected: true,
		},
	}
	allowlist.EqualPrecompileWithAllowListTests(t, Module, tests)
}
