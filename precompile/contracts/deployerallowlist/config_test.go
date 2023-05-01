// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package deployerallowlist

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ethereum/go-ethereum/common"
)

func TestVerify(t *testing.T) {
	allowlist.VerifyPrecompileWithAllowListTests(t, Module, nil)
}

func TestEqual(t *testing.T) {
	admins := []common.Address{allowlist.TestAdminAddr}
	enableds := []common.Address{allowlist.TestEnabledAddr}
	tests := map[string]testutils.ConfigEqualTest{
		"non-nil config and nil other": {
			Config:   NewConfig(big.NewInt(3), admins, enableds),
			Other:    nil,
			Expected: false,
		},
		"different type": {
			Config:   NewConfig(nil, nil, nil),
			Other:    precompileconfig.NewNoopStatefulPrecompileConfig(),
			Expected: false,
		},
		"different timestamp": {
			Config:   NewConfig(big.NewInt(3), admins, enableds),
			Other:    NewConfig(big.NewInt(4), admins, enableds),
			Expected: false,
		},
		"same config": {
			Config:   NewConfig(big.NewInt(3), admins, enableds),
			Other:    NewConfig(big.NewInt(3), admins, enableds),
			Expected: true,
		},
	}
	allowlist.EqualPrecompileWithAllowListTests(t, Module, tests)
}
