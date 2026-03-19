// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
	"github.com/ava-labs/strevm/hook"
)

type Transaction struct {
	ID       ids.ID
	Tx       *tx.Tx
	Inputs   set.Set[ids.ID]
	GasPrice uint256.Int
	Op       hook.Op
}

func (t *Transaction) AsOp() hook.Op { return t.Op }
