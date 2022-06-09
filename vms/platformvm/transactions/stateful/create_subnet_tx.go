// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var _ DecisionTx = &CreateSubnetTx{}

type CreateSubnetTx struct {
	*unsigned.CreateSubnetTx

	txID        ids.ID // ID of signed create subnet tx
	signedBytes []byte // signed Tx bytes, needed to recreate signed.Tx
	creds       []verify.Verifiable

	verifier TxVerifier
}

// InputUTXOs for [DecisionTxs] will return an empty set to diffrentiate from the [AtomicTxs] input UTXOs
func (tx *CreateSubnetTx) InputUTXOs() ids.Set { return nil }

func (tx *CreateSubnetTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	return ids.ID{}, nil, nil
}

// Execute this transaction.
func (tx *CreateSubnetTx) Execute(vs state.Versioned) (
	func() error,
	error,
) {
	var (
		ctx = tx.verifier.Ctx()
		cfg = *tx.verifier.PlatformConfig()
	)

	// Make sure this transaction is well formed.
	if err := tx.SyntacticVerify(ctx); err != nil {
		return nil, err
	}

	// Verify the flowcheck
	createSubnetTxFee := builder.GetCreateSubnetTxFee(cfg, vs.GetTimestamp())
	if err := tx.verifier.SemanticVerifySpend(
		vs,
		tx,
		tx.Ins,
		tx.Outs,
		tx.creds,
		createSubnetTxFee,
		ctx.AVAXAssetID,
	); err != nil {
		return nil, err
	}

	// Consume the UTXOS
	utxos.ConsumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(vs, tx.txID, ctx.AVAXAssetID, tx.Outs)

	// Attempt to the new chain to the database
	stx := &signed.Tx{
		Unsigned: tx,
		Creds:    tx.creds,
	}
	stx.Initialize(tx.UnsignedBytes(), tx.signedBytes)
	vs.AddSubnet(stx)

	return nil, nil
}
