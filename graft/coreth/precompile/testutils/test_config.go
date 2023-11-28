// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutils

import (
	"testing"

	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// ConfigVerifyTest is a test case for verifying a config
type ConfigVerifyTest struct {
	Config        precompileconfig.Config
	ChainConfig   precompileconfig.ChainConfig
	ExpectedError string
}

// ConfigEqualTest is a test case for comparing two configs
type ConfigEqualTest struct {
	Config   precompileconfig.Config
	Other    precompileconfig.Config
	Expected bool
}

func RunVerifyTests(t *testing.T, tests map[string]ConfigVerifyTest) {
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Helper()
			require := require.New(t)

			chainConfig := test.ChainConfig
			if chainConfig == nil {
				ctrl := gomock.NewController(t)
				mockChainConfig := precompileconfig.NewMockChainConfig(ctrl)
				mockChainConfig.EXPECT().IsDUpgrade(gomock.Any()).AnyTimes().Return(true)
				chainConfig = mockChainConfig
			}
			err := test.Config.Verify(chainConfig)
			if test.ExpectedError == "" {
				require.NoError(err)
			} else {
				require.ErrorContains(err, test.ExpectedError)
			}
		})
	}
}

func RunEqualTests(t *testing.T, tests map[string]ConfigEqualTest) {
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Helper()
			require := require.New(t)

			require.Equal(test.Expected, test.Config.Equal(test.Other))
		})
	}
}
