// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
)

var _ txs.Visitor = &standardTxExecutor{}

type standardTxExecutor struct {
	// inputs
	vm    *VM
	state state.Diff // state is expected to be modified
	tx    *txs.Tx

	// outputs
	onAccept       func() // may be nil
	inputs         ids.Set
	atomicRequests map[ids.ID]*atomic.Requests // may be nil
}

func (*standardTxExecutor) AddValidatorTx(*txs.AddValidatorTx) error { return errWrongTxType }
func (*standardTxExecutor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return errWrongTxType
}
func (*standardTxExecutor) AddDelegatorTx(*txs.AddDelegatorTx) error       { return errWrongTxType }
func (*standardTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error         { return errWrongTxType }
func (*standardTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error { return errWrongTxType }

func (e *standardTxExecutor) CreateChainTx(tx *txs.CreateChainTx) error {
	if err := e.tx.SyntacticVerify(e.vm.ctx); err != nil {
		return err
	}

	// Make sure this transaction has at least one credential for the subnet
	// authorization.
	if len(e.tx.Creds) == 0 {
		return errWrongNumberOfCredentials
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(e.tx.Creds) - 1
	baseTxCreds := e.tx.Creds[:baseTxCredsLen]
	subnetCred := e.tx.Creds[baseTxCredsLen]

	// Verify the flowcheck
	timestamp := e.state.GetTimestamp()
	createBlockchainTxFee := e.vm.Config.GetCreateBlockchainTxFee(timestamp)
	if err := e.vm.utxoHandler.SemanticVerifySpend(
		tx,
		e.state,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		createBlockchainTxFee,
		e.vm.ctx.AVAXAssetID,
	); err != nil {
		return err
	}

	subnetIntf, _, err := e.state.GetTx(tx.SubnetID)
	if err == database.ErrNotFound {
		return fmt.Errorf("%s isn't a known subnet", tx.SubnetID)
	}
	if err != nil {
		return err
	}

	subnet, ok := subnetIntf.Unsigned.(*txs.CreateSubnetTx)
	if !ok {
		return fmt.Errorf("%s isn't a subnet", tx.SubnetID)
	}

	// Verify that this chain is authorized by the subnet
	if err := e.vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	utxo.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.state, txID, e.vm.ctx.AVAXAssetID, tx.Outs)
	// Add the new chain to the database
	e.state.AddChain(e.tx)

	// If this proposal is committed and this node is a member of the subnet
	// that validates the blockchain, create the blockchain
	e.onAccept = func() { e.vm.Config.CreateChain(txID, tx) }
	return nil
}

func (e *standardTxExecutor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	// Make sure this transaction is well formed.
	if err := e.tx.SyntacticVerify(e.vm.ctx); err != nil {
		return err
	}

	// Verify the flowcheck
	timestamp := e.state.GetTimestamp()
	createSubnetTxFee := e.vm.Config.GetCreateSubnetTxFee(timestamp)
	if err := e.vm.utxoHandler.SemanticVerifySpend(
		tx,
		e.state,
		tx.Ins,
		tx.Outs,
		e.tx.Creds,
		createSubnetTxFee,
		e.vm.ctx.AVAXAssetID,
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	utxo.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.state, txID, e.vm.ctx.AVAXAssetID, tx.Outs)
	// Add the new subnet to the database
	e.state.AddSubnet(e.tx)
	return nil
}

func (e *standardTxExecutor) ImportTx(tx *txs.ImportTx) error {
	if err := e.tx.SyntacticVerify(e.vm.ctx); err != nil {
		return err
	}

	e.inputs = ids.NewSet(len(tx.ImportedInputs))
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.UTXOID.InputID()

		e.inputs.Add(utxoID)
		utxoIDs[i] = utxoID[:]
	}

	if e.vm.bootstrapped.GetValue() {
		if err := verify.SameSubnet(e.vm.ctx, tx.SourceChain); err != nil {
			return err
		}

		allUTXOBytes, err := e.vm.ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
		if err != nil {
			return fmt.Errorf("failed to get shared memory: %w", err)
		}

		utxos := make([]*avax.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
		for index, input := range tx.Ins {
			utxo, err := e.state.GetUTXO(input.InputID())
			if err != nil {
				return fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err)
			}
			utxos[index] = utxo
		}
		for i, utxoBytes := range allUTXOBytes {
			utxo := &avax.UTXO{}
			if _, err := Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxos[i+len(tx.Ins)] = utxo
		}

		ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
		copy(ins, tx.Ins)
		copy(ins[len(tx.Ins):], tx.ImportedInputs)

		if err := e.vm.utxoHandler.SemanticVerifySpendUTXOs(
			tx,
			utxos,
			ins,
			tx.Outs,
			e.tx.Creds,
			e.vm.TxFee,
			e.vm.ctx.AVAXAssetID,
		); err != nil {
			return err
		}
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	utxo.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.state, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	e.atomicRequests = map[ids.ID]*atomic.Requests{
		tx.SourceChain: {
			RemoveRequests: utxoIDs,
		},
	}
	return nil
}

func (e *standardTxExecutor) ExportTx(tx *txs.ExportTx) error {
	if err := e.tx.SyntacticVerify(e.vm.ctx); err != nil {
		return err
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	if e.vm.bootstrapped.GetValue() {
		if err := verify.SameSubnet(e.vm.ctx, tx.DestinationChain); err != nil {
			return err
		}
	}

	// Verify the flowcheck
	if err := e.vm.utxoHandler.SemanticVerifySpend(
		tx,
		e.state,
		tx.Ins,
		outs,
		e.tx.Creds,
		e.vm.TxFee,
		e.vm.ctx.AVAXAssetID,
	); err != nil {
		return fmt.Errorf("failed semanticVerifySpend: %w", err)
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	utxo.Consume(e.state, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.state, txID, e.vm.ctx.AVAXAssetID, tx.Outs)

	elems := make([]*atomic.Element, len(tx.ExportedOutputs))
	for i, out := range tx.ExportedOutputs {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(tx.Outs) + i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}

		utxoBytes, err := Codec.Marshal(txs.Version, utxo)
		if err != nil {
			return fmt.Errorf("failed to marshal UTXO: %w", err)
		}
		utxoID := utxo.InputID()
		elem := &atomic.Element{
			Key:   utxoID[:],
			Value: utxoBytes,
		}
		if out, ok := utxo.Out.(avax.Addressable); ok {
			elem.Traits = out.Addresses()
		}

		elems[i] = elem
	}
	e.atomicRequests = map[ids.ID]*atomic.Requests{
		tx.DestinationChain: {
			PutRequests: elems,
		},
	}
	return nil
}
