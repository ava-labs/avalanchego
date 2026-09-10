package flatfirewood

import (
	"bytes"
	"encoding/binary"

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
