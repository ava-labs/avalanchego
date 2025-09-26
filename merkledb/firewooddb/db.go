// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewooddb

import (
	"github.com/ava-labs/firewood/ffi"
	"github.com/ava-labs/avalanchego/merkledb"
)

var (
	_ merkledb.Proposal = (*Proposal)(nil)
	_ merkledb.Database = (*DB)(nil)
)

type Proposal struct {
	p *ffi.Proposal
}

func (p *Proposal) Get(key []byte) ([]byte, error) {
	val, err := p.p.Get(key)

	if val == nil && err == nil {
		return nil, merkledb.ErrNotFound
	}

	return val, err
}

func (p *Proposal) Propose(cs merkledb.Changes) (merkledb.Proposal, error) {
	proposal, err := p.p.Propose(cs.Keys, cs.Vals)
	if err != nil {
		return nil, err
	}

	return &Proposal{p: proposal}, nil
}

func (p *Proposal) Commit() error {
	return p.p.Commit()
}

type DB struct {
	db *ffi.Database
}

func New(path string) (*DB, error) {
	db, err := ffi.New(path, ffi.DefaultConfig())
	if err != nil {
		return nil, err
	}

	return &DB{db: db}, nil
}

func (db *DB) Get(key []byte) ([]byte, error) {
	val, err := db.db.Get(key)

	if val == nil && err == nil {
		return nil, merkledb.ErrNotFound
	}

	return val, err
}

func (db *DB) Propose(cs merkledb.Changes) (merkledb.Proposal, error) {
	proposal, err := db.db.Propose(cs.Keys, cs.Vals)
	if err != nil {
		return nil, err
	}

	return &Proposal{p: proposal}, nil
}

// No-op
func (db *DB) Commit() error {
	return db.db.Commit(nil)
}

func (db *DB) Close() error {
	return db.db.Close()
}
