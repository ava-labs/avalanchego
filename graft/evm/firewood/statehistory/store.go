// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package statehistory implements a flat, full-history value store for
// Firewood archive nodes. Instead of persisting a Merkle trie per block, it
// records one row per state change, keyed by the same hashed keys Firewood
// uses:
//
//	A | keccak(addr)(32) | ^block(8)                  ->  RLP(account)  (empty value = absent/destroyed)
//	S | keccak(addr)(32) | keccak(slot)(32) | ^block(8) ->  RLP(value)  (empty value = deleted slot)
//	D | keccak(addr)(32) | ^block(8)                  ->  {}            (account destruction marker)
//	M | name                                          ->  metadata (first/head captured block)
//
// The block number is stored inverted (^block, big-endian) so that a plain
// forward seek to (key | ^N) lands on the greatest block <= N for that key,
// i.e. the value as of block N, using avalanchego's forward-only iterator.
// Contract code is not stored here: it lives in rawdb as usual and is never
// pruned.
//
// Rows are captured from the Firewood batch ops at proposal time and flushed
// by [TrieDB.Commit] in a single synced batch immediately BEFORE the Firewood
// proposal is committed. Firewood defers persistence (up to
// DeferredCommitInterval commits) and relies on client block replay after a
// crash; replay re-proposes and re-commits, rewriting identical history rows
// idempotently. History may therefore run ahead of the Firewood durable
// state, never behind, and the captured range [firstBlock, head] is always
// contiguous.
package statehistory

import (
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/database"
)

// OpKind distinguishes the state mutations captured from Firewood batch ops.
type OpKind uint8

const (
	// OpPut writes a new value: an RLP-encoded account (32-byte key) or an
	// RLP-encoded storage value (64-byte key).
	OpPut OpKind = iota
	// OpDelete removes a single storage slot (64-byte key).
	OpDelete
	// OpDestruct destroys an account and all its storage (32-byte key); it
	// mirrors Firewood's PrefixDelete(keccak(addr)).
	OpDestruct
)

// Op is one captured state mutation. Key is the hashed Firewood key:
// keccak(addr) for accounts, keccak(addr)||keccak(slot) for storage.
type Op struct {
	Kind  OpKind
	Key   []byte
	Value []byte // only for OpPut
}

// Key-space prefixes. Every row starts with exactly one of these.
const (
	prefixAccount  byte = 'A'
	prefixStorage  byte = 'S'
	prefixDestruct byte = 'D'
	prefixMeta     byte = 'M'
)

// Metadata names (under prefixMeta).
const (
	metaFirstBlock = "first" // lowest block number captured (uint64 BE)
	metaHead       = "head"  // highest block number captured (uint64 BE)
)

const hashedAccountKeyLen = common.HashLength // keccak(addr)

// Store is the flat historical value store. A single writer (the block
// committer) drives Flush; readers use it concurrently for historical state
// reconstruction.
type Store struct {
	db database.Database
}

