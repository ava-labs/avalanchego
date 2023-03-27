// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"fmt"

	stdcontext "context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ Backend = (*backend)(nil)

type ChainUTXOs interface {
	AddUTXO(ctx stdcontext.Context, destinationChainID ids.ID, utxo *avax.UTXO) error
	RemoveUTXO(ctx stdcontext.Context, sourceChainID, utxoID ids.ID) error

	UTXOs(ctx stdcontext.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
	GetUTXO(ctx stdcontext.Context, sourceChainID, utxoID ids.ID) (*avax.UTXO, error)
}

// Backend defines the full interface required to support an X-chain wallet.
type Backend interface {
	ChainUTXOs
	BuilderBackend
	SignerBackend

	AcceptTx(ctx stdcontext.Context, tx *txs.Tx) error
}

type backend struct {
	Context
	ChainUTXOs

	chainID ids.ID
}

func NewBackend(ctx Context, chainID ids.ID, utxos ChainUTXOs) Backend {
	return &backend{
		Context:    ctx,
		ChainUTXOs: utxos,

		chainID: chainID,
	}
}

// TODO: implement txs.Visitor here
func (b *backend) AcceptTx(ctx stdcontext.Context, tx *txs.Tx) error {
	switch utx := tx.Unsigned.(type) {
	case *txs.BaseTx, *txs.CreateAssetTx, *txs.OperationTx:
	case *txs.ImportTx:
		for _, input := range utx.ImportedIns {
			utxoID := input.UTXOID.InputID()
			if err := b.RemoveUTXO(ctx, utx.SourceChain, utxoID); err != nil {
				return err
			}
		}
	case *txs.ExportTx:
		txID := tx.ID()
		for i, out := range utx.ExportedOuts {
			err := b.AddUTXO(
				ctx,
				utx.DestinationChain,
				&avax.UTXO{
					UTXOID: avax.UTXOID{
						TxID:        txID,
						OutputIndex: uint32(len(utx.Outs) + i),
					},
					Asset: avax.Asset{ID: out.AssetID()},
					Out:   out.Out,
				},
			)
			if err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%w: %T", errUnknownTxType, tx.Unsigned)
	}

	inputUTXOs := tx.Unsigned.InputUTXOs()
	for _, utxoID := range inputUTXOs {
		if utxoID.Symbol {
			continue
		}
		if err := b.RemoveUTXO(ctx, b.chainID, utxoID.InputID()); err != nil {
			return err
		}
	}

	outputUTXOs := tx.UTXOs()
	for _, utxo := range outputUTXOs {
		if err := b.AddUTXO(ctx, b.chainID, utxo); err != nil {
			return err
		}
	}
	return nil
}
