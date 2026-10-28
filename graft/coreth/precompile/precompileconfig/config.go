// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package precompileconfig re-exports predicate types from graft/evm/precompileconfig.
package precompileconfig

import evmprecompileconfig "github.com/ava-labs/avalanchego/graft/evm/precompileconfig"

// PredicateContext is the context that Warp messages are verified within.
type PredicateContext = evmprecompileconfig.PredicateContext

// Rules defines the interface that provides information about the current rules of the chain.
type Rules = evmprecompileconfig.Rules
