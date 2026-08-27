// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warp provides the C-Chain-specific glue around the shared SAE warp
// implementation ([saewarp]): extraction of outbound messages from receipts
// and predicate verification against coreth's precompile registry.
package warp

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
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

// VerifyBlock verifies the predicates of every transaction in the block.
func VerifyBlock(
	snowContext *snow.Context,
	blockContext *block.Context, // MAY be nil
	rules *extras.Rules,
	txs []*types.Transaction,
) (predicate.BlockResults, error) {
	pc := &precompileconfig.PredicateContext{
		SnowCtx:            snowContext,
		ProposerVMBlockCtx: blockContext,
	}
	return saewarp.VerifyBlockPredicates(
		rules,
		blockContext != nil,
		func(address common.Address, pred predicate.Predicate) error {
			return rules.Predicaters[address].VerifyPredicate(pc, pred)
		},
		txs,
	)
}
