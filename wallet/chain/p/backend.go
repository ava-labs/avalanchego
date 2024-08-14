// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Backend = (*backend)(nil)

// Backend defines the full interface required to support a P-chain wallet.
type Backend interface {
	builder.Backend
	signer.Backend

	AcceptTx(ctx context.Context, tx *txs.Tx) error
}

type backend struct {
	common.ChainUTXOs

	context *builder.Context

	subnetOwnerLock sync.RWMutex
	subnetOwner     map[ids.ID]fx.Owner // subnetID -> owner
}

func NewBackend(context *builder.Context, utxos common.ChainUTXOs, subnetOwner map[ids.ID]fx.Owner) Backend {
	return &backend{
		ChainUTXOs:  utxos,
		context:     context,
		subnetOwner: subnetOwner,
	}
}

func (b *backend) AcceptTx(ctx context.Context, tx *txs.Tx) error {
	txID := tx.ID()
	err := tx.Unsigned.Visit(&backendVisitor{
		b:    b,
		ctx:  ctx,
		txID: txID,
	})
	if err != nil {
		return err
	}

	producedUTXOSlice := tx.UTXOs()
	return b.addUTXOs(ctx, constants.PlatformChainID, producedUTXOSlice)
}

func (b *backend) addUTXOs(ctx context.Context, destinationChainID ids.ID, utxos []*avax.UTXO) error {
	for _, utxo := range utxos {
		if err := b.AddUTXO(ctx, destinationChainID, utxo); err != nil {
			return err
		}
	}
	return nil
}

func (b *backend) removeUTXOs(ctx context.Context, sourceChain ids.ID, utxoIDs set.Set[ids.ID]) error {
	for utxoID := range utxoIDs {
		if err := b.RemoveUTXO(ctx, sourceChain, utxoID); err != nil {
			return err
		}
	}
	return nil
}

func (b *backend) GetSubnetOwner(_ context.Context, subnetID ids.ID) (fx.Owner, error) {
	b.subnetOwnerLock.RLock()
	defer b.subnetOwnerLock.RUnlock()

	owner, exists := b.subnetOwner[subnetID]
	if !exists {
		return nil, database.ErrNotFound
	}
	return owner, nil
}

func (b *backend) setSubnetOwner(subnetID ids.ID, owner fx.Owner) {
	b.subnetOwnerLock.Lock()
	defer b.subnetOwnerLock.Unlock()

	b.subnetOwner[subnetID] = owner
}
