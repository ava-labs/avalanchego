// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var _ StatefulDecisionTx = &StatefulCreateSubnetTx{}

// StatefulCreateSubnetTx is an unsigned proposal to create a new subnet
type StatefulCreateSubnetTx struct {
	*unsigned.CreateSubnetTx `serialize:"true"`

	txID ids.ID // ID of signed create subnet tx
}

// InputUTXOs for [DecisionTxs] will return an empty set to diffrentiate from the [AtomicTxs] input UTXOs
func (tx *StatefulCreateSubnetTx) InputUTXOs() ids.Set { return nil }

func (tx *StatefulCreateSubnetTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	return ids.ID{}, nil, nil
}

// Attempts to verify this transaction with the provided state.
func (tx *StatefulCreateSubnetTx) SemanticVerify(vm *VM, parentState state.Mutable, stx *signed.Tx) error {
	vs := state.NewVersioned(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)
	_, err := tx.Execute(vm, vs, stx)
	return err
}

// Execute this transaction.
func (tx *StatefulCreateSubnetTx) Execute(
	vm *VM,
	vs state.Versioned,
	stx *signed.Tx,
) (
	func() error,
	error,
) {
	// Make sure this transaction is well formed.
	if err := stx.SyntacticVerify(vm.ctx); err != nil {
		return nil, err
	}

	// Verify the flowcheck
	timestamp := vs.GetTimestamp()
	createSubnetTxFee := vm.getCreateSubnetTxFee(timestamp)
	if err := vm.spendOps.SemanticVerifySpend(
		vs,
		tx.CreateSubnetTx,
		tx.Ins,
		tx.Outs,
		stx.Creds,
		createSubnetTxFee,
		vm.ctx.AVAXAssetID,
	); err != nil {
		return nil, err
	}

	// Consume the UTXOS
	utxos.ConsumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(vs, tx.txID, vm.ctx.AVAXAssetID, tx.Outs)
	// Attempt to the new chain to the database
	vs.AddSubnet(stx)

	return nil, nil
}

func (vm *VM) getCreateSubnetTxFee(t time.Time) uint64 {
	if t.Before(vm.ApricotPhase3Time) {
		return vm.CreateAssetTxFee
	}
	return vm.CreateSubnetTxFee
}
