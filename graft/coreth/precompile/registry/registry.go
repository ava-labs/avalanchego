// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Module to facilitate the registration of precompiles and their configuration.
package registry

// Force imports of each precompile to ensure each precompile's init function runs and registers itself
// with the registry.
import (
	_ "github.com/ava-labs/coreth/precompile/contracts/warp"
)
