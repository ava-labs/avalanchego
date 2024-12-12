// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package deployerallowlist_test

import (
	"testing"

	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	. "github.com/ava-labs/subnet-evm/precompile/contracts/deployerallowlist"
)

func TestContractDeployerAllowListRun(t *testing.T) {
	allowlist.RunPrecompileWithAllowListTests(t, Module, extstate.NewTestStateDB, nil)
}

func BenchmarkContractDeployerAllowList(b *testing.B) {
	allowlist.BenchPrecompileWithAllowList(b, Module, extstate.NewTestStateDB, nil)
}
