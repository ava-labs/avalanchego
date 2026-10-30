// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warp handles the parsing, verification, and storage of Avalanche Warp
// Messages.
package warp

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// FromReceipts returns the outbound messages included in receipts.
func FromReceipts(rs types.Receipts) ([]*avalanchewarp.UnsignedMessage, error) {
	var messages []*avalanchewarp.UnsignedMessage
	for _, r := range rs {
		for _, log := range r.Logs {
			if log.Address != corethwarp.ContractAddress {
				continue
			}

			m, err := corethwarp.UnpackSendWarpEventDataToMessage(log.Data)
			if err != nil {
				return nil, fmt.Errorf("parsing log data into warp message (TxHash: %s, LogIndex: %d): %w", log.TxHash, log.Index, err)
			}
			messages = append(messages, m)
		}
	}
	return messages, nil
}

var errNoBlockContext = errors.New("no block context")

// VerifyBlock verifies the Warp messages of every transaction in the block.
func VerifyBlock(
	snowContext *snow.Context,
	blockContext *block.Context, // MAY be nil
	rules *corethparams.RulesExtra,
	txs []*types.Transaction,
) (warpmsg.BlockResults, error) {
	type result = lazyEntry[common.Hash, warpmsg.TxResults]
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
		messages := rules.WarpMessages(tx.AccessList())
		if len(messages) == 0 {
			continue
		}
		if blockContext == nil {
			// This can never happen after scheduling any goroutines, so this
			// doesn't leak goroutines.
			//
			// This check exists inside the loop rather than outside so that we
			// don't require a non-nil block context when there are no Warp
			// messages to verify.
			return nil, errNoBlockContext
		}
		results = append(results, result{
			key:   tx.Hash(),
			value: verifyTx(pc, messages, eg),
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

// verifyTx enqueues the verification of a transaction's Warp messages onto eg.
// Each message is verified in its own goroutine.
//
// The results MUST be collected after eg.Wait has returned.
func verifyTx(
	pc *precompileconfig.PredicateContext,
	messages []warpmsg.Packed,
	eg *errgroup.Group,
) lazy[warpmsg.TxResults] {
	failures := make([]bool, len(messages))
	for i, msg := range messages {
		eg.Go(func() error {
			// TODO(StephenButtolph): Properly report unexpected errors when
			// cleaning up the coreth precompile code.
			failures[i] = corethwarp.VerifyPredicate(pc, msg) != nil
			return nil
		})
	}
	return func() warpmsg.TxResults {
		r := set.NewBits()
		for i, failed := range failures {
			if failed {
				r.Add(i)
			}
		}
		return warpmsg.TxResults{corethwarp.ContractAddress: r}
	}
}
