// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import "github.com/ava-labs/avalanchego/vms/platformvm/txs"

// Camino Visitor implementations
// Standard
func (e *StandardTxExecutor) AddAddressStateTx(*txs.AddAddressStateTx) error { return errWrongTxType }

// Proposal
func (*ProposalTxExecutor) AddAddressStateTx(*txs.AddAddressStateTx) error { return errWrongTxType }

// Atomic
func (*AtomicTxExecutor) AddAddressStateTx(*txs.AddAddressStateTx) error { return errWrongTxType }

// MemPool
func (v *MempoolTxVerifier) AddAddressStateTx(tx *txs.AddAddressStateTx) error {
	return v.standardTx(tx)
}
