// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var _ DecisionTx = &CreateChainTx{}

const (
	MaxNameLen    = 128
	MaxGenesisLen = units.MiB
)

type CreateChainTx struct {
	*unsigned.CreateChainTx
}

func (tx *CreateChainTx) InputUTXOs() ids.Set { return nil }

func (tx *CreateChainTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	return ids.ID{}, nil, nil
}

// Attempts to verify this transaction with the provided state.
func (tx *CreateChainTx) SemanticVerify(
	verifier TxVerifier,
	parentState state.Mutable,
	creds []verify.Verifiable,
) error {
	vs := state.NewVersioned(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)
	_, err := tx.Execute(verifier, vs, creds)
	return err
}

// Execute this transaction.
func (tx *CreateChainTx) Execute(
	verifier TxVerifier,
	vs state.Versioned,
	creds []verify.Verifiable,
) (
	func() error,
	error,
) {
	var (
		ctx = verifier.Ctx()
		cfg = *verifier.PlatformConfig()
	)

	// Make sure this transaction is well formed.
	if len(creds) == 0 {
		return nil, unsigned.ErrWrongNumberOfCredentials
	}

	if err := tx.SyntacticVerify(verifier.Ctx()); err != nil {
		return nil, err
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(creds) - 1
	baseTxCreds := creds[:baseTxCredsLen]
	subnetCred := creds[baseTxCredsLen]

	// Verify the flowcheck
	createBlockchainTxFee := builder.GetCreateBlockchainTxFee(cfg, vs.GetTimestamp())
	if err := verifier.SemanticVerifySpend(
		vs,
		tx,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		createBlockchainTxFee,
		ctx.AVAXAssetID,
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
	if err := verifier.FeatureExtension().VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return nil, err
	}

	// Consume the UTXOS
	utxos.ConsumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	utxos.ProduceOutputs(vs, txID, ctx.AVAXAssetID, tx.Outs)
	// Attempt to the new chain to the database
	stx := &signed.Tx{
		Unsigned: tx.CreateChainTx,
		Creds:    creds,
	}
	vs.AddChain(stx)

	// If this proposal is committed and this node is a member of the
	// subnet that validates the blockchain, create the blockchain
	onAccept := func() error { return verifier.CreateChain(tx.CreateChainTx) }
	return onAccept, nil
}
