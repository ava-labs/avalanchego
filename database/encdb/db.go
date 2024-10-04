// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package encdb

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"slices"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	_ database.Database = (*Database)(nil)
	_ database.Batch    = (*batch)(nil)
	_ database.Iterator = (*iterator)(nil)
)

// Database encrypts all values that are provided
type Database struct {
	lock   sync.RWMutex
	cipher cipher.AEAD
	db     database.Database
	closed bool
}

// New returns a new encrypted database
func New(password []byte, db database.Database) (*Database, error) {
	h := hashing.ComputeHash256(password)
	aead, err := chacha20poly1305.NewX(h)
	return &Database{
		cipher: aead,
		db:     db,
	}, err
}

func (db *Database) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return false, database.ErrClosed
	}
	return db.db.Has(key)
}

func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}
	encVal, err := db.db.Get(key)
	if err != nil {
		return nil, err
	}
	return db.decrypt(encVal)
}

func (db *Database) Put(key, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	encValue, err := db.encrypt(value)
	if err != nil {
		return err
	}
	return db.db.Put(key, encValue)
}

func (db *Database) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}
	return db.db.Delete(key)
}

func (db *Database) NewBatch() database.Batch {
	return &batch{
		Batch: db.db.NewBatch(),
		db:    db,
	}
}

func (db *Database) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return &database.IteratorError{
			Err: database.ErrClosed,
		}
	}
	return &iterator{
		Iterator: db.db.NewIteratorWithStartAndPrefix(start, prefix),
		db:       db,
	}
}

func (db *Database) Compact(start, limit []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}
	return db.db.Compact(start, limit)
}

func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}
	db.closed = true
	return nil
}

func (db *Database) isClosed() bool {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.closed
}

func (db *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}
	return db.db.HealthCheck(ctx)
}

type batch struct {
	database.Batch

	db  *Database
	ops []database.BatchOp
}

func (b *batch) Put(key, value []byte) error {
	b.ops = append(b.ops, database.BatchOp{
		Key:   slices.Clone(key),
		Value: slices.Clone(value),
	})
	encValue, err := b.db.encrypt(value)
	if err != nil {
		return err
	}
	return b.Batch.Put(key, encValue)
}

func (b *batch) Delete(key []byte) error {
	b.ops = append(b.ops, database.BatchOp{
		Key:    slices.Clone(key),
		Delete: true,
	})
	return b.Batch.Delete(key)
}

func (b *batch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	if b.db.closed {
		return database.ErrClosed
	}

	return b.Batch.Write()
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	if cap(b.ops) > len(b.ops)*database.MaxExcessCapacityFactor {
		b.ops = make([]database.BatchOp, 0, cap(b.ops)/database.CapacityReductionFactor)
	} else {
		clear(b.ops)
		b.ops = b.ops[:0]
	}
	b.Batch.Reset()
}

// Replay replays the batch contents.
func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, op := range b.ops {
		if op.Delete {
			if err := w.Delete(op.Key); err != nil {
				return err
			}
		} else if err := w.Put(op.Key, op.Value); err != nil {
			return err
		}
	}
	return nil
}

type iterator struct {
	database.Iterator
	db *Database

	val, key []byte
	err      error
}

func (it *iterator) Next() bool {
	// Short-circuit and set an error if the underlying database has been closed.
	if it.db.isClosed() {
		it.val = nil
		it.key = nil
		it.err = database.ErrClosed
		return false
	}

	next := it.Iterator.Next()
	if next {
		encVal := it.Iterator.Value()
		val, err := it.db.decrypt(encVal)
		if err != nil {
			it.err = err
			return false
		}
		it.val = val
		it.key = it.Iterator.Key()
	} else {
		it.val = nil
		it.key = nil
	}
	return next
}

func (it *iterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.Iterator.Error()
}

func (it *iterator) Key() []byte {
	return it.key
}

func (it *iterator) Value() []byte {
	return it.val
}

type encryptedValue struct {
	Ciphertext []byte `serialize:"true"`
	Nonce      []byte `serialize:"true"`
}

func (db *Database) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := db.cipher.Seal(nil, nonce, plaintext, nil)
	return Codec.Marshal(CodecVersion, &encryptedValue{
		Ciphertext: ciphertext,
		Nonce:      nonce,
	})
}

func (db *Database) decrypt(ciphertext []byte) ([]byte, error) {
	val := encryptedValue{}
	if _, err := Codec.Unmarshal(ciphertext, &val); err != nil {
		return nil, err
	}
	return db.cipher.Open(nil, val.Nonce, val.Ciphertext, nil)
}
