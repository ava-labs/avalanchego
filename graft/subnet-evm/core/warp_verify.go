// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"

	ethcore "github.com/ava-labs/libevm/core"
)

var ErrMissingVerifyContext = errors.New("missing verify context")

// VerifyBlockWarpMessages verifies the Warp messages of txs and returns the
// results.
//
// Returning an error invalidates the block.
func VerifyBlockWarpMessages(
	rules params.Rules,
	verifyCtx *warpmsg.VerifyContext,
	txs []*types.Transaction,
) (warpmsg.BlockResults, error) {
	var results warpmsg.BlockResults
	// TODO: Verify txs concurrently.
	for _, tx := range txs {
		txResults, ok, err := VerifyTxWarpMessages(rules, verifyCtx, tx)
		if err != nil {
			return nil, err
		}
		if ok {
			results.Set(tx.Hash(), txResults)
		}
	}
	return results, nil
}

// VerifyTxWarpMessages verifies the Warp messages of a transaction and returns the
// indexes of the messages that failed verification. It returns false if the
// transaction has no Warp messages, in which case it has no results.
//
// Returning an error invalidates the transaction.
func VerifyTxWarpMessages(
	rules params.Rules,
	verifyCtx *warpmsg.VerifyContext,
	tx *types.Transaction,
) (set.Bits, bool, error) {
	// Check that the transaction can cover its IntrinsicGas, including the gas
	// required by its Warp messages, before verifying them.
	accessList := tx.AccessList()
	intrinsicGas, err := ethcore.IntrinsicGas(tx.Data(), accessList, tx.To() == nil, rules)
	if err != nil {
		return set.Bits{}, false, err
	}
	if gas := tx.Gas(); gas < intrinsicGas {
		return set.Bits{}, false, fmt.Errorf("%w for predicate verification (%d) < intrinsic gas (%d)",
			ErrIntrinsicGas,
			gas,
			intrinsicGas,
		)
	}

	rulesExtra := params.GetRulesExtra(rules)
	warpMessages := rulesExtra.WarpMessages(accessList)
	// If there are no Warp messages to verify, return early and skip requiring
	// the proposervm block context to be populated.
	if len(warpMessages) == 0 {
		return set.Bits{}, false, nil
	}

	if verifyCtx == nil || verifyCtx.ProposerVMBlockCtx == nil {
		return set.Bits{}, false, ErrMissingVerifyContext
	}

	bitset := set.NewBits()
	for i, msg := range warpMessages {
		if err := rulesExtra.WarpConfig.VerifyMessage(verifyCtx, msg); err != nil {
			bitset.Add(i)
		}
	}
	log.Debug("Warp message verify",
		"tx", tx.Hash(),
		"res", bitset.String(),
	)
	return bitset, true, nil
}
