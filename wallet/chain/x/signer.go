// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	stdcontext "context"
)

var _ Signer = (*signer)(nil)

type Signer interface {
	// Sign adds as many missing signatures as possible to the provided
	// transaction.
	//
	// If there are already some signatures on the transaction, those signatures
	// will not be removed.
	//
	// If the signer doesn't have the ability to provide a required signature,
	// the signature slot will be skipped without reporting an error.
	Sign(ctx stdcontext.Context, tx *txs.Tx) error
}

type SignerBackend interface {
	GetUTXO(ctx stdcontext.Context, chainID, utxoID ids.ID) (*avax.UTXO, error)
}

type signer struct {
	kc      keychain.Keychain
	backend SignerBackend
}

func NewSigner(kc keychain.Keychain, backend SignerBackend) Signer {
	return &signer{
		kc:      kc,
		backend: backend,
	}
}

func (s *signer) Sign(ctx stdcontext.Context, tx *txs.Tx) error {
	return tx.Unsigned.Visit(&signerVisitor{
		kc:      s.kc,
		backend: s.backend,
		ctx:     ctx,
		tx:      tx,
	})
}

func SignUnsigned(
	ctx stdcontext.Context,
	signer Signer,
	utx txs.UnsignedTx,
) (*txs.Tx, error) {
	tx := &txs.Tx{Unsigned: utx}
	return tx, signer.Sign(ctx, tx)
}
