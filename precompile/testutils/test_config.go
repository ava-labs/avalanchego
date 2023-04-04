// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutils

import (
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
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
