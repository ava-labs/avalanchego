// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package precompileconfig re-exports predicate types from graft/evm/precompileconfig.
package precompileconfig

import evmprecompileconfig "github.com/ava-labs/avalanchego/graft/evm/precompileconfig"

// PredicateContext is the context passed in to the Predicater interface to verify
// a precompile predicate within a specific ProposerVM wrapper.
type PredicateContext = evmprecompileconfig.PredicateContext

// Predicater is an optional interface for StatefulPrecompileContracts to implement.
// If implemented, the predicate will be called for each predicate included in the
// access list of a transaction.
// PredicateGas will be called while calculating the IntrinsicGas of a transaction
// causing it to be dropped if the total gas goes above the tx gas limit.
// VerifyPredicate is used to populate a bit set of predicates verified prior to
// block execution, which can be accessed via the StateDB during execution.
// The bitset is stored in the block, so that historical blocks can be re-verified
// without calling VerifyPredicate.
type Predicater = evmprecompileconfig.Predicater

// Rules defines the interface that provides information about the current rules of the chain.
type Rules = evmprecompileconfig.Rules
