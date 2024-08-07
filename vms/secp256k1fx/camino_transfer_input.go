// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import "golang.org/x/exp/slices"

// Used in vms/platformvm/txs/executor/camino_tx_executor.go func inputsAreEqual
func (in *TransferInput) Equal(to any) bool {
	toIn, ok := to.(*TransferInput)
	return ok && in.Amt == toIn.Amt && slices.Equal(in.SigIndices, toIn.SigIndices)
}
