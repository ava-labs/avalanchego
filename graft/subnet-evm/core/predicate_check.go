// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"

	ethcore "github.com/ava-labs/libevm/core"
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
) (warpmsg.BlockResults, error) {
	var results warpmsg.BlockResults
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
) (warpmsg.TxResults, error) {
	// Check that the transaction can cover its IntrinsicGas, including the gas
	// required by the predicate, before verifying the predicate.
	accessList := tx.AccessList()
	intrinsicGas, err := ethcore.IntrinsicGas(tx.Data(), accessList, tx.To() == nil, rules)
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
	warpMessages := rulesExtra.WarpMessages(accessList)
	// If there are no Warp messages to verify, return early and skip requiring
	// the proposervm block context to be populated.
	if len(warpMessages) == 0 {
		return nil, nil
	}

	if predicateContext == nil || predicateContext.ProposerVMBlockCtx == nil {
		return nil, ErrMissingPredicateContext
	}

	bitset := set.NewBits()
	for i, msg := range warpMessages {
		if err := rulesExtra.WarpConfig.VerifyPredicate(predicateContext, msg); err != nil {
			bitset.Add(i)
		}
	}
	log.Debug("Warp message verify",
		"tx", tx.Hash(),
		"res", bitset.String(),
	)
	return warpmsg.TxResults{warp.ContractAddress: bitset}, nil
}
