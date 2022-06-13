// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signed

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	ErrNilSignedTx           = errors.New("nil signed tx is not valid")
	ErrSignedTxNotInitialize = errors.New("signed tx was never initialized and is not valid")
)

// Tx is a signed transaction
type Tx struct {
	// The body of this transaction
	Unsigned unsigned.Tx `serialize:"true" json:"unsignedTx"`

	// The credentials of this transaction
	Creds []verify.Verifiable `serialize:"true" json:"credentials"`

	id    ids.ID
	bytes []byte
}

func New(unsigned unsigned.Tx, c codec.Manager, signers [][]*crypto.PrivateKeySECP256K1R) (*Tx, error) {
	res := &Tx{Unsigned: unsigned}
	return res, res.Sign(c, signers)
}

// Build signed tx starting from its byte representation
func FromBytes(c codec.Manager, signedBytes []byte) (*Tx, error) {
	res := &Tx{}
	if _, err := c.Unmarshal(signedBytes, res); err != nil {
		return nil, fmt.Errorf("couldn't parse tx: %w", err)
	}
	unsignedBytes, err := c.Marshal(unsigned.Version, &res.Unsigned)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedTx: %w", err)
	}
	res.Initialize(unsignedBytes, signedBytes)
	return res, nil
}

func (tx *Tx) Initialize(unsignedBytes, signedBytes []byte) {
	tx.Unsigned.Initialize(unsignedBytes)

	tx.bytes = signedBytes
	tx.id = hashing.ComputeHash256Array(signedBytes)
}

func (tx *Tx) Bytes() []byte { return tx.bytes }
func (tx *Tx) ID() ids.ID    { return tx.id }

// UTXOs returns the UTXOs transaction is producing.
func (tx *Tx) UTXOs() []*avax.UTXO {
	outs := tx.Unsigned.Outputs()
	utxos := make([]*avax.UTXO, len(outs))
	for i, out := range outs {
		utxos[i] = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        tx.id,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}
	}
	return utxos
}

func (tx *Tx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilSignedTx
	case tx.id == ids.Empty:
		return ErrSignedTxNotInitialize
	default:
		return tx.Unsigned.SyntacticVerify(ctx)
	}
}

// Sign this transaction with the provided signers
func (tx *Tx) Sign(c codec.Manager, signers [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := c.Marshal(unsigned.Version, &tx.Unsigned)
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedTx: %w", err)
	}

	// Attach credentials
	hash := hashing.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][crypto.SECP256K1RSigLen]byte, len(keys)),
		}
		for i, key := range keys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		tx.Creds = append(tx.Creds, cred) // Attach credential
	}

	signedBytes, err := c.Marshal(unsigned.Version, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}
	tx.Initialize(unsignedBytes, signedBytes)
	return nil
}
