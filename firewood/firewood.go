// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"context"
	"fmt"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const PrefixDelimiter = '/'

var (
	reservedPrefix = []byte("reserved")
	heightKey      = []byte("height")
	appPrefix      = []byte("app")
)

type changes struct {
	Keys [][]byte
	Vals [][]byte

	kv map[string][]byte
}

func (c *changes) Put(key []byte, val []byte) {
	c.put(key, val, false)
}

func (c *changes) Delete(key []byte) {
	c.put(key, nil, true)
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
	v, ok := c.kv[string(key)]
	return v, ok
}

type DB struct {
	db                *ffi.Database
	height            uint64
	heightKey         []byte
	pending           changes
}

// New returns an instance of [DB]. If [DB] has not been written yet, it has
// an initial height key of `height`.
func New(path string, height uint64) (*DB, error) {
	db, err := ffi.New(path, ffi.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("opening firewood db: %w", err)
	}

	heightKey := Prefix(reservedPrefix, heightKey)
	heightBytes, err := db.Get(heightKey)
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
		db:                db,
		height:            height,
		heightKey:         heightKey,
	}, nil
}

// Get returns a key value pair or [database.ErrNotFound] if `key` is not in the
// [DB].
func (db *DB) Get(key []byte) ([]byte, error) {
	key = Prefix(appPrefix, key)

	if val, ok := db.pending.Get(key); ok {
		if val == nil {
			return nil, database.ErrNotFound
		}

		return val, nil
	}

	val, err := db.db.Get(key)
	if val == nil && err == nil {
		return nil, database.ErrNotFound
	}

	return val, err
}

// Put inserts a key value pair into [DB].
func (db *DB) Put(key []byte, val []byte) {
	db.pending.Put(Prefix(appPrefix, key), val)
}

func (db *DB) Delete(key []byte) {
	db.pending.Delete(Prefix(appPrefix, key))
}

// Height returns the last height of [DB] written to by [DB.Flush].
//
// If this returns false, the height has not been initialized yet.
func (db *DB) Height() uint64 {
	return db.height
}

// Root returns the merkle root of the state on disk ignoring pending writes.
func (db *DB) Root() (ids.ID, error) {
	root, err := db.db.Root()
	if err != nil {
		return ids.ID{}, err
	}

	return ids.ID(root[:]), nil
}

// Abort cancels all pending writes.
func (db *DB) Abort() {
	db.pending = changes{}
}

// Flush flushes pending writes to disk and increments [DB.Height].
func (db *DB) Flush() error {
	db.height++
	db.pending.Put(db.heightKey, database.PackUInt64(db.height))

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

func (db *DB) Close(ctx context.Context) error {
	return db.db.Close(ctx)
}

// Prefix prefixes `key` with `prefix` + [PrefixDelimiter].
func Prefix(prefix []byte, key []byte) []byte {
	k := make([]byte, len(prefix)+1+len(key))

	copy(k, prefix)
	k[len(prefix)] = PrefixDelimiter
	copy(k[len(prefix)+1:], key)

	return k
}
