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

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// ErrNoBlockContext is returned by [VerifyBlockPredicates] when a transaction
// carries a predicate but no block context is available to verify it against.
var ErrNoBlockContext = errors.New("no block context")

// A PredicateVerifier verifies a single predicate of the precompile at
// `address`, returning a non-nil error for a failed predicate. Chain packages
// build one as a closure over their own predicate context and precompile
// registry; it MUST be safe for concurrent use.
type PredicateVerifier func(address common.Address, pred predicate.Predicate) error

// VerifyBlockPredicates verifies the predicates of every transaction in
// parallel, keyed for inclusion in a block header. `haveBlockContext` reports
// whether the caller has a (chain-specific) block context: it MUST be true
// whenever any transaction carries a predicate, but MAY be false otherwise.
func VerifyBlockPredicates(
	rules predicate.Predicates,
	haveBlockContext bool,
	verify PredicateVerifier,
	txs []*types.Transaction,
) (predicate.BlockResults, error) {
	type result = lazyEntry[common.Hash, predicate.PrecompileResults]
	var (
		results = make([]result, 0, len(txs))
		eg      = &errgroup.Group{}
	)
	eg.SetLimit(runtime.GOMAXPROCS(0))
	for _, tx := range txs {
		predicates := predicate.FromAccessList(rules, tx.AccessList())
		if len(predicates) == 0 {
			continue
		}
		if !haveBlockContext {
			// This can never happen after scheduling any goroutines, so this
			// doesn't leak goroutines.
			//
			// This check exists inside the loop rather than outside so that we
			// don't require a block context when there are no predicates to
			// verify.
			return nil, ErrNoBlockContext
		}
		results = append(results, result{
			key:   tx.Hash(),
			value: verifyTx(verify, predicates, eg),
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
	verify PredicateVerifier,
	predicatesByAddress map[common.Address][]predicate.Predicate,
	eg *errgroup.Group,
) lazy[predicate.PrecompileResults] {
	type result = lazyEntry[common.Address, set.Bits]
	results := make([]result, 0, len(predicatesByAddress))
	for address, predicates := range predicatesByAddress {
		results = append(results, result{
			key:   address,
			value: verifyContract(verify, address, predicates, eg),
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
	verify PredicateVerifier,
	address common.Address,
	predicates []predicate.Predicate,
	eg *errgroup.Group,
) lazy[set.Bits] {
	failures := make([]bool, len(predicates))
	for i, pred := range predicates {
		eg.Go(func() error {
			// TODO(StephenButtolph): Properly report unexpected errors when
			// cleaning up the coreth precompile code.
			failures[i] = verify(address, pred) != nil
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
