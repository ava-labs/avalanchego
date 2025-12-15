// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist_test

import (
	"testing"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
)

var testModule = modules.Module{
	Address:      dummyAddr,
	Contract:     allowlist.CreateAllowListPrecompile(dummyAddr),
	Configurator: &dummyConfigurator{},
	ConfigKey:    "dummy",
}

func TestVerifyAllowlist(t *testing.T) {
	allowlisttest.RunPrecompileWithAllowListTests(t, testModule, nil)
}

func TestEqualAllowList(t *testing.T) {
	allowlisttest.EqualPrecompileWithAllowListTests(t, testModule, nil)
}
