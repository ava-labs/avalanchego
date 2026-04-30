// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip"
)

// admitter is a [txgossip.Admitter] that gates inbound txs on
// [hook.Points.CanExecuteTransaction] against the LAST-EXECUTED state.
//
// Last-executed is fresher than the worst-case build path (which uses
// last-settled): the mempool rejects txs as soon as execution observes a
// permission change, instead of waiting for settlement.
//
// Each [admitter.Admit] call resolves the current head via
// [saexec.Executor.LastExecuted] (a single atomic load), consults a cached
// [admissionHead] for the per-head rules/signer, and opens a FRESH
// [state.StateDB] for the precompile read. The cache is rebuilt only when
// the head changes.
type admitter struct {
	exec   *saexec.Executor
	hooks  hook.Points
	config *params.ChainConfig

	// head caches the [admissionHead] for the most recently observed
	// last-executed block. Multiple [Admit] callers may race to refresh it
	// after a head change; the loser's work is discarded.
	head atomic.Pointer[admissionHead]
}

// admissionHead bundles the immutable per-head data used by [admitter.Admit]:
// the [params.Rules] and [types.Signer] for the head's height/time, plus a
// cheap `check` flag indicating whether the chain's hooks could possibly
// reject any tx under those rules.
type admissionHead struct {
	hash      common.Hash
	header    *types.Header
	stateRoot common.Hash
	rules     params.Rules
	check     bool // indicates whether the chain's hooks could possibly reject any tx under those rules
	signer    types.Signer
}

var _ txgossip.Admitter = (*admitter)(nil)

func newAdmitter(
	exec *saexec.Executor,
	hooks hook.Points,
	config *params.ChainConfig,
) *admitter {
	return &admitter{
		exec:   exec,
		hooks:  hooks,
		config: config,
	}
}

var errNoExecutedBlock = errors.New("no last-executed block available")

// Admit gates `tx` via [hook.Points.CanExecuteTransaction] against the
// last-executed state. Returns nil immediately if no admission precompile
// is active at the current head.
//
// A fresh [state.StateDB] is opened per call because StateDB
// lazily populates an unsynchronised `stateObjects` map even on reads and
// is NOT safe for concurrent use. The shared cache + snapshots underneath
// [saedb.Tracker.StateDB] are concurrency-safe, so each open is cheap.
func (a *admitter) Admit(tx *types.Transaction) error {
	last := a.exec.LastExecuted()
	if last == nil {
		return errNoExecutedBlock
	}

	head := a.headFor(last)
	if !head.check { // no admission is required
		return nil
	}

	// TODO(ceyonur): consider caching the state and using a mutex to protect it
	state, err := a.exec.StateDB(head.stateRoot)
	if err != nil {
		return fmt.Errorf("could not open last-executed state: %w", err)
	}

	from, err := types.Sender(head.signer, tx)
	if err != nil {
		return fmt.Errorf("determining sender: %w", err)
	}

	if err := a.hooks.CanExecuteTransaction(head.rules, from, tx.To(), state); err != nil {
		return fmt.Errorf("transaction blocked by CanExecuteTransaction hook: %w", err)
	}
	return nil
}

// headFor returns the cached [admissionHead] for `last`,
// building a new one if the cache is empty or stale.
func (a *admitter) headFor(last *blocks.Block) *admissionHead {
	hash := last.Hash()
	if cur := a.head.Load(); cur != nil && cur.hash == hash {
		return cur
	}
	fresh := a.makeHead(last)
	a.head.Store(fresh)
	return fresh
}

// makeHead builds a new [admissionHead] for `last`.
func (a *admitter) makeHead(last *blocks.Block) *admissionHead {
	header := last.Header()
	rules := a.config.Rules(header.Number, true /*isMerge*/, header.Time)
	return &admissionHead{
		hash:      last.Hash(),
		header:    header,
		stateRoot: last.PostExecutionStateRoot(),
		rules:     rules,
		check:     a.hooks.RequiresTransactionAdmissionCheck(rules),
		signer:    types.MakeSigner(a.config, header.Number, header.Time),
	}
}
