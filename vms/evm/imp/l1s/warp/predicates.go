// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warp provides the L1-specific glue around the shared SAE warp
// implementation (vms/evm/imp/warp): predicate verification against the legacy
// precompile registry, precompile-accept handling, and validator-uptime
// attestation signing.
package warp

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	saewarp "github.com/ava-labs/avalanchego/vms/evm/imp/warp"
)

// VerifyBlock verifies the predicates of every transaction in the block
// against the legacy precompile registry.
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
