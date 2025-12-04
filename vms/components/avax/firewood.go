// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/firewood"
	"github.com/ava-labs/avalanchego/ids"
)

var utxoPrefix = []byte("utxo")

type FirewoodUTXODB struct {
	db    *firewood.DB
	codec codec.Manager
}

var _ UTXODB = (*FirewoodUTXODB)(nil)

func NewFirewoodUTXODB(db *firewood.DB, codec codec.Manager) *FirewoodUTXODB {
	return &FirewoodUTXODB{
		db:    db,
		codec: codec,
	}
}

func (f *FirewoodUTXODB) Get(inputID ids.ID) (*UTXO, error) {
	bytes, err := f.db.Get(firewood.Prefix(utxoPrefix, inputID[:]))
	if err != nil {
		return nil, err
	}

	u := &UTXO{}
	if _, err := f.codec.Unmarshal(bytes, u); err != nil {
		return nil, err
	}

	return u, nil
}

func (f *FirewoodUTXODB) Put(u *UTXO) error {
	bytes, err := f.codec.Marshal(codecVersion, u)
	if err != nil {
		return err
	}

	inputID := u.InputID()
	f.db.Put(firewood.Prefix(utxoPrefix, inputID[:]), bytes)
	return nil
}

func (f *FirewoodUTXODB) Delete(key []byte) error {
	f.db.Delete(firewood.Prefix(utxoPrefix, key))
	return nil
}

// InitChecksum is a no-op because firewood already initializes the merkle root.
func (*FirewoodUTXODB) InitChecksum() error {
	return nil
}

// UpdateChecksum is a no-op because firewood already updates the merkle root.
func (*FirewoodUTXODB) UpdateChecksum(ids.ID) {}

func (f *FirewoodUTXODB) Checksum() (ids.ID, error) {
	return f.db.Root()
}

// Close is a no-op because this is a subset of firewoodDB which performs
// Close.
func (*FirewoodUTXODB) Close() error {
	return nil
}
