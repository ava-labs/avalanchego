// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	errMissingTxID          = errors.New("missing tx id")
	errMissingStopSignature = errors.New("missing stop signature")
)

type StopContinuousValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// ID of the tx that created the continuous validator.
	TxID ids.ID `serialize:"true" json:"txID"`

	// Authorizes this validator to be stopped.
	// It is a BLS Proof of Possession signature using validator key of the TxID.

	StopSignature [bls.SignatureLen]byte `serialize:"true" json:"stopSignature"`
}

func (tx *StopContinuousValidatorTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.TxID == ids.Empty:
		return errMissingTxID
	case bytes.Equal(tx.StopSignature[:], make([]byte, bls.SignatureLen)):
		return errMissingStopSignature
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *StopContinuousValidatorTx) Visit(visitor Visitor) error {
	return visitor.StopContinuousValidatorTx(tx)
}
