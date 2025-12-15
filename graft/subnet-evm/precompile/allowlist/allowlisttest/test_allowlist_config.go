// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlisttest

import (
	"encoding/json"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
)

// mkConfigWithAllowList creates a new config with the correct type for [module]
// by marshalling [cfg] to JSON and then unmarshalling it into the config.
func mkConfigWithAllowList(module modules.Module, cfg *allowlist.AllowListConfig) precompileconfig.Config {
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

func mkConfigWithUpgradeAndAllowList(module modules.Module, cfg *allowlist.AllowListConfig, update precompileconfig.Upgrade) precompileconfig.Config {
	jsonUpgradeBytes, err := json.Marshal(update)
	if err != nil {
		panic(err)
	}

	moduleCfg := mkConfigWithAllowList(module, cfg)
	err = json.Unmarshal(jsonUpgradeBytes, moduleCfg)
	if err != nil {
		panic(err)
	}
	return moduleCfg
}

func AllowListConfigVerifyTests(t testing.TB, module modules.Module) map[string]precompiletest.ConfigVerifyTest {
	return map[string]precompiletest.ConfigVerifyTest{
		"invalid allow list config with duplicate admins in allowlist": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr, TestAdminAddr},
				ManagerAddresses: nil,
				EnabledAddresses: nil,
			}),
			ExpectedError: allowlist.ErrDuplicateAdminAddress,
		},
		"invalid allow list config with duplicate enableds in allowlist": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   nil,
				ManagerAddresses: nil,
				EnabledAddresses: []common.Address{TestEnabledAddr, TestEnabledAddr},
			}),
			ExpectedError: allowlist.ErrDuplicateEnabledAddress,
		},
		"invalid allow list config with duplicate managers in allowlist": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   nil,
				ManagerAddresses: []common.Address{TestManagerAddr, TestManagerAddr},
				EnabledAddresses: nil,
			}),
			ExpectedError: allowlist.ErrDuplicateManagerAddress,
		},
		"invalid allow list config with same admin and enabled in allowlist": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: nil,
				EnabledAddresses: []common.Address{TestAdminAddr},
			}),
			ExpectedError: allowlist.ErrAdminAndEnabledAddress,
		},
		"invalid allow list config with same admin and manager in allowlist": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestAdminAddr},
				EnabledAddresses: nil,
			}),
			ExpectedError: allowlist.ErrAdminAndManagerAddress,
		},
		"invalid allow list config with same manager and enabled in allowlist": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   nil,
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestManagerAddr},
			}),
			ExpectedError: allowlist.ErrEnabledAndManagerAddress,
		},
		"invalid allow list config with manager role before activation": {
			Config: mkConfigWithUpgradeAndAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   nil,
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: nil,
			}, precompileconfig.Upgrade{
				BlockTimestamp: utils.PointerTo[uint64](1),
			}),
			ChainConfig: func() precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(gomock.NewController(t))
				config.EXPECT().IsDurango(gomock.Any()).Return(false)
				return config
			}(),
			ExpectedError: allowlist.ErrCannotAddManagersBeforeDurango,
		},
		"nil member allow list config in allowlist": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   nil,
				ManagerAddresses: nil,
				EnabledAddresses: nil,
			}),
			ExpectedError: nil,
		},
		"empty member allow list config in allowlist": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{},
				ManagerAddresses: []common.Address{},
				EnabledAddresses: []common.Address{},
			}),
			ExpectedError: nil,
		},
		"valid allow list config in allowlist": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			ExpectedError: nil,
		},
	}
}

func AllowListConfigEqualTests(_ testing.TB, module modules.Module) map[string]precompiletest.ConfigEqualTest {
	return map[string]precompiletest.ConfigEqualTest{
		"allowlist non-nil config and nil other": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			Other:    nil,
			Expected: false,
		},
		"allowlist different admin": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			Other: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{{3}},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			Expected: false,
		},
		"allowlist different manager": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			Other: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{{3}},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			Expected: false,
		},
		"allowlist different enabled": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			Other: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{{3}},
			}),
			Expected: false,
		},
		"allowlist same config": {
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			Other: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			Expected: true,
		},
	}
}

func VerifyPrecompileWithAllowListTests(t *testing.T, module modules.Module, verifyTests map[string]precompiletest.ConfigVerifyTest) {
	t.Helper()
	tests := AllowListConfigVerifyTests(t, module)
	// Add the contract specific tests to the map of tests to run.
	for name, test := range verifyTests {
		require.NotContains(t, tests, name, "duplicate test name: %s", name)
		tests[name] = test
	}

	precompiletest.RunVerifyTests(t, tests)
}

func EqualPrecompileWithAllowListTests(t *testing.T, module modules.Module, equalTests map[string]precompiletest.ConfigEqualTest) {
	t.Helper()
	tests := AllowListConfigEqualTests(t, module)
	// Add the contract specific tests to the map of tests to run.
	for name, test := range equalTests {
		require.NotContains(t, tests, name, "duplicate test name: %s", name)
		tests[name] = test
	}

	precompiletest.RunEqualTests(t, tests)
}
