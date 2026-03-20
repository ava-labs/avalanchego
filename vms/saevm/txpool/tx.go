// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"github.com/ava-labs/strevm/hook"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
)

type Tx struct {
	ID       ids.ID
	Tx       *tx.Tx
	Inputs   set.Set[ids.ID]
	GasPrice uint256.Int
	Op       hook.Op
}

func NewTx(tx *tx.Tx, avaxAssetID ids.ID) (*Tx, error) {
	op, err := tx.AsOp(avaxAssetID)
	if err != nil {
		return nil, err
	}
	return &Tx{
		ID:       op.ID,
		Tx:       tx,
		Inputs:   tx.InputUTXOs(),
		GasPrice: op.GasFeeCap,
		Op:       op,
	}, nil
}

func (t *Tx) AsOp() hook.Op { return t.Op }
