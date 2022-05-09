// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ UnsignedDecisionTx = &StatefulCreateSubnetTx{}

// StatefulCreateSubnetTx is an unsigned proposal to create a new subnet
type StatefulCreateSubnetTx struct {
	*unsigned.CreateSubnetTx `serialize:"true"`
}

// InputUTXOs for [DecisionTxs] will return an empty set to diffrentiate from the [AtomicTxs] input UTXOs
func (tx *StatefulCreateSubnetTx) InputUTXOs() ids.Set { return nil }

func (tx *StatefulCreateSubnetTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	return ids.ID{}, nil, nil
}

// Attempts to verify this transaction with the provided state.
func (tx *StatefulCreateSubnetTx) SemanticVerify(vm *VM, parentState MutableState, stx *signed.Tx) error {
	vs := newVersionedState(
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
	vs VersionedState,
	stx *signed.Tx,
) (
	func() error,
	error,
) {
	// Make sure this transaction is well formed.
	if err := tx.SyntacticVerify(vm.ctx); err != nil {
		return nil, err
	}

	// Verify the flowcheck
	timestamp := vs.GetTimestamp()
	createSubnetTxFee := vm.getCreateSubnetTxFee(timestamp)
	if err := vm.semanticVerifySpend(vs, tx, tx.Ins, tx.Outs, stx.Creds, createSubnetTxFee, vm.ctx.AVAXAssetID); err != nil {
		return nil, err
	}

	// Consume the UTXOS
	consumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	produceOutputs(vs, txID, vm.ctx.AVAXAssetID, tx.Outs)
	// Attempt to the new chain to the database
	vs.AddSubnet(stx)

	return nil, nil
}

// [controlKeys] must be unique. They will be sorted by this method.
// If [controlKeys] is nil, [tx.Controlkeys] will be an empty list.
func (vm *VM) newCreateSubnetTx(
	threshold uint32, // [threshold] of [ownerAddrs] needed to manage this subnet
	ownerAddrs []ids.ShortID, // control addresses for the new subnet
	keys []*crypto.PrivateKeySECP256K1R, // pay the fee
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	timestamp := vm.internalState.GetTimestamp()
	createSubnetTxFee := vm.getCreateSubnetTxFee(timestamp)
	ins, outs, _, signers, err := vm.stake(keys, 0, createSubnetTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Sort control addresses
	ids.SortShortIDs(ownerAddrs)

	// Create the tx
	utx := &unsigned.CreateSubnetTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Owner: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     ownerAddrs,
		},
	}
	tx := &signed.Tx{Unsigned: utx}
	if err := tx.Sign(Codec, signers); err != nil {
		return nil, err
	}

	return tx, utx.SyntacticVerify(vm.ctx)
}

func (vm *VM) getCreateSubnetTxFee(t time.Time) uint64 {
	if t.Before(vm.ApricotPhase3Time) {
		return vm.CreateAssetTxFee
	}
	return vm.CreateSubnetTxFee
}
