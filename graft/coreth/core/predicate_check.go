// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var ErrMissingPredicateContext = errors.New("missing predicate context")

// CheckPredicates verifies the predicates of [tx] and returns the result. Returning an error invalidates the block.
func CheckPredicates(rules params.Rules, predicateContext *precompileconfig.PredicateContext, tx *types.Transaction) (map[common.Address][]byte, error) {
	// Check that the transaction can cover its IntrinsicGas (including the gas required by the predicate) before
	// verifying the predicate.
	intrinsicGas, err := IntrinsicGas(tx.Data(), tx.AccessList(), tx.To() == nil, rules)
	if err != nil {
		return nil, err
	}
	if tx.Gas() < intrinsicGas {
		return nil, fmt.Errorf("%w for predicate verification (%d) < intrinsic gas (%d)", ErrIntrinsicGas, tx.Gas(), intrinsicGas)
	}

	predicateResults := make(map[common.Address][]byte)
	// Short circuit early if there are no precompile predicates to verify
	if len(rules.Predicaters) == 0 {
		return predicateResults, nil
	}

	// Prepare the predicate storage slots from the transaction's access list
	predicateArguments := predicate.PreparePredicateStorageSlots(rules, tx.AccessList())

	// If there are no predicates to verify, return early and skip requiring the proposervm block
	// context to be populated.
	if len(predicateArguments) == 0 {
		return predicateResults, nil
	}

	if predicateContext == nil || predicateContext.ProposerVMBlockCtx == nil {
		return nil, ErrMissingPredicateContext
	}

	for address, predicates := range predicateArguments {
		// Since [address] is only added to [predicateArguments] when there's a valid predicate in the ruleset
		// there's no need to check if the predicate exists here.
		predicaterContract := rules.Predicaters[address]
		res := predicaterContract.VerifyPredicate(predicateContext, predicates)
		log.Debug("predicate verify", "tx", tx.Hash(), "address", address, "res", res)
		predicateResults[address] = res
	}

	return predicateResults, nil
}
