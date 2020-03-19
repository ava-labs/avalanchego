// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"

	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/vms/components/codec"
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

	NetworkID() uint32
	ChainID() ids.ID
	Outputs() []*TransferableOutput
	Inputs() []*TransferableInput

	AssetIDs() ids.Set
	InputUTXOs() []*UTXOID
	UTXOs() []*UTXO
	SyntacticVerify(ctx *snow.Context, c codec.Codec, numFxs int) error
	SemanticVerify(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error
}

// Tx is the core operation that can be performed. The tx uses the UTXO model.
// Specifically, a txs inputs will consume previous txs outputs. A tx will be
// valid if the inputs have the authority to consume the outputs they are
// attempting to consume and the inputs consume sufficient state to produce the
// outputs.
type Tx struct {
	UnsignedTx `serialize:"true"`

	Creds []verify.Verifiable `serialize:"true"` // The credentials of this transaction
}

// Credentials describes the authorization that allows the Inputs to consume the
// specified UTXOs. The returned array should not be modified.
func (t *Tx) Credentials() []verify.Verifiable { return t.Creds }

// SyntacticVerify verifies that this transaction is well-formed.
func (t *Tx) SyntacticVerify(ctx *snow.Context, c codec.Codec, numFxs int) error {
	switch {
	case t == nil || t.UnsignedTx == nil:
		return errNilTx
	}

	if err := t.UnsignedTx.SyntacticVerify(ctx, c, numFxs); err != nil {
		return err
	}

	for _, cred := range t.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numInputs := len(t.InputUTXOs())
	if numInputs != len(t.Creds) {
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
