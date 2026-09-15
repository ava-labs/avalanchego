// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flatfirewood

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/database"
)

// The store holds one row per state change, keyed by the same hashed keys
// Firewood uses, plus the height at which each post-execution root was first
// produced:
//
//	A | keccak(addr)(32) | ^block(8)                    -> RLP(account)  (empty: absent or destroyed)
//	S | keccak(addr)(32) | keccak(slot)(32) | ^block(8) -> RLP(value)    (empty: deleted slot)
//	D | keccak(addr)(32) | ^block(8)                    -> {}            (account destruction marker)
//	R | root(32)                                        -> block(8)      (first height with this root)
//
// The block number is stored inverted (^block, big-endian) so a forward seek
// to (key | ^N) lands on the greatest block <= N for that key, i.e. the value
// as of the end of block N, using avalanchego's forward-only iterator.
// Contract code is not stored here: it lives in rawdb and is never pruned.
const (
	prefixAccount  byte = 'A'
	prefixStorage  byte = 'S'
	prefixDestruct byte = 'D'
	prefixRoot     byte = 'R'
)

type opKind uint8

const (
	opPut      opKind = iota // account (32-byte key) or storage (64-byte key) value
	opDelete                 // storage slot deleted
	opDestruct               // account and all its storage destroyed (Firewood PrefixDelete)
)

// op is one captured state mutation, with key and value encoded exactly as
// the corresponding Firewood batch op.
type op struct {
	kind  opKind
	key   []byte
	value []byte
}

type store struct {
	db database.Database
}

func rowKey(prefix byte, hashedKey []byte, block uint64) []byte {
	k := make([]byte, 1+len(hashedKey)+8)
	k[0] = prefix
	copy(k[1:], hashedKey)
	binary.BigEndian.PutUint64(k[1+len(hashedKey):], ^block)
	return k
}

func seekPrefix(prefix byte, hashedKey []byte) []byte {
	return append([]byte{prefix}, hashedKey...)
}

func blockFromTail(key []byte) uint64 {
	return ^binary.BigEndian.Uint64(key[len(key)-8:])
}

// flush durably writes one block's ops in a single batch and records root as
// first produced at block (unless an earlier height already produced it).
// Re-flushing a block, as happens when blocks are re-executed after a crash,
// rewrites identical rows.
func (s *store) flush(root common.Hash, block uint64, ops []op) error {
	batch := s.db.NewBatch()
	for _, o := range ops {
		isAccount := len(o.key) == common.HashLength
		var err error
		switch o.kind {
		case opPut:
			prefix := prefixStorage
			if isAccount {
				prefix = prefixAccount
			}
			err = batch.Put(rowKey(prefix, o.key, block), o.value)
		case opDelete:
			err = batch.Put(rowKey(prefixStorage, o.key, block), nil)
		case opDestruct:
			if err = batch.Put(rowKey(prefixDestruct, o.key, block), nil); err == nil {
				err = batch.Put(rowKey(prefixAccount, o.key, block), nil)
			}
		default:
			err = fmt.Errorf("unknown op kind %d", o.kind)
		}
		if err != nil {
			return err
		}
	}
	if _, ok, err := s.heightOf(root); err != nil {
		return err
	} else if !ok {
		if err := batch.Put(seekPrefix(prefixRoot, root[:]), binary.BigEndian.AppendUint64(nil, block)); err != nil {
			return err
		}
	}
	return batch.Write()
}

// heightOf returns the first block whose post-execution state root is root.
func (s *store) heightOf(root common.Hash) (uint64, bool, error) {
	v, err := s.db.Get(seekPrefix(prefixRoot, root[:]))
	switch {
	case errors.Is(err, database.ErrNotFound):
		return 0, false, nil
	case err != nil:
		return 0, false, err
	case len(v) != 8:
		return 0, false, fmt.Errorf("root %#x: want 8-byte height, got %d bytes", root, len(v))
	}
	return binary.BigEndian.Uint64(v), true, nil
}

// accountAt returns the account as of the end of block target, or nil if it
// did not exist then.
func (s *store) accountAt(hashedAddr common.Hash, target uint64) (*types.StateAccount, error) {
	enc, _, ok, err := s.latestRowLE(prefixAccount, hashedAddr[:], target)
	if err != nil || !ok || len(enc) == 0 {
		return nil, err
	}
	acc := new(types.StateAccount)
	if err := rlp.DecodeBytes(enc, acc); err != nil {
		return nil, fmt.Errorf("decoding account %#x at block %d: %w", hashedAddr, target, err)
	}
	return acc, nil
}

// destructAt returns the block of the latest destruction of the account at or
// before target, reporting ok=false if it was never destroyed by then.
func (s *store) destructAt(hashedAddr common.Hash, target uint64) (uint64, bool, error) {
	_, block, ok, err := s.latestRowLE(prefixDestruct, hashedAddr[:], target)
	return block, ok, err
}

// storageAt returns the RLP-encoded slot value as of the end of block target,
// or nil for a never-written, deleted, or destruct-cleared slot. destructBlock
// and destroyed are the result of [store.destructAt] for the account.
func (s *store) storageAt(hashedAddr, hashedSlot common.Hash, target uint64, destructBlock uint64, destroyed bool) ([]byte, error) {
	key := make([]byte, 0, 2*common.HashLength)
	key = append(append(key, hashedAddr[:]...), hashedSlot[:]...)
	value, writeBlock, ok, err := s.latestRowLE(prefixStorage, key, target)
	if err != nil || !ok || len(value) == 0 {
		return nil, err
	}
	// A destruction after the slot's last write clears it. A slot rewritten
	// in the same block as the destruction (the destruct op precedes the
	// re-creation ops) stays visible.
	if destroyed && destructBlock > writeBlock {
		return nil, nil
	}
	return value, nil
}

// latestRowLE returns the value and block of the greatest block <= target for
// the row, reporting ok=false if there is none.
func (s *store) latestRowLE(prefix byte, hashedKey []byte, target uint64) ([]byte, uint64, bool, error) {
	it := s.db.NewIteratorWithStartAndPrefix(rowKey(prefix, hashedKey, target), seekPrefix(prefix, hashedKey))
	defer it.Release()
	if !it.Next() {
		return nil, 0, false, it.Error()
	}
	return it.Value(), blockFromTail(it.Key()), true, it.Error()
}
