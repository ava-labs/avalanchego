// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompiletest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

// ConfigVerifyTest is a test case for verifying a config
type ConfigVerifyTest struct {
	Name          string
	Config        precompileconfig.Config
	ChainConfig   precompileconfig.ChainConfig
	ExpectedErr error
}

// ConfigEqualTest is a test case for comparing two configs
type ConfigEqualTest struct {
	Name     string
	Config   precompileconfig.Config
	Other    precompileconfig.Config
	Expected bool
}

func requireUniqueNames(t *testing.T, n int, name func(int) string) {
	t.Helper()
	seen := make(map[string]struct{}, n)
	for i := range n {
		s := name(i)
		require.NotContains(t, seen, s, "duplicate test name: %s", s)
		seen[s] = struct{}{}
	}
}

func RunVerifyTests(t *testing.T, tests []ConfigVerifyTest) {
	requireUniqueNames(t, len(tests), func(i int) string { return tests[i].Name })
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Helper()
			require := require.New(t)

			chainConfig := test.ChainConfig
			if chainConfig == nil {
				ctrl := gomock.NewController(t)
				mockChainConfig := precompileconfig.NewMockChainConfig(ctrl)
				mockChainConfig.EXPECT().GetFeeConfig().AnyTimes().Return(commontype.ValidTestFeeConfig)
				mockChainConfig.EXPECT().AllowedFeeRecipients().AnyTimes().Return(false)
				mockChainConfig.EXPECT().IsDurango(gomock.Any()).AnyTimes().Return(true)
				chainConfig = mockChainConfig
			}
			err := test.Config.Verify(chainConfig)
			require.ErrorIs(err, test.ExpectedErr)
		})
	}
}

func RunEqualTests(t *testing.T, tests []ConfigEqualTest) {
	requireUniqueNames(t, len(tests), func(i int) string { return tests[i].Name })
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Helper()
			require := require.New(t)

			require.Equal(test.Expected, test.Config.Equal(test.Other))
		})
	}
}
