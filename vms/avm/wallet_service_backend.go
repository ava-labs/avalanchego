// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/builder"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ builder.TxBuilderBackend = (*walletServiceBackend)(nil)

func NewWalletServiceBackend(vm *VM) *walletServiceBackend {
	return &walletServiceBackend{
		vm:         vm,
		pendingTxs: linked.NewHashmap[ids.ID, *txs.Tx](),
		utxos:      make([]*avax.UTXO, 0),
	}
}

type walletServiceBackend struct {
	vm         *VM
	pendingTxs *linked.Hashmap[ids.ID, *txs.Tx]
	utxos      []*avax.UTXO

	addrs set.Set[ids.ShortID]
}

func (b *walletServiceBackend) ResetAddresses(addrs set.Set[ids.ShortID]) {
	b.addrs = addrs
}

func (b *walletServiceBackend) UTXOs(_ context.Context, _ ids.ID) ([]*avax.UTXO, error) {
	res, err := avax.GetAllUTXOs(b.vm.state, b.addrs)
	if err != nil {
		return nil, err
	}
	res = append(res, b.utxos...)
	return res, nil
}

func (b *walletServiceBackend) GetUTXO(_ context.Context, _, utxoID ids.ID) (*avax.UTXO, error) {
	allUTXOs, err := avax.GetAllUTXOs(b.vm.state, b.addrs)
	if err != nil {
		return nil, err
	}
	allUTXOs = append(allUTXOs, b.utxos...)

	for _, utxo := range allUTXOs {
		if utxo.InputID() == utxoID {
			return utxo, nil
		}
	}
	return nil, database.ErrNotFound
}

func (b *walletServiceBackend) update(utxos []*avax.UTXO) error {
	utxoMap := make(map[ids.ID]*avax.UTXO, len(utxos))
	for _, utxo := range utxos {
		utxoMap[utxo.InputID()] = utxo
	}

	iter := b.pendingTxs.NewIterator()

	for iter.Next() {
		tx := iter.Value()
		for _, inputUTXO := range tx.Unsigned.InputUTXOs() {
			if inputUTXO.Symbolic() {
				continue
			}
			utxoID := inputUTXO.InputID()
			if _, exists := utxoMap[utxoID]; !exists {
				return errMissingUTXO
			}
			delete(utxoMap, utxoID)
		}

		for _, utxo := range tx.UTXOs() {
			utxoMap[utxo.InputID()] = utxo
		}
	}

	b.utxos = maps.Values(utxoMap)
	return nil
}
