// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package encdb

import (
	"crypto/cipher"
	"crypto/rand"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/nodb"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	codecVersion = 0
)

var (
	_ database.Database = &Database{}
	_ database.Batch    = &batch{}
	_ database.Iterator = &iterator{}
)

// Database encrypts all values that are provided
type Database struct {
	lock   sync.RWMutex
	codec  codec.Manager
	cipher cipher.AEAD
	db     database.Database
}

// New returns a new encrypted database
func New(password []byte, db database.Database) (*Database, error) {
	h := hashing.ComputeHash256(password)
	aead, err := chacha20poly1305.NewX(h)
	if err != nil {
		return nil, err
	}
	c := linearcodec.NewDefault()
	manager := codec.NewDefaultManager()
	return &Database{
		codec:  manager,
		cipher: aead,
		db:     db,
	}, manager.RegisterCodec(codecVersion, c)
}

// Has implements the Database interface
func (db *Database) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return false, database.ErrClosed
	}
	return db.db.Has(key)
}

// Get implements the Database interface
func (db *Database) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return nil, database.ErrClosed
	}
	encVal, err := db.db.Get(key)
	if err != nil {
		return nil, err
	}
	return db.decrypt(encVal)
}

// Put implements the Database interface
func (db *Database) Put(key, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}

	encValue, err := db.encrypt(value)
	if err != nil {
		return err
	}
	return db.db.Put(key, encValue)
}

// Delete implements the Database interface
func (db *Database) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	return db.db.Delete(key)
}

// NewBatch implements the Database interface
func (db *Database) NewBatch() database.Batch {
	return &batch{
		Batch: db.db.NewBatch(),
		db:    db,
	}
}

// NewIterator implements the Database interface
func (db *Database) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

// NewIteratorWithStart implements the Database interface
func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

// NewIteratorWithPrefix implements the Database interface
func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

// NewIteratorWithStartAndPrefix implements the Database interface
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return &nodb.Iterator{Err: database.ErrClosed}
	}
	return &iterator{
		Iterator: db.db.NewIteratorWithStartAndPrefix(start, prefix),
		db:       db,
	}
}

// Stat implements the Database interface
func (db *Database) Stat(stat string) (string, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.db == nil {
		return "", database.ErrClosed
	}
	return db.db.Stat(stat)
}

// Compact implements the Database interface
func (db *Database) Compact(start, limit []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	return db.db.Compact(start, limit)
}

// Close implements the Database interface
func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.db == nil {
		return database.ErrClosed
	}
	db.db = nil
	return nil
}

func (db *Database) isClosed() bool {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.db == nil
}

type keyValue struct {
	key    []byte
	value  []byte
	delete bool
}

type batch struct {
	database.Batch

	db     *Database
	writes []keyValue
}

func (b *batch) Put(key, value []byte) error {
	b.writes = append(b.writes, keyValue{utils.CopyBytes(key), utils.CopyBytes(value), false})
	encValue, err := b.db.encrypt(value)
	if err != nil {
		return err
	}
	return b.Batch.Put(key, encValue)
}

func (b *batch) Delete(key []byte) error {
	b.writes = append(b.writes, keyValue{utils.CopyBytes(key), nil, true})
	return b.Batch.Delete(key)
}

func (b *batch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	if b.db.db == nil {
		return database.ErrClosed
	}

	return b.Batch.Write()
}

// Reset resets the batch for reuse.
func (b *batch) Reset() {
	if cap(b.writes) > len(b.writes)*database.MaxExcessCapacityFactor {
		b.writes = make([]keyValue, 0, cap(b.writes)/database.CapacityReductionFactor)
	} else {
		b.writes = b.writes[:0]
	}
	b.Batch.Reset()
}

// Replay replays the batch contents.
func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, keyvalue := range b.writes {
		if keyvalue.delete {
			if err := w.Delete(keyvalue.key); err != nil {
				return err
			}
		} else if err := w.Put(keyvalue.key, keyvalue.value); err != nil {
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

func (it *iterator) Key() []byte { return it.key }

func (it *iterator) Value() []byte { return it.val }

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
	return db.codec.Marshal(codecVersion, &encryptedValue{
		Ciphertext: ciphertext,
		Nonce:      nonce,
	})
}

func (db *Database) decrypt(ciphertext []byte) ([]byte, error) {
	val := encryptedValue{}
	if _, err := db.codec.Unmarshal(ciphertext, &val); err != nil {
		return nil, err
	}
	return db.cipher.Open(nil, val.Nonce, val.Ciphertext, nil)
}
