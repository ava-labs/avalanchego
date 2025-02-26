// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewooddb

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/merkledb"
	"github.com/ava-labs/firewood/ffi"
	"github.com/ava-labs/avalanchego/database"
	"fmt"
)

var (
	_ merkledb.Proposal = (*Proposal)(nil)
	_ database.Batch = (*batch)(nil)

	_ merkledb.Database = (*DB)(nil)
	_ database.Batcher = (*DB)(nil)
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

func (db *DB) Root() (ids.ID, error) {
	root, err := db.db.Root()
	if err != nil {
		return ids.ID{}, err
	}

	return ids.ID(root), nil
}

func (db *DB) Close() error {
	return db.db.Close()
}

func (db *DB) NewBatch() database.Batch {
	return &batch{db: db}
}

type batch struct {
	db      *DB
	changes merkledb.Changes
}

func (b *batch) Put(key []byte, val []byte) error {
	b.changes.Append(key, val)
	return nil
}

func (b *batch) Delete(key []byte) error {
	b.changes.Append(key, nil)
	return nil
}

// TODO this is not used outside of tests/EVM
func (b *batch) Size() int {
	panic("implement me")
}

func (b *batch) Write() error {
	p, err := b.db.Propose(b.changes)
	if err != nil {
		return fmt.Errorf("failed to create proposal: %w", err)
	}

	if err := p.Commit(); err != nil {
		return fmt.Errorf("failed to commit proposal: %w", err)
	}

	return nil
}

func (b *batch) Reset() {
	b.changes = merkledb.Changes{}
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	for k, v := range b.changes.Seq2() {
		if len(v) == 0 {
			if err := w.Delete(k); err != nil {
				return err
			}
			continue
		}

		if err := w.Put(k, v); err != nil {
			return err
		}
	}

	return nil
}

func (b *batch) Inner() database.Batch {
	return b
}
