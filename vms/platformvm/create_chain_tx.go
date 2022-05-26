// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ StatefulDecisionTx = &StatefulCreateChainTx{}

const (
	maxNameLen    = 128
	maxGenesisLen = units.MiB
)

// StatefulCreateChainTx is an unsigned CreateChainTx
type StatefulCreateChainTx struct {
	*unsigned.CreateChainTx `serialize:"true"`

	txID ids.ID // ID of signed create subnet tx
}

func (tx *StatefulCreateChainTx) InputUTXOs() ids.Set { return nil }

func (tx *StatefulCreateChainTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	return ids.ID{}, nil, nil
}

// Attempts to verify this transaction with the provided state.
func (tx *StatefulCreateChainTx) SemanticVerify(vm *VM, parentState state.Mutable, stx *signed.Tx) error {
	vs := state.NewVersioned(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)
	_, err := tx.Execute(vm, vs, stx)
	return err
}

// Execute this transaction.
func (tx *StatefulCreateChainTx) Execute(
	vm *VM,
	vs state.Versioned,
	stx *signed.Tx,
) (
	func() error,
	error,
) {
	// Make sure this transaction is well formed.
	if len(stx.Creds) == 0 {
		return nil, unsigned.ErrWrongNumberOfCredentials
	}

	if err := stx.SyntacticVerify(vm.ctx); err != nil {
		return nil, err
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(stx.Creds) - 1
	baseTxCreds := stx.Creds[:baseTxCredsLen]
	subnetCred := stx.Creds[baseTxCredsLen]

	// Verify the flowcheck
	timestamp := vs.GetTimestamp()
	createBlockchainTxFee := vm.getCreateBlockchainTxFee(timestamp)
	if err := vm.semanticVerifySpend(
		vs,
		tx.CreateChainTx,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		createBlockchainTxFee,
		vm.ctx.AVAXAssetID,
	); err != nil {
		return nil, err
	}

	subnetIntf, _, err := vs.GetTx(tx.SubnetID)
	if err == database.ErrNotFound {
		return nil, fmt.Errorf("%s isn't a known subnet", tx.SubnetID)
	}
	if err != nil {
		return nil, err
	}

	subnet, ok := subnetIntf.Unsigned.(*unsigned.CreateSubnetTx)
	if !ok {
		return nil, fmt.Errorf("%s isn't a subnet", tx.SubnetID)
	}

	// Verify that this chain is authorized by the subnet
	if err := vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return nil, err
	}

	// Consume the UTXOS
	consumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	produceOutputs(vs, tx.txID, vm.ctx.AVAXAssetID, tx.Outs)
	// Attempt to the new chain to the database
	vs.AddChain(stx)

	// If this proposal is committed and this node is a member of the
	// subnet that validates the blockchain, create the blockchain
	onAccept := func() error { return vm.createChain(stx) }
	return onAccept, nil
}

// Create a new transaction
func (vm *VM) newCreateChainTx(
	subnetID ids.ID, // ID of the subnet that validates the new chain
	genesisData []byte, // Byte repr. of genesis state of the new chain
	vmID ids.ID, // VM this chain runs
	fxIDs []ids.ID, // fxs this chain supports
	chainName string, // Name of the chain
	keys []*crypto.PrivateKeySECP256K1R, // Keys to sign the tx
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	timestamp := vm.internalState.GetTimestamp()
	createBlockchainTxFee := vm.getCreateBlockchainTxFee(timestamp)
	ins, outs, _, signers, err := vm.stake(keys, 0, createBlockchainTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	subnetAuth, subnetSigners, err := vm.authorize(vm.internalState, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	signers = append(signers, subnetSigners)

	// Sort the provided fxIDs
	ids.SortIDs(fxIDs)

	// Create the tx
	utx := &unsigned.CreateChainTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		SubnetID:    subnetID,
		ChainName:   chainName,
		VMID:        vmID,
		FxIDs:       fxIDs,
		GenesisData: genesisData,
		SubnetAuth:  subnetAuth,
	}
	tx, err := signed.NewSigned(utx, unsigned.Codec, signers)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(vm.ctx)
}

func (vm *VM) getCreateBlockchainTxFee(t time.Time) uint64 {
	if t.Before(vm.ApricotPhase3Time) {
		return vm.CreateAssetTxFee
	}
	return vm.CreateBlockchainTxFee
}
