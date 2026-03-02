// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package deployerallowlist_test

import (
	"testing"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
)

func TestContractDeployerAllowListRun(t *testing.T) {
	allowlisttest.RunPrecompileWithAllowListTests(t, deployerallowlist.Module, nil)
}
