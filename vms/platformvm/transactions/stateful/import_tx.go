// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ AtomicTx = &ImportTx{}

type ImportTx struct {
	*unsigned.ImportTx

	txID  ids.ID // ID of signed import tx
	creds []verify.Verifiable

	verifier TxVerifier
}

// AtomicOperations returns the shared memory requests
func (tx *ImportTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.InputID()
		utxoIDs[i] = utxoID[:]
	}
	return tx.SourceChain, &atomic.Requests{RemoveRequests: utxoIDs}, nil
}

// Accept this transaction and spend imported inputs
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (tx *ImportTx) AtomicAccept(ctx *snow.Context, batch database.Batch) error {
	chainID, requests, err := tx.AtomicOperations()
	if err != nil {
		return err
	}
	return ctx.SharedMemory.Apply(map[ids.ID]*atomic.Requests{chainID: requests}, batch)
}
