// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/x/signer"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Backend = (*backend)(nil)

// Backend defines the full interface required to support an X-chain wallet.
type Backend interface {
	common.ChainUTXOs
	builder.Backend
	signer.Backend

	AcceptTx(ctx context.Context, tx *txs.Tx) error
}

type backend struct {
	common.ChainUTXOs

	context *builder.Context
}

func NewBackend(context *builder.Context, utxos common.ChainUTXOs) Backend {
	return &backend{
		ChainUTXOs: utxos,
		context:    context,
	}
}

func (b *backend) AcceptTx(ctx context.Context, tx *txs.Tx) error {
	err := tx.Unsigned.Visit(&backendVisitor{
		b:    b,
		ctx:  ctx,
		txID: tx.ID(),
	})
	if err != nil {
		return err
	}

	chainID := b.context.BlockchainID
	inputUTXOs := tx.Unsigned.InputUTXOs()
	for _, utxoID := range inputUTXOs {
		if utxoID.Symbol {
			continue
		}
		if err := b.RemoveUTXO(ctx, chainID, utxoID.InputID()); err != nil {
			return err
		}
	}

	outputUTXOs := tx.UTXOs()
	for _, utxo := range outputUTXOs {
		if err := b.AddUTXO(ctx, chainID, utxo); err != nil {
			return err
		}
	}
	return nil
}
