// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import (
	"encoding/json"
	"testing"

	"github.com/ava-labs/subnet-evm/precompile/modules"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ethereum/go-ethereum/common"
)

// mkConfigWithAllowList creates a new config with the correct type for [module]
// by marshalling [cfg] to JSON and then unmarshalling it into the config.
func mkConfigWithAllowList(module modules.Module, cfg *AllowListConfig) precompileconfig.Config {
	jsonBytes, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}

	moduleCfg := module.MakeConfig()
	err = json.Unmarshal(jsonBytes, moduleCfg)
	if err != nil {
		panic(err)
	}

	return moduleCfg
}

func AllowListConfigVerifyTests(module modules.Module) map[string]testutils.ConfigVerifyTest {
	return map[string]testutils.ConfigVerifyTest{
		"invalid allow list config with duplicate admins in allowlist": {
			Config:        mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{TestAdminAddr, TestAdminAddr}, nil}),
			ExpectedError: "duplicate address in admin list",
		},
		"invalid allow list config with duplicate enableds in allowlist": {
			Config:        mkConfigWithAllowList(module, &AllowListConfig{nil, []common.Address{TestEnabledAddr, TestEnabledAddr}}),
			ExpectedError: "duplicate address in enabled list",
		},
		"invalid allow list config with same admin and enabled in allowlist": {
			Config:        mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{TestAdminAddr}, []common.Address{TestAdminAddr}}),
			ExpectedError: "cannot set address as both admin and enabled",
		},
		"nil member allow list config in allowlist": {
			Config:        mkConfigWithAllowList(module, &AllowListConfig{nil, nil}),
			ExpectedError: "",
		},
		"empty member allow list config in allowlist": {
			Config:        mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{}, []common.Address{}}),
			ExpectedError: "",
		},
		"valid allow list config in allowlist": {
			Config:        mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{TestAdminAddr}, []common.Address{TestEnabledAddr}}),
			ExpectedError: "",
		},
	}
}

func AllowListConfigEqualTests(module modules.Module) map[string]testutils.ConfigEqualTest {
	return map[string]testutils.ConfigEqualTest{
		"allowlist non-nil config and nil other": {
			Config:   mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{TestAdminAddr}, []common.Address{TestEnabledAddr}}),
			Other:    nil,
			Expected: false,
		},
		"allowlist different admin": {
			Config:   mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{TestAdminAddr}, []common.Address{TestEnabledAddr}}),
			Other:    mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{{3}}, []common.Address{TestEnabledAddr}}),
			Expected: false,
		},
		"allowlist different enabled": {
			Config:   mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{TestAdminAddr}, []common.Address{TestEnabledAddr}}),
			Other:    mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{module.Address}, []common.Address{{3}}}),
			Expected: false,
		},
		"allowlist same config": {
			Config:   mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{TestAdminAddr}, []common.Address{TestEnabledAddr}}),
			Other:    mkConfigWithAllowList(module, &AllowListConfig{[]common.Address{TestAdminAddr}, []common.Address{TestEnabledAddr}}),
			Expected: true,
		},
	}
}

func VerifyPrecompileWithAllowListTests(t *testing.T, module modules.Module, verifyTests map[string]testutils.ConfigVerifyTest) {
	t.Helper()
	tests := AllowListConfigVerifyTests(module)
	// Add the contract specific tests to the map of tests to run.
	for name, test := range verifyTests {
		if _, exists := tests[name]; exists {
			t.Fatalf("duplicate test name: %s", name)
		}
		tests[name] = test
	}

	testutils.RunVerifyTests(t, tests)
}

func EqualPrecompileWithAllowListTests(t *testing.T, module modules.Module, equalTests map[string]testutils.ConfigEqualTest) {
	t.Helper()
	tests := AllowListConfigEqualTests(module)
	// Add the contract specific tests to the map of tests to run.
	for name, test := range equalTests {
		if _, exists := tests[name]; exists {
			t.Fatalf("duplicate test name: %s", name)
		}
		tests[name] = test
	}

	testutils.RunEqualTests(t, tests)
}
