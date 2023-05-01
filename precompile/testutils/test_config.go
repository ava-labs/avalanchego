// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutils

import (
	"testing"

	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/stretchr/testify/require"
)

// ConfigVerifyTest is a test case for verifying a config
type ConfigVerifyTest struct {
	Config        precompileconfig.Config
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

			err := test.Config.Verify()
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
