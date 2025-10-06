// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/binary"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type BlockDB struct {
	db database.Database
}

func NewBlockDB(dbDir string) (*BlockDB, error) {
	db, err := leveldb.New(dbDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		return nil, fmt.Errorf("failed to create leveldb block database from %q: %w", dbDir, err)
	}
	return &BlockDB{db: db}, nil
}

func (b *BlockDB) WriteBlock(height uint64, bytes []byte) error {
	if err := b.db.Put(blockKey(height), bytes); err != nil {
		return fmt.Errorf("failed to write block at height %d: %w", height, err)
	}
	return nil
}

func (b *BlockDB) ReadBlock(height uint64) ([]byte, error) {
	bytes, err := b.db.Get(blockKey(height))
	if err != nil {
		return nil, fmt.Errorf("failed to read block at height %d: %w", height, err)
	}
	return bytes, nil
}

func (b *BlockDB) NewIteratorFromHeight(height uint64) database.Iterator {
	return b.db.NewIteratorWithStartAndPrefix(blockKey(height), nil)
}

func (b *BlockDB) Close() error {
	return b.db.Close()
}

func blockKey(height uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, height)
}
