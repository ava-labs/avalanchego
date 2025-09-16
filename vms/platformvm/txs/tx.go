// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ gossip.Gossipable = (*Tx)(nil)

	ErrNilSignedTx = errors.New("nil signed tx is not valid")

	errSignedTxNotInitialized = errors.New("signed tx was never initialized and is not valid")
)

// Tx is a signed transaction
type Tx struct {
	// The body of this transaction
	Unsigned UnsignedTx `serialize:"true" json:"unsignedTx"`

	// The credentials of this transaction
	Creds []verify.Verifiable `serialize:"true" json:"credentials"`

	TxID  ids.ID `json:"id"`
	bytes []byte
}

func NewSigned(
	unsigned UnsignedTx,
	c codec.Manager,
	signers [][]*secp256k1.PrivateKey,
) (*Tx, error) {
	res := &Tx{Unsigned: unsigned}
	return res, res.Sign(c, signers)
}

func (tx *Tx) Initialize(c codec.Manager) error {
	signedBytes, err := c.Marshal(CodecVersion, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}

	unsignedBytesLen, err := c.Size(CodecVersion, &tx.Unsigned)
	if err != nil {
		return fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}

	unsignedBytes := signedBytes[:unsignedBytesLen]
	tx.SetBytes(unsignedBytes, signedBytes)
	return nil
}

func (tx *Tx) SetBytes(unsignedBytes, signedBytes []byte) {
	tx.Unsigned.SetBytes(unsignedBytes)
	tx.bytes = signedBytes
	tx.TxID = hashing.ComputeHash256Array(signedBytes)
}

// Parse signed tx starting from its byte representation.
// Note: We explicitly pass the codec in Parse since we may need to parse
// P-Chain genesis txs whose length exceed the max length of txs.Codec.
func Parse(c codec.Manager, signedBytes []byte) (*Tx, error) {
	tx := &Tx{}
	if _, err := c.Unmarshal(signedBytes, tx); err != nil {
		return nil, fmt.Errorf("couldn't parse tx: %w", err)
	}

	unsignedBytesLen, err := c.Size(CodecVersion, &tx.Unsigned)
	if err != nil {
		return nil, fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}

	unsignedBytes := signedBytes[:unsignedBytesLen]
	tx.SetBytes(unsignedBytes, signedBytes)
	return tx, nil
}

func (tx *Tx) Bytes() []byte {
	return tx.bytes
}

func (tx *Tx) Size() int {
	return len(tx.bytes)
}

func (tx *Tx) ID() ids.ID {
	return tx.TxID
}

func (tx *Tx) GossipID() ids.ID {
	return tx.TxID
}

// UTXOs returns the UTXOs transaction is producing.
func (tx *Tx) UTXOs() []*avax.UTXO {
	outs := tx.Unsigned.Outputs()
	utxos := make([]*avax.UTXO, len(outs))
	for i, out := range outs {
		utxos[i] = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        tx.TxID,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}
	}
	return utxos
}

// InputIDs returns the set of inputs this transaction consumes
func (tx *Tx) InputIDs() set.Set[ids.ID] {
	return tx.Unsigned.InputIDs()
}

func (tx *Tx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilSignedTx
	case tx.TxID == ids.Empty:
		return errSignedTxNotInitialized
	default:
		return tx.Unsigned.SyntacticVerify(ctx)
	}
}

// Sign this transaction with the provided signers
// Note: We explicitly pass the codec in Sign since we may need to sign P-Chain
// genesis txs whose length exceed the max length of txs.Codec.
func (tx *Tx) Sign(c codec.Manager, signers [][]*secp256k1.PrivateKey) error {
	unsignedBytes, err := c.Marshal(CodecVersion, &tx.Unsigned)
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedTx: %w", err)
	}

	// Attach credentials
	hash := hashing.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][secp256k1.SignatureLen]byte, len(keys)),
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

	tx.UTXOs()

	signedBytes, err := c.Marshal(CodecVersion, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}
	tx.SetBytes(unsignedBytes, signedBytes)
	return nil
}
