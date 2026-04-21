// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// PredicateBytes returns the marshalled predicate results of a block of
// transactions.
func PredicateBytes(
	snowContext *snow.Context,
	blockContext *block.Context, // MAY be nil
	rules *extras.Rules,
	txs []*types.Transaction,
) ([]byte, error) {
	if !rules.PredicatersExist() {
		return nil, nil
	}

	predicateResults, err := blockPredicates(snowContext, blockContext, rules, txs)
	if err != nil {
		return nil, fmt.Errorf("block predicates: %w", err)
	}
	return predicateResults.Bytes()
}

func blockPredicates(
	snowContext *snow.Context,
	blockContext *block.Context, // MAY be nil
	rules *extras.Rules,
	txs []*types.Transaction,
) (predicate.BlockResults, error) {
	var results predicate.BlockResults
	// TODO: Calculate tx predicates concurrently.
	for _, tx := range txs {
		txResults, err := txPredicates(snowContext, blockContext, rules, tx)
		if err != nil {
			return nil, err
		}
		results.Set(tx.Hash(), txResults)
	}
	return results, nil
}

var errNoBlockContext = errors.New("no block context")

func txPredicates(
	snowContext *snow.Context,
	blockContext *block.Context, // MAY be nil
	rules *extras.Rules,
	tx *types.Transaction,
) (predicate.PrecompileResults, error) {
	predicates := predicate.FromAccessList(rules, tx.AccessList())
	// Only require require the block context to be populated when the tx has
	// predicates.
	if len(predicates) == 0 {
		return nil, nil
	}

	if blockContext == nil {
		return nil, errNoBlockContext
	}

	var (
		context = precompileconfig.PredicateContext{
			SnowCtx:            snowContext,
			ProposerVMBlockCtx: blockContext,
		}
		txHash  = tx.Hash()
		results = make(predicate.PrecompileResults, len(predicates))
	)
	for address, contractPredicates := range predicates {
		// Since address is only added to predicateArguments when there's a
		// valid predicate in the ruleset there's no need to check if the
		// predicate exists here.
		predicaterContract := rules.Predicaters[address]
		bitset := set.NewBits()
		for i, predicate := range contractPredicates {
			if err := predicaterContract.VerifyPredicate(&context, predicate); err != nil {
				bitset.Add(i)
			}
		}
		snowContext.Log.Debug("verified predicates",
			zap.Stringer("txHash", txHash),
			zap.Stringer("address", address),
			zap.Stringer("results", bitset),
		)
		results[address] = bitset
	}
	return results, nil
}
