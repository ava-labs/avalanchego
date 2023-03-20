// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ Message = (*TxGossip)(nil)

type TxGossip struct {
	message

	Tx []byte `serialize:"true"`
}

func (msg *TxGossip) Handle(handler Handler, nodeID ids.NodeID, _ uint32) error {
	return handler.HandleTxGossip(nodeID, msg)
}
