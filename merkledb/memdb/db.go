// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memdb

import "github.com/ava-labs/avalanchego/merkledb"

var(
	_ view = (*DB)(nil)
	_ view = (*Proposal)(nil)

	_ merkledb.Proposal = (*Proposal)(nil)
	_ merkledb.Database = (*DB)(nil)
)

type view interface {
	read(k []byte, v []byte)
	write(k []byte, v []byte)
}
type Proposal struct {
	view view
	cs   merkledb.Changes
}

func (m Proposal) read(k []byte, v []byte) {
	//TODO implement me
	panic("implement me")
}

func (m Proposal) Get(key []byte) ([]byte, error) {
	return nil, nil
}

func (m Proposal) Propose(cs merkledb.Changes) (merkledb.Proposal, error) {
	return Proposal{cs: cs, view: m}, nil
}

func (m Proposal) Commit() error {
	for k, v := range m.cs.Seq2() {
		m.view.write(k, v)
	}

	return nil
}

func (m Proposal) write(k []byte, v []byte) {
	m.cs.Append(k, v)
}

type DB struct {
	db map[string][]byte
}

func (db *DB) read(k []byte, v []byte) {
	//TODO implement me
	panic("implement me")
}

func (db *DB) Get(key []byte) ([]byte, error) {
	v, ok := db.db[string(key)]
	if !ok {
		return nil, merkledb.ErrNotFound
	}

	return v, nil
}

func (db *DB) Propose(cs merkledb.Changes) (merkledb.Proposal, error) {
	return &Proposal{cs: cs, view: db}, nil
}

func (db *DB) Commit() error {
	return nil
}

func (db *DB) Close() error {
	//TODO actually close
	db.db = nil

	return nil
}

func (db *DB) write(k []byte, v []byte) {
	db.db[string(k)] = v
}
