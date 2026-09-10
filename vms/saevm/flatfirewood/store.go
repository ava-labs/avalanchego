package flatfirewood

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
)

type store struct {
	db database.Database
}

func rowKey(prefix byte, key []byte, block uint64) []byte {
	out := make([]byte, 1+len(key)+8)
	out[0] = prefix
	copy(out[1:], key)
	binary.BigEndian.PutUint64(out[1+len(key):], ^block)
	return out
}

func blockFromRowKey(key []byte) uint64 {
	return ^binary.BigEndian.Uint64(key[len(key)-8:])
}

func (s *store) latestRowLE(prefix byte, key []byte, targetBlock uint64) ([]byte, uint64, bool, error) {
	targetKey := rowKey(prefix, key, targetBlock)
	iter := s.db.NewIteratorWithStartAndPrefix(targetKey, targetKey[:len(targetKey)-8])
	defer iter.Release()

	if !iter.Next() {
		return nil, 0, false, iter.Error()
	}

	block := blockFromRowKey(iter.Key())
	value := bytes.Clone(iter.Value())
	return value, block, true, iter.Error()
}

type mutationKind uint8

const (
	mutationPut mutationKind = iota
	mutationDelete
	mutationDestruct
)

type mutation struct {
	kind  mutationKind
	key   []byte
	value []byte
}

const (
	prefixAccount  byte = 'A'
	prefixStorage  byte = 'S'
	prefixDestruct byte = 'D'
)

func (s *store) flush(block uint64, mutations []mutation) error {
	batch := s.db.NewBatch()

	for _, mutation := range mutations {
		switch mutation.kind {
		case mutationPut, mutationDelete:
			if mutation.kind == mutationDelete {
				mutation.value = nil
			}
			var prefix byte
			if len(mutation.key) == 20 {
				prefix = prefixAccount
			} else {
				prefix = prefixStorage
			}
			targetKey := rowKey(prefix, mutation.key, block)
			err := batch.Put(targetKey, mutation.value)
			if err != nil {
				return err
			}
		case mutationDestruct:
			if len(mutation.key) != 20 {
				return fmt.Errorf("destruct key must be 20 bytes")
			}
			err := batch.Put(rowKey(prefixAccount, mutation.key, block), nil)
			if err != nil {
				return err
			}
			err = batch.Put(rowKey(prefixDestruct, mutation.key, block), nil)
			if err != nil {
				return err
			}
		}
	}
	return batch.Write()
}