// New returns a Store backed by db. db should be a dedicated database (or
// namespace) so the key prefixes above do not collide with other data.
func New(db database.Database) *Store {
	return &Store{db: db}
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// --- key builders ---------------------------------------------------------

// rowKey builds prefix || hashedKey || ^block.
func rowKey(prefix byte, hashedKey []byte, block uint64) []byte {
	k := make([]byte, 1+len(hashedKey)+8)
	k[0] = prefix
	copy(k[1:], hashedKey)
	binary.BigEndian.PutUint64(k[1+len(hashedKey):], ^block)
	return k
}

// seekPrefix builds prefix || hashedKey.
func seekPrefix(prefix byte, hashedKey []byte) []byte {
	k := make([]byte, 1+len(hashedKey))
	k[0] = prefix
	copy(k[1:], hashedKey)
	return k
}

func metaKey(name string) []byte {
	return append([]byte{prefixMeta}, name...)
}

// blockFromTail decodes the inverted big-endian block number stored in the
// trailing 8 bytes of a row key.
func blockFromTail(key []byte) uint64 {
	return ^binary.BigEndian.Uint64(key[len(key)-8:])
}

// --- write path ------------------------------------------------------------

// Flush durably persists one block's captured ops as flat rows in a single
// synced batch. It must be called before the Firewood proposal for this block
// is committed.
//
// The captured range must stay contiguous: the first flush ever must be the
// genesis block (a non-genesis first flush means history was enabled on an
// existing chain with no baseline snapshot), and later flushes must be at
// most head+1. Re-flushing any block <= head is the idempotent rewrite that
// happens when blocks are replayed after a crash. Violations are fatal: no
// silent holes ever.
func (s *Store) Flush(block uint64, ops []Op) error {
	head, ok, err := s.Head()
	if err != nil {
		return err
	}
	switch {
	case !ok && block != 0:
		log.Crit("state history has no baseline: enable state history only on a chain captured from genesis", "block", block)
	case ok && block > head+1:
		log.Crit("state history flush would leave a hole", "block", block, "head", head)
	}

	batch := s.db.NewBatch()
	for _, op := range ops {
		if err := writeOp(batch, block, op); err != nil {
			return err
		}
	}

	if !ok {
		if err := batch.Put(metaKey(metaFirstBlock), encodeUint64(block)); err != nil {
			return err
		}
	}
	if !ok || block > head {
		if err := batch.Put(metaKey(metaHead), encodeUint64(block)); err != nil {
			return err
		}
	}
	return batch.Write()
}

func writeOp(batch database.Batch, block uint64, op Op) error {
	isAccount := len(op.Key) == hashedAccountKeyLen
	switch op.Kind {
	case OpPut:
		prefix := prefixStorage
		if isAccount {
			prefix = prefixAccount
		}
		return batch.Put(rowKey(prefix, op.Key, block), op.Value)
	case OpDelete:
		if isAccount {
			return batch.Put(rowKey(prefixAccount, op.Key, block), nil)
		}
		return batch.Put(rowKey(prefixStorage, op.Key, block), nil)
	case OpDestruct:
		if !isAccount {
			return fmt.Errorf("statehistory: destruct op with %d-byte key", len(op.Key))
		}
		if err := batch.Put(rowKey(prefixDestruct, op.Key, block), nil); err != nil {
			return err
		}
		return batch.Put(rowKey(prefixAccount, op.Key, block), nil)
	default:
		return fmt.Errorf("statehistory: unknown op kind %d", op.Kind)
	}
}

// --- read path ------------------------------------------------------------

// AccountAt returns the account state for hashedAddr as of the end of block
// target, or (nil, nil) if the account did not exist at that height.
func (s *Store) AccountAt(hashedAddr common.Hash, target uint64) (*types.StateAccount, error) {
	enc, _, ok, err := s.latestRowLE(prefixAccount, hashedAddr[:], target)
	if err != nil || !ok || len(enc) == 0 {
		return nil, err
	}
	acc := new(types.StateAccount)
	if err := rlp.DecodeBytes(enc, acc); err != nil {
		return nil, fmt.Errorf("statehistory: decode account %x@%d: %w", hashedAddr, target, err)
	}
	return acc, nil
}

// StorageAt returns the RLP-encoded value of (hashedAddr, hashedSlot) as of
// the end of block target. A never-written, deleted, or destruct-cleared slot
// returns (nil, nil), which the caller interprets as the zero value.
func (s *Store) StorageAt(hashedAddr, hashedSlot common.Hash, target uint64) ([]byte, error) {
	key := make([]byte, 2*common.HashLength)
	copy(key, hashedAddr[:])
	copy(key[common.HashLength:], hashedSlot[:])

	value, writeBlock, ok, err := s.latestRowLE(prefixStorage, key, target)
	if err != nil || !ok || len(value) == 0 {
		return nil, err
	}

	// If the account was destroyed after this slot's last write (and at or
	// before target), the slot was cleared and reads as zero. A slot
	// re-written at or after the destruction (including in the same block,
	// where the destruct op precedes the re-creation ops) stays visible.
	destructBlock, ok, err := s.latestRowBlockLE(prefixDestruct, hashedAddr[:], target)
	if err != nil {
		return nil, err
	}
	if ok && destructBlock > writeBlock {
		return nil, nil
	}
	return value, nil
}

// latestRowLE returns the value and block of the greatest block <= target for
// the given row, reporting ok=false if no such row exists.
func (s *Store) latestRowLE(prefix byte, hashedKey []byte, target uint64) ([]byte, uint64, bool, error) {
	it := s.db.NewIteratorWithStartAndPrefix(rowKey(prefix, hashedKey, target), seekPrefix(prefix, hashedKey))
	defer it.Release()

	if !it.Next() {
		return nil, 0, false, it.Error()
	}
	block := blockFromTail(it.Key())
	value := append([]byte(nil), it.Value()...)
	return value, block, true, it.Error()
}

func (s *Store) latestRowBlockLE(prefix byte, hashedKey []byte, target uint64) (uint64, bool, error) {
	_, block, ok, err := s.latestRowLE(prefix, hashedKey, target)
	return block, ok, err
}

// --- watermarks -----------------------------------------------------------

// FirstBlock returns the lowest block number captured, reporting ok=false if
// the store is empty.
func (s *Store) FirstBlock() (uint64, bool, error) { return s.getUint64Meta(metaFirstBlock) }

// Head returns the highest block number captured, reporting ok=false if the
// store is empty.
func (s *Store) Head() (uint64, bool, error) { return s.getUint64Meta(metaHead) }

func (s *Store) getUint64Meta(name string) (uint64, bool, error) {
	v, err := s.db.Get(metaKey(name))
	if err == database.ErrNotFound {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if len(v) != 8 {
		return 0, false, fmt.Errorf("statehistory: meta %q: want 8 bytes, got %d", name, len(v))
	}
	return binary.BigEndian.Uint64(v), true, nil
}

func encodeUint64(n uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n)
	return b[:]
}
