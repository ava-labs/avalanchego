// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txallowlist_test

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
)

func TestVerify(t *testing.T) {
	allowlisttest.VerifyPrecompileWithAllowListTests(t, txallowlist.Module, nil)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlisttest.TestAdminAddr}
	enableds := []common.Address{allowlisttest.TestEnabledAddr}
	managers := []common.Address{allowlisttest.TestManagerAddr}
	tests := map[string]precompiletest.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   txallowlist.NewConfig(utils.NewUint64(3), admins, enableds, managers),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   txallowlist.NewConfig(nil, nil, nil, nil),
			Other:    precompileconfig.NewMockConfig(gomock.NewController(t)),
			Expected: false,
		},
		"different timestamp": {
			Config:   txallowlist.NewConfig(utils.NewUint64(3), admins, enableds, managers),
			Other:    txallowlist.NewConfig(utils.NewUint64(4), admins, enableds, managers),
			Expected: false,
		},
		"same config": {
			Config:   txallowlist.NewConfig(utils.NewUint64(3), admins, enableds, managers),
			Other:    txallowlist.NewConfig(utils.NewUint64(3), admins, enableds, managers),
			Expected: true,
		},
	}
	allowlisttest.EqualPrecompileWithAllowListTests(t, txallowlist.Module, tests)
}
