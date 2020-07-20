// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errWrongNumberOfCredentials = errors.New("should have the same number of credentials as inputs")
)

// UnsignedTx ...
type UnsignedTx interface {
	Initialize(bytes []byte)
	ID() ids.ID
	Bytes() []byte

	ConsumedAssetIDs() ids.Set
	AssetIDs() ids.Set

	NumCredentials() int
	InputUTXOs() []*ava.UTXOID
	UTXOs() []*ava.UTXO

	SyntacticVerify(ctx *snow.Context, c codec.Codec, txFeeAssetID ids.ID, txFee uint64, numFxs int) error
	SemanticVerify(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error
	ExecuteWithSideEffects(vm *VM, batch database.Batch) error
}

// Tx is the core operation that can be performed. The tx uses the UTXO model.
// Specifically, a txs inputs will consume previous txs outputs. A tx will be
// valid if the inputs have the authority to consume the outputs they are
// attempting to consume and the inputs consume sufficient state to produce the
// outputs.
type Tx struct {
	UnsignedTx `serialize:"true" json:"unsignedTx"`

	Creds []verify.Verifiable `serialize:"true" json:"credentials"` // The credentials of this transaction
}

// Credentials describes the authorization that allows the Inputs to consume the
// specified UTXOs. The returned array should not be modified.
func (t *Tx) Credentials() []verify.Verifiable { return t.Creds }

// SyntacticVerify verifies that this transaction is well-formed.
func (t *Tx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Codec,
	txFeeAssetID ids.ID,
	txFee uint64,
	numFxs int,
) error {
	switch {
	case t == nil || t.UnsignedTx == nil:
		return errNilTx
	}

	if err := t.UnsignedTx.SyntacticVerify(ctx, c, txFeeAssetID, txFee, numFxs); err != nil {
		return err
	}

	for _, cred := range t.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	if numCreds := t.UnsignedTx.NumCredentials(); numCreds != len(t.Creds) {
		return errWrongNumberOfCredentials
	}
	return nil
}

// SemanticVerify verifies that this transaction is well-formed.
func (t *Tx) SemanticVerify(vm *VM, uTx *UniqueTx) error {
	if t == nil {
		return errNilTx
	}

	return t.UnsignedTx.SemanticVerify(vm, uTx, t.Creds)
}
