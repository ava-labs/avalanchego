// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
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
	Sign(ctx context.Context, tx *txs.Tx, options ...common.Option) error
}

type Backend interface {
	GetUTXO(ctx context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error)
}

type signer struct {
	kc      keychain.Keychain
	backend Backend
}

func New(kc keychain.Keychain, backend Backend) Signer {
	return &signer{
		kc:      kc,
		backend: backend,
	}
}

func (s *signer) Sign(ctx context.Context, tx *txs.Tx, options ...common.Option) error {
	ops := common.NewOptions(options)

	return tx.Unsigned.Visit(&visitor{
		kc:            s.kc,
		backend:       s.backend,
		ctx:           ctx,
		tx:            tx,
		forceSignHash: ops.ForceSignHash(),
	})
}

func SignUnsigned(
	ctx context.Context,
	signer Signer,
	utx txs.UnsignedTx,
	options ...common.Option,
) (*txs.Tx, error) {
	tx := &txs.Tx{Unsigned: utx}
	return tx, signer.Sign(ctx, tx, options...)
}
