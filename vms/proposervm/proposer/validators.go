// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/utils"
)

var _ utils.Sortable[validatorData] = validatorData{}

type validatorData struct {
	id     ids.NodeID
	weight uint64
}

func (d validatorData) Compare(other validatorData) int {
	return d.id.Compare(other.id)
}
