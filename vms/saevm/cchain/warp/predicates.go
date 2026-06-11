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

// VerifyBlock verifies the predicates of every transaction in the block.
func VerifyBlock(
	snowContext *snow.Context,
	blockContext *block.Context, // MAY be nil
	rules *extras.Rules,
	txs []*types.Transaction,
) (predicate.BlockResults, error) {
	type result = lazyEntry[common.Hash, predicate.PrecompileResults]
	var (
		results = make([]result, 0, len(txs))
		eg      = &errgroup.Group{}
		pc      = &precompileconfig.PredicateContext{
			SnowCtx:            snowContext,
			ProposerVMBlockCtx: blockContext,
		}
	)
	eg.SetLimit(runtime.GOMAXPROCS(0))
	for _, tx := range txs {
		predicates := predicate.FromAccessList(rules, tx.AccessList())
		if len(predicates) == 0 {
			continue
		}
		if blockContext == nil {
			// This can never happen after scheduling any goroutines, so this
			// return doesn't cause any goroutine leaks.
			return nil, errNoBlockContext
		}
		results = append(results, result{
			key:   tx.Hash(),
			value: verifyTx(pc, rules.Predicaters, predicates, eg),
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("waiting for results: %w", err)
	}
	return collect(results), nil
}

type (
	// lazy defers the evaluation of a value.
	lazy[T any]         = func() T
	lazyEntry[K, V any] struct {
		key   K
		value lazy[V]
	}
)

// collect resolves each entry's lazy value into a map. It returns nil if there
// are no entries.
func collect[K comparable, V any](entries []lazyEntry[K, V]) map[K]V {
	if len(entries) == 0 {
		return nil
	}
	m := make(map[K]V, len(entries))
	for _, e := range entries {
		m[e.key] = e.value()
	}
	return m
}

// verifyTx enqueues the verification of a transaction's predicates onto eg.
// Each predicate is verified in its own goroutine.
//
// The predicate results MUST be collected after eg.Wait has returned.
func verifyTx(
	pc *precompileconfig.PredicateContext,
	contracts map[common.Address]precompileconfig.Predicater,
	predicatesByAddress map[common.Address][]predicate.Predicate,
	eg *errgroup.Group,
) lazy[predicate.PrecompileResults] {
	type result = lazyEntry[common.Address, set.Bits]
	results := make([]result, 0, len(predicatesByAddress))
	for address, predicates := range predicatesByAddress {
		results = append(results, result{
			key: address,
			value: verifyContract(
				pc,
				contracts[address],
				predicates,
				eg,
			),
		})
	}
	return func() predicate.PrecompileResults {
		return collect(results)
	}
}

// verifyContract enqueues the verification of each of a contract's predicates
// onto eg. Each predicate is verified in its own goroutine.
//
// The results MUST be collected after eg.Wait has returned.
func verifyContract(
	pc *precompileconfig.PredicateContext,
	contract precompileconfig.Predicater,
	predicates []predicate.Predicate,
	eg *errgroup.Group,
) lazy[set.Bits] {
	failures := make([]bool, len(predicates))
	for i, pred := range predicates {
		eg.Go(func() error {
			failures[i] = contract.VerifyPredicate(pc, pred) != nil
			return nil
		})
	}
	return func() set.Bits {
		r := set.NewBits()
		for i, failed := range failures {
			if failed {
				r.Add(i)
			}
		}
		return r
	}
}
