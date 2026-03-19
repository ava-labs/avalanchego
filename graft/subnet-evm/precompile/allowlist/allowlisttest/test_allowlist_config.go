// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlisttest

import (
	"encoding/json"
	"testing"

	"github.com/ava-labs/libevm/common"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
	"github.com/ava-labs/avalanchego/utils"
)

// mkConfigWithAllowList creates a new config with the correct type for [module]
// by marshalling [cfg] to JSON and then unmarshalling it into the config.
func mkConfigWithAllowList(module modules.Module, cfg *allowlist.AllowListConfig) precompileconfig.Config {
	return mkConfigWithUpgradeAndAllowList(module, cfg, precompileconfig.Upgrade{})
}

func mkConfigWithUpgradeAndAllowList(module modules.Module, cfg *allowlist.AllowListConfig, update precompileconfig.Upgrade) precompileconfig.Config {
	// Apply AllowListConfig fields via JSON round-trip so they land in the
	// correct concrete Config type for the module.
	jsonBytes, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}

	moduleCfg := module.MakeConfig()
	if err := json.Unmarshal(jsonBytes, moduleCfg); err != nil {
		panic(err)
	}

	if update != (precompileconfig.Upgrade{}) {
		jsonUpgradeBytes, err := json.Marshal(update)
		if err != nil {
			panic(err)
		}
		if err := json.Unmarshal(jsonUpgradeBytes, moduleCfg); err != nil {
			panic(err)
		}
	}

	return moduleCfg
}

func AllowListConfigVerifyTests(t testing.TB, module modules.Module) []precompiletest.ConfigVerifyTest {
	return []precompiletest.ConfigVerifyTest{
		{
			Name: "invalid allow list config with duplicate admins in allowlist",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses: []common.Address{TestAdminAddr, TestAdminAddr},
			}),
			ExpectedErr: allowlist.ErrDuplicateAdminAddress,
		},
		{
			Name: "invalid allow list config with duplicate enableds in allowlist",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				EnabledAddresses: []common.Address{TestEnabledAddr, TestEnabledAddr},
			}),
			ExpectedErr: allowlist.ErrDuplicateEnabledAddress,
		},
		{
			Name: "invalid allow list config with duplicate managers in allowlist",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				ManagerAddresses: []common.Address{TestManagerAddr, TestManagerAddr},
			}),
			ExpectedErr: allowlist.ErrDuplicateManagerAddress,
		},
		{
			Name: "invalid allow list config with same admin and enabled in allowlist",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				EnabledAddresses: []common.Address{TestAdminAddr},
			}),
			ExpectedErr: allowlist.ErrAdminAndEnabledAddress,
		},
		{
			Name: "invalid allow list config with same admin and manager in allowlist",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestAdminAddr},
			}),
			ExpectedErr: allowlist.ErrAdminAndManagerAddress,
		},
		{
			Name: "invalid allow list config with same manager and enabled in allowlist",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestManagerAddr},
			}),
			ExpectedErr: allowlist.ErrEnabledAndManagerAddress,
		},
		{
			Name: "invalid allow list config with manager role before activation",
			Config: mkConfigWithUpgradeAndAllowList(module, &allowlist.AllowListConfig{
				ManagerAddresses: []common.Address{TestManagerAddr},
			}, precompileconfig.Upgrade{
				BlockTimestamp: utils.PointerTo[uint64](1),
			}),
			ChainConfig: func() precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(gomock.NewController(t))
				config.EXPECT().IsDurango(gomock.Any()).Return(false)
				return config
			}(),
			ExpectedErr: allowlist.ErrCannotAddManagersBeforeDurango,
		},
		{
			Name: "nil member allow list config in allowlist",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{}),
		},
		{
			Name: "empty member allow list config in allowlist",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{},
				ManagerAddresses: []common.Address{},
				EnabledAddresses: []common.Address{},
			}),
		},
		{
			Name: "valid allow list config in allowlist",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
		},
	}
}

func AllowListConfigEqualTests(_ testing.TB, module modules.Module) []precompiletest.ConfigEqualTest {
	return []precompiletest.ConfigEqualTest{
		{
			Name: "allowlist non-nil config and nil other",
			Config: mkConfigWithAllowList(module, &allowlist.AllowListConfig{
				AdminAddresses:   []common.Address{TestAdminAddr},
				ManagerAddresses: []common.Address{TestManagerAddr},
				EnabledAddresses: []common.Address{TestEnabledAddr},
			}),
			Expected: false,
		},
		{
			Name: "allowlist different admin",
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
		{
			Name: "allowlist different manager",
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
		{
			Name: "allowlist different enabled",
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
		{
			Name: "allowlist same config",
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

func VerifyPrecompileWithAllowListTests(t *testing.T, module modules.Module, verifyTests []precompiletest.ConfigVerifyTest) {
	t.Helper()
	tests := AllowListConfigVerifyTests(t, module)
	tests = append(tests, verifyTests...)
	precompiletest.RunVerifyTests(t, tests)
}

func EqualPrecompileWithAllowListTests(t *testing.T, module modules.Module, equalTests []precompiletest.ConfigEqualTest) {
	t.Helper()
	tests := AllowListConfigEqualTests(t, module)
	tests = append(tests, equalTests...)
	precompiletest.RunEqualTests(t, tests)
}
