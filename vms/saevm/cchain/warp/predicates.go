// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

var errNoBlockContext = errors.New("no block context")

// PredicateBytes returns the marshalled predicate results of a block of
// transactions.
func PredicateBytes(
	snowContext *snow.Context,
	blockContext *block.Context, // MAY be nil
	rules *extras.Rules,
	txs []*types.Transaction,
) ([]byte, error) {
	results, err := blockPredicates(snowContext, blockContext, rules, txs)
	if err != nil {
		return nil, err
	}
	return results.Bytes()
}

// blockPredicates verifies the predicates of every transaction in the block.
//
// Every predicate is verified in its own goroutine on a single pool shared by
// all transactions in the block, bounding the number of concurrent
// verifications to the number of available CPUs.
func blockPredicates(
	snowContext *snow.Context,
	blockContext *block.Context, // MAY be nil
	rules *extras.Rules,
	txs []*types.Transaction,
) (predicate.BlockResults, error) {
	if !rules.PredicatersExist() {
		return nil, nil
	}

	var (
		txResults = make([]func() (common.Hash, predicate.PrecompileResults), 0, len(txs))
		eg        = &errgroup.Group{}
		pc        = &precompileconfig.PredicateContext{
			SnowCtx:            snowContext,
			ProposerVMBlockCtx: blockContext,
		}
	)
	eg.SetLimit(runtime.GOMAXPROCS(0))
	for i, tx := range txs {
		result, err := txPredicates(pc, rules, tx, eg)
		if err != nil {
			return nil, fmt.Errorf("tx predicates %s (%d): %w", tx.Hash(), i, err)
		}
		if result != nil {
			txResults = append(txResults, result)
		}
	}
	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("waiting for predicate verification: %w", err)
	}

	var results predicate.BlockResults
	for _, result := range txResults {
		txHash, txResults := result()
		results.Set(txHash, txResults)
	}
	return results, nil
}

// txPredicates enqueues the verification of each of tx's predicates onto eg.
// Each predicate is verified in its own goroutine.
//
// It returns a function that assembles the predicate results; the function
// must only be called after eg.Wait has returned. The returned function is nil
// when tx has no predicates.
func txPredicates(
	pc *precompileconfig.PredicateContext,
	rules *extras.Rules,
	tx *types.Transaction,
	eg *errgroup.Group,
) (func() (common.Hash, predicate.PrecompileResults), error) {
	predicates := predicate.FromAccessList(rules, tx.AccessList())
	// Only require the block context to be populated when the tx has
	// predicates.
	if len(predicates) == 0 {
		return nil, nil
	}
	if pc.ProposerVMBlockCtx == nil {
		return nil, errNoBlockContext
	}

	contractResults := make([]func() (common.Address, set.Bits), 0, len(predicates))
	for address, contractPreds := range predicates {
		contract := rules.Predicaters[address]
		result := contractPredicates(pc, contract, contractPreds, address, eg)
		contractResults = append(contractResults, result)
	}

	return func() (common.Hash, predicate.PrecompileResults) {
		results := make(predicate.PrecompileResults, len(contractResults))
		for _, result := range contractResults {
			address, bitset := result()
			results[address] = bitset
		}
		return tx.Hash(), results
	}, nil
}

// contractPredicates enqueues the verification of each of a contract's
// predicates onto eg. Each predicate is verified in its own goroutine.
//
// It returns a function that assembles the contract's results; the function
// must only be called after eg.Wait has returned.
func contractPredicates(
	context *precompileconfig.PredicateContext,
	predicaterContract precompileconfig.Predicater,
	predicates []predicate.Predicate,
	address common.Address,
	eg *errgroup.Group,
) func() (common.Address, set.Bits) {
	failures := make([]bool, len(predicates))
	for i, pred := range predicates {
		eg.Go(func() error {
			failures[i] = predicaterContract.VerifyPredicate(context, pred) != nil
			return nil
		})
	}

	return func() (common.Address, set.Bits) {
		bitset := set.NewBits()
		for i, failed := range failures {
			if failed {
				bitset.Add(i)
			}
		}
		return address, bitset
	}
}
