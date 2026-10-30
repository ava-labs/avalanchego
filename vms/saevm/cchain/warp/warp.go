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
			if log.Address != warpmsg.ContractAddress {
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
	var (
		pending   = make([]pendingTx, 0, len(txs))
		eg        = &errgroup.Group{}
		verifyCtx = &warpmsg.VerifyContext{
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
		pending = append(pending, pendingTx{
			hash:     tx.Hash(),
			failures: verifyTx(verifyCtx, messages, eg),
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("waiting for results: %w", err)
	}
	if len(pending) == 0 {
		return nil, nil
	}

	results := make(warpmsg.BlockResults, len(pending))
	for _, p := range pending {
		bits := set.NewBits()
		for i, failed := range p.failures {
			if failed {
				bits.Add(i)
			}
		}
		results[p.hash] = bits
	}
	return results, nil
}

// pendingTx is a transaction whose Warp messages are being verified.
type pendingTx struct {
	hash     common.Hash
	failures []bool
}

// verifyTx enqueues the verification of a transaction's Warp messages onto eg.
// Each message is verified in its own goroutine. The i-th returned value
// reports whether the i-th message failed verification.
//
// The returned slice MUST only be read after eg.Wait has returned.
func verifyTx(
	verifyCtx *warpmsg.VerifyContext,
	messages []warpmsg.Packed,
	eg *errgroup.Group,
) []bool {
	failures := make([]bool, len(messages))
	for i, msg := range messages {
		eg.Go(func() error {
			// TODO(StephenButtolph): Properly report unexpected errors when
			// cleaning up the coreth precompile code.
			failures[i] = corethwarp.VerifyMessage(verifyCtx, msg) != nil
			return nil
		})
	}
	return failures
}
