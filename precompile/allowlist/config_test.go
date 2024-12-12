// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist_test

import (
	"testing"

	. "github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/modules"
)

var testModule = modules.Module{
	Address:      dummyAddr,
	Contract:     CreateAllowListPrecompile(dummyAddr),
	Configurator: &dummyConfigurator{},
	ConfigKey:    "dummy",
}

func TestVerifyAllowlist(t *testing.T) {
	VerifyPrecompileWithAllowListTests(t, testModule, nil)
}

func TestEqualAllowList(t *testing.T) {
	EqualPrecompileWithAllowListTests(t, testModule, nil)
}
