// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"sync"

	stdcontext "context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Backend = (*backend)(nil)

// Backend defines the full interface required to support a P-chain wallet.
type Backend interface {
	common.ChainUTXOs
	BuilderBackend
	SignerBackend

	AcceptTx(ctx stdcontext.Context, tx *txs.Tx) error
}

type backend struct {
	Context
	common.ChainUTXOs

	txsLock sync.RWMutex
	// txID -> tx
	txs map[ids.ID]*txs.Tx

	subnetOwnerLock sync.RWMutex
	// subnetID -> owner
	subnetOwner map[ids.ID]*secp256k1fx.OutputOwners
}

func NewBackend(ctx Context, utxos common.ChainUTXOs, txs map[ids.ID]*txs.Tx) (Backend, error) {
	subnetOwner, err := getSubnetOwnerMap(txs)
	if err != nil {
		return nil, err
	}
	return &backend{
		Context:     ctx,
		ChainUTXOs:  utxos,
		txs:         txs,
		subnetOwner: subnetOwner,
	}, nil
}

func (b *backend) AcceptTx(ctx stdcontext.Context, tx *txs.Tx) error {
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
	err = b.addUTXOs(ctx, constants.PlatformChainID, producedUTXOSlice)
	if err != nil {
		return err
	}

	b.txsLock.Lock()
	defer b.txsLock.Unlock()

	b.txs[txID] = tx
	return nil
}

func (b *backend) addUTXOs(ctx stdcontext.Context, destinationChainID ids.ID, utxos []*avax.UTXO) error {
	for _, utxo := range utxos {
		if err := b.AddUTXO(ctx, destinationChainID, utxo); err != nil {
			return err
		}
	}
	return nil
}

func (b *backend) removeUTXOs(ctx stdcontext.Context, sourceChain ids.ID, utxoIDs set.Set[ids.ID]) error {
	for utxoID := range utxoIDs {
		if err := b.RemoveUTXO(ctx, sourceChain, utxoID); err != nil {
			return err
		}
	}
	return nil
}

func (b *backend) GetTx(_ stdcontext.Context, txID ids.ID) (*txs.Tx, error) {
	b.txsLock.RLock()
	defer b.txsLock.RUnlock()

	tx, exists := b.txs[txID]
	if !exists {
		return nil, database.ErrNotFound
	}
	return tx, nil
}

func (b *backend) setSubnetOwner(_ stdcontext.Context, subnetID ids.ID, owner *secp256k1fx.OutputOwners) {
	b.subnetOwnerLock.Lock()
	defer b.subnetOwnerLock.Unlock()

	b.subnetOwner[subnetID] = owner
}

func (b *backend) GetSubnetOwner(_ stdcontext.Context, subnetID ids.ID) (*secp256k1fx.OutputOwners, error) {
	b.subnetOwnerLock.RLock()
	defer b.subnetOwnerLock.RUnlock()

	owner, exists := b.subnetOwner[subnetID]
	if !exists {
		return nil, database.ErrNotFound
	}
	return owner, nil
}

func getSubnetOwnerMap(pChainTxs map[ids.ID]*txs.Tx) (map[ids.ID]*secp256k1fx.OutputOwners, error) {
	subnetOwner := map[ids.ID]*secp256k1fx.OutputOwners{}
	// first get owners from original CreateSubnetTx
	for txID, tx := range pChainTxs {
		createSubnetTx, ok := tx.Unsigned.(*txs.CreateSubnetTx)
		if !ok {
			continue
		}
		owner, ok := createSubnetTx.Owner.(*secp256k1fx.OutputOwners)
		if !ok {
			return nil, errUnknownOwnerType
		}
		subnetOwner[txID] = owner
	}
	// then check TransferSubnetOwnershipTx
	for _, tx := range pChainTxs {
		transferSubnetOwnershipTx, ok := tx.Unsigned.(*txs.TransferSubnetOwnershipTx)
		if !ok {
			continue
		}
		owner, ok := transferSubnetOwnershipTx.Owner.(*secp256k1fx.OutputOwners)
		if !ok {
			return nil, errUnknownOwnerType
		}
		subnetOwner[transferSubnetOwnershipTx.Subnet] = owner
	}
	return subnetOwner, nil
}
