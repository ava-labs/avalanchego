// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"

	p_utils "github.com/ava-labs/avalanchego/vms/platformvm/utils"
)

var _ DecisionExecutor = &decisionExecutor{}

type DecisionExecutor interface {
	ExecuteDecision(
		stx *signed.Tx,
		vs state.Versioned,
	) (func() error, error)

	semanticVerifyDecision(
		stx *signed.Tx,
		parentState state.Mutable,
	) error
}

type decisionExecutor struct {
	*components
}

func (de *decisionExecutor) ExecuteDecision(
	stx *signed.Tx,
	vs state.Versioned,
) (func() error, error) {
	var (
		txID        = stx.ID()
		creds       = stx.Creds
		signedBytes = stx.Bytes()
	)

	switch utx := stx.Unsigned.(type) {
	case *unsigned.CreateChainTx:
		return de.executeCreateChain(vs, utx, txID, signedBytes, creds)
	case *unsigned.CreateSubnetTx:
		return de.executeCreateSubnet(vs, utx, txID, signedBytes, creds)
	case *unsigned.ExportTx:
		return de.executeExport(vs, utx, txID, creds)
	case *unsigned.ImportTx:
		return de.executeImport(vs, utx, txID, creds)
	default:
		return nil, fmt.Errorf("expected decision tx but got %T", utx)
	}
}

