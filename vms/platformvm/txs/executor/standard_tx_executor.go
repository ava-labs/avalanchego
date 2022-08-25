// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
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

var (
	_ txs.Visitor = &StandardTxExecutor{}

	errCustomAssetBeforeBlueberry = errors.New("custom assets can only be imported after the blueberry upgrade")
)

type StandardTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	State state.Diff // state is expected to be modified
	Tx    *txs.Tx

	// outputs of visitor execution
	OnAccept       func() // may be nil
	Inputs         ids.Set
	AtomicRequests map[ids.ID]*atomic.Requests // may be nil
}

func (*StandardTxExecutor) AddValidatorTx(*txs.AddValidatorTx) error { return errWrongTxType }
func (*StandardTxExecutor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return errWrongTxType
}
func (*StandardTxExecutor) AddDelegatorTx(*txs.AddDelegatorTx) error       { return errWrongTxType }
func (*StandardTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error         { return errWrongTxType }
func (*StandardTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error { return errWrongTxType }

func (e *StandardTxExecutor) CreateChainTx(tx *txs.CreateChainTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	// Make sure this transaction has at least one credential for the subnet
	// authorization.
	if len(e.Tx.Creds) == 0 {
		return errWrongNumberOfCredentials
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(e.Tx.Creds) - 1
	baseTxCreds := e.Tx.Creds[:baseTxCredsLen]
	subnetCred := e.Tx.Creds[baseTxCredsLen]

	// Verify the flowcheck
	timestamp := e.State.GetTimestamp()
	createBlockchainTxFee := e.Config.GetCreateBlockchainTxFee(timestamp)
	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: createBlockchainTxFee,
		},
	); err != nil {
		return err
	}

	subnetIntf, _, err := e.State.GetTx(tx.SubnetID)
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
	if err := e.Fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	utxo.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.State, txID, tx.Outs)
	// Add the new chain to the database
	e.State.AddChain(e.Tx)

	// If this proposal is committed and this node is a member of the subnet
	// that validates the blockchain, create the blockchain
	e.OnAccept = func() { e.Config.CreateChain(txID, tx) }
	return nil
}

func (e *StandardTxExecutor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	// Make sure this transaction is well formed.
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	// Verify the flowcheck
	timestamp := e.State.GetTimestamp()
	createSubnetTxFee := e.Config.GetCreateSubnetTxFee(timestamp)
	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: createSubnetTxFee,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	utxo.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.State, txID, tx.Outs)
	// Add the new subnet to the database
	e.State.AddSubnet(e.Tx)
	return nil
}

func (e *StandardTxExecutor) ImportTx(tx *txs.ImportTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	currentChainTime := e.State.GetTimestamp()

	e.Inputs = ids.NewSet(len(tx.ImportedInputs))
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.UTXOID.InputID()

		e.Inputs.Add(utxoID)
		utxoIDs[i] = utxoID[:]

		if !e.Config.IsBlueberryActivated(currentChainTime) {
			// TODO: Remove this check once the Blueberry network upgrade is
			//       complete.
			//
			// Blueberry network upgrade allows exporting of all assets to the
			// P-chain.
			assetID := in.AssetID()
			if assetID != e.Ctx.AVAXAssetID {
				return errCustomAssetBeforeBlueberry
			}
		}
	}

	if e.Bootstrapped.GetValue() {
		if err := verify.SameSubnet(e.Ctx, tx.SourceChain); err != nil {
			return err
		}

		allUTXOBytes, err := e.Ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
		if err != nil {
			return fmt.Errorf("failed to get shared memory: %w", err)
		}

		utxos := make([]*avax.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
		for index, input := range tx.Ins {
			utxo, err := e.State.GetUTXO(input.InputID())
			if err != nil {
				return fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err)
			}
			utxos[index] = utxo
		}
		for i, utxoBytes := range allUTXOBytes {
			utxo := &avax.UTXO{}
			if _, err := txs.Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxos[i+len(tx.Ins)] = utxo
		}

		ins := make([]*avax.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
		copy(ins, tx.Ins)
		copy(ins[len(tx.Ins):], tx.ImportedInputs)

		if err := e.FlowChecker.VerifySpendUTXOs(
			tx,
			utxos,
			ins,
			tx.Outs,
			e.Tx.Creds,
			map[ids.ID]uint64{
				e.Ctx.AVAXAssetID: e.Config.TxFee,
			},
		); err != nil {
			return err
		}
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	utxo.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.State, txID, tx.Outs)

	e.AtomicRequests = map[ids.ID]*atomic.Requests{
		tx.SourceChain: {
			RemoveRequests: utxoIDs,
		},
	}
	return nil
}

func (e *StandardTxExecutor) ExportTx(tx *txs.ExportTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	if e.Bootstrapped.GetValue() {
		if err := verify.SameSubnet(e.Ctx, tx.DestinationChain); err != nil {
			return err
		}
	}

	// Verify the flowcheck
	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.Ctx.AVAXAssetID: e.Config.TxFee,
		},
	); err != nil {
		return fmt.Errorf("failed verifySpend: %w", err)
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	utxo.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	utxo.Produce(e.State, txID, tx.Outs)

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

		utxoBytes, err := txs.Codec.Marshal(txs.Version, utxo)
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
	e.AtomicRequests = map[ids.ID]*atomic.Requests{
		tx.DestinationChain: {
			PutRequests: elems,
		},
	}
	return nil
}
