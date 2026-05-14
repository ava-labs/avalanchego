// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

// cchainTx adapts a [tx.Tx] to the no-arg [hook.Transaction] interface required
// by [hook.PointsG]. The [hook.Op] is precomputed to bind the configured
// AVAX asset ID.
type cchainTx struct {
	ID     ids.ID
	Tx     *tx.Tx
	Inputs set.Set[ids.ID]
	Op     hook.Op
}

func newCChainTx(t *tx.Tx, avaxAssetID ids.ID) (*cchainTx, error) {
	op, err := t.AsOp(avaxAssetID)
	if err != nil {
		return nil, err
	}
	return &cchainTx{
		ID:     op.ID,
		Tx:     t,
		Inputs: t.InputIDs(),
		Op:     op,
	}, nil
}

func (t *cchainTx) AsOp() hook.Op { return t.Op }
