// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/merkledb/firewooddb"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/merkledb"
	"fmt"
)

var _ avax.UTXODB = (*FirewoodUTXODB)(nil)

// TODO rename for H upgrade
type FirewoodUTXODB struct {
	pending merkledb.Changes

	// TODO handle prefix
	db *firewooddb.DB
}

func (f *FirewoodUTXODB) Get(key []byte) ([]byte, error) {
	val, err := f.db.Get(key)
	if val == nil && err == nil {
		return nil, database.ErrNotFound
	}

	return val, nil
}

func (f *FirewoodUTXODB) Put(key []byte, value []byte) error {
	f.pending.Append(key, value)
	return nil
}

func (f *FirewoodUTXODB) Delete(key []byte) error {
	f.pending.Append(key, nil)
	return nil
}

// Checksums are updated by the underlying db
func (f *FirewoodUTXODB) InitChecksum() error {
	return nil
}

// Checksums are updated by the underlying db
func (f *FirewoodUTXODB) UpdateChecksum(utxoID ids.ID) {
	return
}

// TODO this updates per-block instead of per change - but this should be good
// enough
func (f *FirewoodUTXODB) Checksum() (ids.ID, error) {
	return f.db.Root()
}

func (f *FirewoodUTXODB) Flush() error {
	p, err := f.db.Propose(f.pending)
	if err != nil {
		return fmt.Errorf("proposing: %w", err)
	}

	if err := p.Commit(); err != nil {
		return fmt.Errorf("committing: %w", err)
	}

	f.pending = merkledb.Changes{}
	return nil
}

func (f *FirewoodUTXODB) Close() error {
	return f.db.Close()
}
