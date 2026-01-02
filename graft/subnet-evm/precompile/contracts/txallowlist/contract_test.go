// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txallowlist_test

import (
	"testing"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
)

func TestTxAllowListRun(t *testing.T) {
	allowlisttest.RunPrecompileWithAllowListTests(t, txallowlist.Module, nil)
}
