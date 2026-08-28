// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"

	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
)

// VerifyBlock verifies the predicates of every transaction in the block
// against subnet-evm's precompile registry.
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
