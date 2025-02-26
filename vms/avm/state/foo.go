// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/ids"
	"iter"
	"github.com/ava-labs/avalanchego/merkledb/firewooddb"
	"github.com/ava-labs/avalanchego/codec"
	"errors"
	"github.com/ava-labs/avalanchego/merkledb"
	"fmt"
	"github.com/ava-labs/avalanchego/database"
)

var (
	_ avax.UTXODB = (*FooUTXODB)(nil)
)

// TODO rename for H upgrade
type FooUTXODB struct {
	stateDB *firewooddb.DB
}

func (f FooUTXODB) Get(key []byte) ([]byte, error) {
	val, err :=  f.stateDB.Get(key)
	if errors.Is(merkledb.ErrNotFound, err) {
		return nil, fmt.Errorf("%w: %w", err, database.ErrNotFound)
	}

	return val, nil
}

func (f FooUTXODB) Put(key []byte, value []byte) error {
	//TODO implement me
	panic("implement me")
}

func (f FooUTXODB) Delete(key []byte) error {
	//TODO implement me
	panic("implement me")
}

func (f FooUTXODB) UTXOs(
	startingUTXOID ids.ID,
	codec codec.Manager,
) iter.Seq2[*avax.UTXO, error] {
	//TODO implement me
	panic("implement me")
}

func (f FooUTXODB) InitChecksum() error {
	//TODO implement me
	panic("implement me")
}

func (f FooUTXODB) UpdateChecksum(utxoID ids.ID) {
	//TODO implement me
	panic("implement me")
}

func (f FooUTXODB) Checksum() (ids.ID, error) {
	//TODO implement me
	panic("implement me")
}

func (f FooUTXODB) Close() error {
	//TODO implement me
	panic("implement me")
}
