// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Issuer
func (i *issuer) AddAddressStateTx(*txs.AddAddressStateTx) error {
	i.m.addStakerTx(i.tx)
	return nil
}

// Remover
func (r *remover) AddAddressStateTx(*txs.AddAddressStateTx) error {
	r.m.removeStakerTx(r.tx)
	return nil
}
