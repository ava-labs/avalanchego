// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

var ErrMissingPredicateContext = errors.New("missing predicate context")

// CheckBlockPredicates verifies the predicates of a block of transactions and
// returns the results.
//
// Returning an error invalidates the block.
func CheckBlockPredicates(
	rules params.Rules,
	predicateContext *precompileconfig.PredicateContext,
	txs []*types.Transaction,
) (predicate.BlockResults, error) {
	var results predicate.BlockResults
	// TODO: Calculate tx predicates concurrently.
	for _, tx := range txs {
		txResults, err := CheckTxPredicates(rules, predicateContext, tx)
		if err != nil {
			return nil, err
		}
		results.Set(tx.Hash(), txResults)
	}
	return results, nil
}

// CheckTxPredicates verifies the predicates of a transaction and returns the
// results.
//
// Returning an error invalidates the transaction.
func CheckTxPredicates(
	rules params.Rules,
	predicateContext *precompileconfig.PredicateContext,
	tx *types.Transaction,
) (predicate.PrecompileResults, error) {
	// Check that the transaction can cover its IntrinsicGas, including the gas
	// required by the predicate, before verifying the predicate.
	accessList := tx.AccessList()
	intrinsicGas, err := IntrinsicGas(tx.Data(), accessList, tx.To() == nil, rules)
	if err != nil {
		return nil, err
	}
	if gas := tx.Gas(); gas < intrinsicGas {
		return nil, fmt.Errorf("%w for predicate verification (%d) < intrinsic gas (%d)",
			ErrIntrinsicGas,
			gas,
			intrinsicGas,
		)
	}

	rulesExtra := params.GetRulesExtra(rules)
	// Short circuit early if there are no precompile predicates to verify
	if !rulesExtra.PredicatersExist() {
		return nil, nil
	}

	// Prepare the predicate storage slots from the transaction's access list
	predicateArguments := predicate.FromAccessList(rulesExtra, accessList)

	// If there are no predicates to verify, return early and skip requiring the
	// proposervm block context to be populated.
	if len(predicateArguments) == 0 {
		return nil, nil
	}

	if predicateContext == nil || predicateContext.ProposerVMBlockCtx == nil {
		return nil, ErrMissingPredicateContext
	}

	var (
		txHash           = tx.Hash()
		predicateResults = make(predicate.PrecompileResults, len(predicateArguments))
	)
	for address, predicates := range predicateArguments {
		// Since address is only added to predicateArguments when there's a
		// valid predicate in the ruleset there's no need to check if the
		// predicate exists here.
		predicaterContract := rulesExtra.Predicaters[address]
		bitset := set.NewBits()
		for i, predicate := range predicates {
			if err := predicaterContract.VerifyPredicate(predicateContext, predicate); err != nil {
				bitset.Add(i)
			}
		}
		log.Debug("predicate verify",
			"tx", txHash,
			"address", address,
			"res", bitset.String(),
		)
		predicateResults[address] = bitset
	}
	return predicateResults, nil
}
