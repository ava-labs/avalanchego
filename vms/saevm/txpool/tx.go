// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/strevm/hook"
)

type Transaction struct {
	Tx       *atomic.Tx
	Inputs   set.Set[ids.ID]
	GasPrice uint256.Int
}

func (t *Transaction) AsOp() hook.Op {
	// TODO: Implement this
	return hook.Op{}
}
