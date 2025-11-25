// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/firewood"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	_ avax.UTXODB = (*firewoodUTXODB)(nil)
	_ ChainDB     = (*firewoodDB)(nil)

	utxoPrefix     = []byte("utxo")
	atomicTxPrefix = []byte("atomic_tx")
)

// firewoodUTXODB is a prefix of firewoodDB that is used for utxo
// management
type firewoodUTXODB struct {
	db *firewood.DB
}

func (f *firewoodUTXODB) Get(key []byte) ([]byte, error) {
	return f.db.Get(firewood.Prefix(utxoPrefix, key))
}

func (f *firewoodUTXODB) Put(key []byte, value []byte) error {
	f.db.Put(firewood.Prefix(utxoPrefix, key), value)
	return nil
}

func (f *firewoodUTXODB) Delete(key []byte) error {
	f.db.Delete(firewood.Prefix(utxoPrefix, key))
	return nil
}

// InitChecksum is a no-op because firewood already initializes the merkle root.
func (*firewoodUTXODB) InitChecksum() error {
	return nil
}

// UpdateChecksum is a no-op because firewood already updates the merkle root.
func (*firewoodUTXODB) UpdateChecksum(ids.ID) {}

func (f *firewoodUTXODB) Checksum() (ids.ID, error) {
	return f.db.Root()
}

// Close is a no-op because this is a subset of firewoodDB which performs
// Close.
func (*firewoodUTXODB) Close() error {
	return nil
}

type ChainDB interface {
	AddAtomicTx(txID ids.ID)
	Repair(vm VM, s State) error
	Abort()
	Close(ctx context.Context) error
}

type firewoodDB struct {
	db *firewood.DB
}

func (f *firewoodDB) AddAtomicTx(txID ids.ID) {
	f.db.Put(firewood.Prefix(atomicTxPrefix, txID[:]), []byte{})
}

func (f *firewoodDB) Repair(vm VM, s State) error {
	lastAcceptedBlk, err := s.GetBlock(s.GetLastAccepted())
	if err != nil {
		return fmt.Errorf("getting last accepted block: %w", err)
	}

	replayStartHeight := 0

	firewoodHeight, ok := f.db.Height()
	if !ok {
		replayStartHeight = -1
	} else {
		replayStartHeight = int(firewoodHeight)
	}

	// Replay any blocks until the last accepted height to synchronize the chain
	// and local dbs.
	for i := replayStartHeight; i < int(lastAcceptedBlk.Height()); i++ {
		blkID, err := s.GetBlockIDAtHeight(uint64(i + 1))
		if err != nil {
			return fmt.Errorf("getting block id: %w", err)
		}

		blk, err := s.GetBlock(blkID)
		if err != nil {
			return fmt.Errorf("getting block: %w", err)
		}

		if err := vm.Replay(blk); err != nil {
			return fmt.Errorf("replaying block: %w", err)
		}
	}

	return s.Commit()
}

func (f *firewoodDB) Abort() {
	f.db.Abort()
}

func (f *firewoodDB) Close(ctx context.Context) error {
	return f.db.Close(ctx)
}