func (de *decisionExecutor) executeCreateChain(
	vs state.Versioned,
	utx *unsigned.CreateChainTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (func() error, error) {
	// Make sure this transaction is well formed.
	if len(creds) == 0 {
		return nil, unsigned.ErrWrongNumberOfCredentials
	}

	if err := utx.SyntacticVerify(de.ctx); err != nil {
		return nil, err
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(creds) - 1
	baseTxCreds := creds[:baseTxCredsLen]
	subnetCred := creds[baseTxCredsLen]

	// Verify the flowcheck
	createBlockchainTxFee := builder.GetCreateBlockchainTxFee(*de.cfg, vs.GetTimestamp())
	if err := de.spendHandler.SemanticVerifySpend(
		vs,
		utx,
		utx.Ins,
		utx.Outs,
		baseTxCreds,
		createBlockchainTxFee,
		de.ctx.AVAXAssetID,
	); err != nil {
		return nil, err
	}

	subnetIntf, _, err := vs.GetTx(utx.SubnetID)
	if err == database.ErrNotFound {
		return nil, fmt.Errorf("%s isn't a known subnet", utx.SubnetID)
	}
	if err != nil {
		return nil, err
	}

	subnet, ok := subnetIntf.Unsigned.(*unsigned.CreateSubnetTx)
	if !ok {
		return nil, fmt.Errorf("%s isn't a subnet", utx.SubnetID)
	}

	// Verify that this chain is authorized by the subnet
	if err := de.fx.VerifyPermission(utx, utx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return nil, err
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, de.ctx.AVAXAssetID, utx.Outs)

	// Attempt to the new chain to the database
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	vs.AddChain(stx)

	// If this proposal is committed and this node is a member of the
	// subnet that validates the blockchain, create the blockchain
	onAccept := func() error { return p_utils.CreateChain(*de.cfg, utx, txID) }
	return onAccept, nil
}

func (de *decisionExecutor) executeCreateSubnet(
	vs state.Versioned,
	utx *unsigned.CreateSubnetTx,
	txID ids.ID,
	signedBytes []byte,
	creds []verify.Verifiable,
) (func() error, error) {
	// Make sure this transaction is well formed.
	if err := utx.SyntacticVerify(de.ctx); err != nil {
		return nil, err
	}

	// Verify the flowcheck
	createSubnetTxFee := builder.GetCreateSubnetTxFee(*de.cfg, vs.GetTimestamp())
	if err := de.spendHandler.SemanticVerifySpend(
		vs,
		utx,
		utx.Ins,
		utx.Outs,
		creds,
		createSubnetTxFee,
		de.ctx.AVAXAssetID,
	); err != nil {
		return nil, err
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, de.ctx.AVAXAssetID, utx.Outs)

	// Attempt to the new chain to the database
	stx := &signed.Tx{
		Unsigned: utx,
		Creds:    creds,
	}
	stx.Initialize(utx.UnsignedBytes(), signedBytes)
	vs.AddSubnet(stx)

	return nil, nil
}

func (de *decisionExecutor) executeExport(
	vs state.Versioned,
	utx *unsigned.ExportTx,
	txID ids.ID,
	creds []verify.Verifiable,
) (func() error, error) {
	if err := utx.SyntacticVerify(de.ctx); err != nil {
		return nil, err
	}

	outs := make([]*avax.TransferableOutput, len(utx.Outs)+len(utx.ExportedOutputs))
	copy(outs, utx.Outs)
	copy(outs[len(utx.Outs):], utx.ExportedOutputs)

	if de.bootstrapped.GetValue() {
		if err := verify.SameSubnet(de.ctx, utx.DestinationChain); err != nil {
			return nil, err
		}
	}

	// Verify the flowcheck
	if err := de.spendHandler.SemanticVerifySpend(
		vs,
		utx,
		utx.Ins,
		outs,
		creds,
		de.cfg.TxFee,
		de.ctx.AVAXAssetID,
	); err != nil {
		return nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, de.ctx.AVAXAssetID, utx.Outs)
	return nil, nil
}

func (de *decisionExecutor) executeImport(
	vs state.Versioned,
	utx *unsigned.ImportTx,
	txID ids.ID,
	creds []verify.Verifiable,
) (func() error, error) {
	if err := utx.SyntacticVerify(de.ctx); err != nil {
		return nil, err
	}

	utxosList := make([]*avax.UTXO, len(utx.Ins)+len(utx.ImportedInputs))
	for index, input := range utx.Ins {
		utxo, err := vs.GetUTXO(input.InputID())
		if err != nil {
			return nil, fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err)
		}
		utxosList[index] = utxo
	}

	if de.bootstrapped.GetValue() {
		if err := verify.SameSubnet(de.ctx, utx.SourceChain); err != nil {
			return nil, err
		}

		utxoIDs := make([][]byte, len(utx.ImportedInputs))
		for i, in := range utx.ImportedInputs {
			utxoID := in.UTXOID.InputID()
			utxoIDs[i] = utxoID[:]
		}
		allUTXOBytes, err := de.ctx.SharedMemory.Get(utx.SourceChain, utxoIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get shared memory: %w", err)
		}

		for i, utxoBytes := range allUTXOBytes {
			utxo := &avax.UTXO{}
			if _, err := unsigned.Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return nil, fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxosList[i+len(utx.Ins)] = utxo
		}

		ins := make([]*avax.TransferableInput, len(utx.Ins)+len(utx.ImportedInputs))
		copy(ins, utx.Ins)
		copy(ins[len(utx.Ins):], utx.ImportedInputs)

		if err := de.spendHandler.SemanticVerifySpendUTXOs(
			utx,
			utxosList,
			ins,
			utx.Outs,
			creds,
			de.cfg.TxFee,
			de.ctx.AVAXAssetID,
		); err != nil {
			return nil, err
		}
	}

	utxos.ConsumeInputs(vs, utx.Ins)
	utxos.ProduceOutputs(vs, txID, de.ctx.AVAXAssetID, utx.Outs)
	return nil, nil
}

func (de *decisionExecutor) semanticVerifyDecision(
	stx *signed.Tx,
	parentState state.Mutable,
) error {
	switch utx := stx.Unsigned.(type) {
	case *unsigned.CreateChainTx,
		*unsigned.CreateSubnetTx:
		vs := state.NewVersioned(
			parentState,
			parentState.CurrentStakerChainState(),
			parentState.PendingStakerChainState(),
		)
		_, err := de.ExecuteDecision(stx, vs)
		return err

	default:
		return fmt.Errorf("tx type %T could not be semantically verified", utx)
	}
}
