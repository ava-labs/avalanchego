// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/ids"
)

var _ Message = (*Tx)(nil)

type Tx struct {
	message

	Tx []byte `serialize:"true"`
}

func (msg *Tx) Handle(handler Handler, nodeID ids.NodeID, requestID uint32) error {
	return handler.HandleTx(nodeID, requestID, msg)
}
