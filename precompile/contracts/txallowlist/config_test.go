// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txallowlist

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
	allowlisttest.VerifyPrecompileWithAllowListTests(t, Module, nil)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	managers := []common.Address{allowlisttest.TestManagerAddr}
	tests := map[string]precompiletest.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   NewConfig(utils.NewUint64(3), admins, enableds, managers),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   NewConfig(nil, nil, nil, nil),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		"different timestamp": {
			Config:   NewConfig(utils.NewUint64(3), admins, enableds, managers),
			Other:    NewConfig(utils.NewUint64(4), admins, enableds, managers),
			Expected: false,
		},
		"same config": {
			Config:   NewConfig(utils.NewUint64(3), admins, enableds, managers),
			Other:    NewConfig(utils.NewUint64(3), admins, enableds, managers),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, Module, tests)
}
