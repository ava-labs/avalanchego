// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const PrefixDelimiter = '/'

var (
	consensusPrefix = []byte("consensus")
	heightKey       = []byte("height")
	appPrefix       = []byte("app")
)

type changes struct {
	Keys [][]byte
	Vals [][]byte

	kv map[string][]byte
}

func (c *changes) Put(key []byte, val []byte) {
	c.put(Prefix(appPrefix, key), val, false)
}

func (c *changes) Delete(key []byte) {
	c.put(Prefix(appPrefix, key), nil, true)
}

// del is true if this is a deletion
func (c *changes) put(key []byte, val []byte, del bool) {
	if val == nil && !del {
		// Firewood treats nil values as deletions, so we use a workaround to use
		// an empty slice until firewood supports this.
		val = []byte{}
	}

	c.Keys = append(c.Keys, key)
	c.Vals = append(c.Vals, val)

	if c.kv == nil {
		c.kv = make(map[string][]byte)
	}

	c.kv[string(key)] = val
}

func (c *changes) Get(key []byte) ([]byte, bool) {
	v, ok := c.kv[string(Prefix(appPrefix, key))]
	return v, ok
}

type DB struct {
	db     *ffi.Database
	height uint64
	// invariant: pending always has a length > 0 due to the inclusion of the
	// block height
	pending changes
}

func NewDB(path string) (*DB, error) {
	db, err := ffi.New(path, ffi.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("opening firewood db: %w", err)
	}

	var height uint64

	heightBytes, err := db.Get(Prefix(consensusPrefix, heightKey))
	if err != nil {
		return nil, fmt.Errorf("getting height: %w", err)
	}

	if heightBytes != nil {
		height, err = database.ParseUInt64(heightBytes)
		if err != nil {
			return nil, fmt.Errorf("parsing height: %w", err)
		}
	}

	return &DB{
		db:     db,
		height: height,
	}, nil
}

func (db *DB) Get(key []byte) ([]byte, error) {
	if val, ok := db.pending.Get(key); ok {
		return val, nil
	}

	val, err := db.db.Get(key)
	if val == nil && err == nil {
		return nil, database.ErrNotFound
	}

	return val, err
}

func (db *DB) Put(key []byte, val []byte) {
	db.pending.Put(key, val)
}

func (db *DB) Delete(key []byte) {
	db.pending.Delete(key)
}

func (db *DB) Height() uint64 {
	return db.height
}

func (db *DB) Root() (ids.ID, error) {
	root, err := db.db.Root()
	if err != nil {
		return ids.ID{}, err
	}

	return ids.ID(root), nil
}

func (db *DB) Abort() {
	db.pending = changes{}
}

// TODO single flush per block height
func (db *DB) Flush() error {
	db.height++
	db.pending.Put(
		Prefix(consensusPrefix, heightKey),
		database.PackUInt64(db.height),
	)

	p, err := db.db.Propose(db.pending.Keys, db.pending.Vals)
	if err != nil {
		return fmt.Errorf("proposing changes: %w", err)
	}

	if err := p.Commit(); err != nil {
		return fmt.Errorf("committing changes: %w", err)
	}

	db.pending = changes{}
	return nil
}

func (db *DB) Close() error {
	return db.db.Close()
}

// TODO test prefixing
//
// Prefix adds a prefix + delimiter
// prefix must not contain the delimiter
func Prefix(prefix []byte, key []byte) []byte {
	k := make([]byte, len(prefix)+len(key))

	copy(k, prefix)
	k[len(prefix)] = PrefixDelimiter
	copy(k[len(prefix)+1:], key)

	return k
}
