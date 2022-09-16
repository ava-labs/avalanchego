// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errNilTx = errors.New("nil tx is not valid")

type UnsignedTx interface {
	snow.ContextInitializable

	Initialize(unsignedBytes []byte)
	Bytes() []byte

	ConsumedAssetIDs() ids.Set
	AssetIDs() ids.Set

	NumCredentials() int
	InputUTXOs() []*avax.UTXOID

	SyntacticVerify(
		ctx *snow.Context,
		c codec.Manager,
		txFeeAssetID ids.ID,
		txFee uint64,
		creationTxFee uint64,
		numFxs int,
	) error
	// Visit calls [visitor] with this transaction's concrete type
	Visit(visitor Visitor) error
}

// Tx is the core operation that can be performed. The tx uses the UTXO model.
// Specifically, a txs inputs will consume previous txs outputs. A tx will be
// valid if the inputs have the authority to consume the outputs they are
// attempting to consume and the inputs consume sufficient state to produce the
// outputs.
type Tx struct {
	Unsigned UnsignedTx          `serialize:"true" json:"unsignedTx"`
	Creds    []*fxs.FxCredential `serialize:"true" json:"credentials"` // The credentials of this transaction

	id    ids.ID
	bytes []byte
}

func (t *Tx) Initialize(unsignedBytes, signedBytes []byte) {
	t.id = hashing.ComputeHash256Array(signedBytes)
	t.bytes = signedBytes
	t.Unsigned.Initialize(unsignedBytes)
}

// ID returns the unique ID of this tx
func (t *Tx) ID() ids.ID { return t.id }

// Bytes returns the binary representation of this tx
func (t *Tx) Bytes() []byte { return t.bytes }

// UTXOs returns the UTXOs transaction is producing.
func (t *Tx) UTXOs() []*avax.UTXO {
	u := utxoGetter{tx: t}
	// The visit error is explicitly dropped here because no error is ever
	// returned from the utxoGetter.
	_ = t.Unsigned.Visit(&u)
	return u.utxos
}

// SyntacticVerify verifies that this transaction is well-formed.
func (t *Tx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Manager,
	txFeeAssetID ids.ID,
	txFee uint64,
	creationTxFee uint64,
	numFxs int,
) error {
	if t == nil || t.Unsigned == nil {
		return errNilTx
	}

	if err := t.Unsigned.SyntacticVerify(ctx, c, txFeeAssetID, txFee, creationTxFee, numFxs); err != nil {
		return err
	}

	for _, cred := range t.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	if numCreds := t.Unsigned.NumCredentials(); numCreds != len(t.Creds) {
		return fmt.Errorf("tx has %d credentials but %d inputs. Should be same",
			len(t.Creds),
			numCreds,
		)
	}
	return nil
}

func (t *Tx) SignSECP256K1Fx(c codec.Manager, signers [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := c.Marshal(CodecVersion, &t.Unsigned)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	hash := hashing.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][crypto.SECP256K1RSigLen]byte, len(keys)),
		}
		for i, key := range keys {
			sig, err := key.SignHash(hash)
			if err != nil {
				return fmt.Errorf("problem creating transaction: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		t.Creds = append(t.Creds, &fxs.FxCredential{Verifiable: cred})
	}

	signedBytes, err := c.Marshal(CodecVersion, t)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	t.Initialize(unsignedBytes, signedBytes)
	return nil
}

func (t *Tx) SignPropertyFx(c codec.Manager, signers [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := c.Marshal(CodecVersion, &t.Unsigned)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	hash := hashing.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &propertyfx.Credential{Credential: secp256k1fx.Credential{
			Sigs: make([][crypto.SECP256K1RSigLen]byte, len(keys)),
		}}
		for i, key := range keys {
			sig, err := key.SignHash(hash)
			if err != nil {
				return fmt.Errorf("problem creating transaction: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		t.Creds = append(t.Creds, &fxs.FxCredential{Verifiable: cred})
	}

	signedBytes, err := c.Marshal(CodecVersion, t)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	t.Initialize(unsignedBytes, signedBytes)
	return nil
}

func (t *Tx) SignNFTFx(c codec.Manager, signers [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := c.Marshal(CodecVersion, &t.Unsigned)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	hash := hashing.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &nftfx.Credential{Credential: secp256k1fx.Credential{
			Sigs: make([][crypto.SECP256K1RSigLen]byte, len(keys)),
		}}
		for i, key := range keys {
			sig, err := key.SignHash(hash)
			if err != nil {
				return fmt.Errorf("problem creating transaction: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		t.Creds = append(t.Creds, &fxs.FxCredential{Verifiable: cred})
	}

	signedBytes, err := c.Marshal(CodecVersion, t)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	t.Initialize(unsignedBytes, signedBytes)
	return nil
}
