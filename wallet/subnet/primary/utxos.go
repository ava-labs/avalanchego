// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/wallet/chain/p"
	"github.com/ava-labs/avalanchego/wallet/chain/x"
)

var (
	_ UTXOs      = &utxos{}
	_ ChainUTXOs = &chainUTXOs{}

	// TODO: refactor ChainUTXOs definition to allow the client implementations
	//       to perform their own assertions.
	_ ChainUTXOs = p.ChainUTXOs(nil)
	_ ChainUTXOs = x.ChainUTXOs(nil)
)

type UTXOs interface {
	AddUTXO(ctx context.Context, sourceChainID, destinationChainID ids.ID, utxo *avax.UTXO) error
	RemoveUTXO(ctx context.Context, sourceChainID, destinationChainID, utxoID ids.ID) error

	UTXOs(ctx context.Context, sourceChainID, destinationChainID ids.ID) ([]*avax.UTXO, error)
	GetUTXO(ctx context.Context, sourceChainID, destinationChainID, utxoID ids.ID) (*avax.UTXO, error)
}

type ChainUTXOs interface {
	AddUTXO(ctx context.Context, destinationChainID ids.ID, utxo *avax.UTXO) error
	RemoveUTXO(ctx context.Context, sourceChainID, utxoID ids.ID) error

	UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
	GetUTXO(ctx context.Context, sourceChainID, utxoID ids.ID) (*avax.UTXO, error)
}

func NewUTXOs() UTXOs {
	return &utxos{
		sourceToDestToUTXOIDToUTXO: make(map[ids.ID]map[ids.ID]map[ids.ID]*avax.UTXO),
	}
}

func NewChainUTXOs(chainID ids.ID, utxos UTXOs) ChainUTXOs {
	return &chainUTXOs{
		utxos:   utxos,
		chainID: chainID,
	}
}

type utxos struct {
	lock sync.RWMutex
	// sourceChainID -> destinationChainID -> utxoID -> utxo
	sourceToDestToUTXOIDToUTXO map[ids.ID]map[ids.ID]map[ids.ID]*avax.UTXO
}

func (u *utxos) AddUTXO(_ context.Context, sourceChainID, destinationChainID ids.ID, utxo *avax.UTXO) error {
	u.lock.Lock()
	defer u.lock.Unlock()

	destToUTXOIDToUTXO, ok := u.sourceToDestToUTXOIDToUTXO[sourceChainID]
	if !ok {
		destToUTXOIDToUTXO = make(map[ids.ID]map[ids.ID]*avax.UTXO)
		u.sourceToDestToUTXOIDToUTXO[sourceChainID] = destToUTXOIDToUTXO
	}

	utxoIDToUTXO, ok := destToUTXOIDToUTXO[destinationChainID]
	if !ok {
		utxoIDToUTXO = make(map[ids.ID]*avax.UTXO)
		destToUTXOIDToUTXO[destinationChainID] = utxoIDToUTXO
	}

	utxoIDToUTXO[utxo.InputID()] = utxo
	return nil
}

func (u *utxos) RemoveUTXO(_ context.Context, sourceChainID, destinationChainID, utxoID ids.ID) error {
	u.lock.Lock()
	defer u.lock.Unlock()

	destToUTXOIDToUTXO := u.sourceToDestToUTXOIDToUTXO[sourceChainID]
	utxoIDToUTXO := destToUTXOIDToUTXO[destinationChainID]
	_, ok := utxoIDToUTXO[utxoID]
	if !ok {
		return nil
	}

	delete(utxoIDToUTXO, utxoID)
	if len(utxoIDToUTXO) != 0 {
		return nil
	}

	delete(destToUTXOIDToUTXO, destinationChainID)
	if len(destToUTXOIDToUTXO) != 0 {
		return nil
	}

	delete(u.sourceToDestToUTXOIDToUTXO, sourceChainID)
	return nil
}

func (u *utxos) UTXOs(_ context.Context, sourceChainID, destinationChainID ids.ID) ([]*avax.UTXO, error) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	destToUTXOIDToUTXO := u.sourceToDestToUTXOIDToUTXO[sourceChainID]
	utxoIDToUTXO := destToUTXOIDToUTXO[destinationChainID]
	utxos := make([]*avax.UTXO, 0, len(utxoIDToUTXO))
	for _, utxo := range utxoIDToUTXO {
		utxos = append(utxos, utxo)
	}
	return utxos, nil
}

func (u *utxos) GetUTXO(_ context.Context, sourceChainID, destinationChainID, utxoID ids.ID) (*avax.UTXO, error) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	destToUTXOIDToUTXO := u.sourceToDestToUTXOIDToUTXO[sourceChainID]
	utxoIDToUTXO := destToUTXOIDToUTXO[destinationChainID]
	utxo, ok := utxoIDToUTXO[utxoID]
	if !ok {
		return nil, database.ErrNotFound
	}
	return utxo, nil
}

type chainUTXOs struct {
	utxos   UTXOs
	chainID ids.ID
}

func (c *chainUTXOs) AddUTXO(ctx context.Context, destinationChainID ids.ID, utxo *avax.UTXO) error {
	return c.utxos.AddUTXO(ctx, c.chainID, destinationChainID, utxo)
}

func (c *chainUTXOs) RemoveUTXO(ctx context.Context, sourceChainID, utxoID ids.ID) error {
	return c.utxos.RemoveUTXO(ctx, sourceChainID, c.chainID, utxoID)
}

func (c *chainUTXOs) UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error) {
	return c.utxos.UTXOs(ctx, sourceChainID, c.chainID)
}

func (c *chainUTXOs) GetUTXO(ctx context.Context, sourceChainID, utxoID ids.ID) (*avax.UTXO, error) {
	return c.utxos.GetUTXO(ctx, sourceChainID, c.chainID, utxoID)
}
