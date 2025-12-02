// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reexecute

import (
	"encoding/binary"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// BlockResult represents the result of reading a block from the database.
// It contains either the block data and height, or an error if the read failed.
type BlockResult struct {
	BlockBytes []byte
	Height     uint64
	Err        error
}

// CreateBlockChanFromLevelDB creates a channel that streams blocks from a LevelDB database.
// It opens the database at sourceDir and iterates through blocks from startBlock to endBlock (inclusive).
// Blocks are read sequentially and sent to the returned channel as BlockResult values.
//
// Any validation errors or iteration errors are sent as BlockResult with Err set, then the channel is closed.
func CreateBlockChanFromLevelDB(sourceDir string, startBlock, endBlock uint64, chanSize int, cleanup func(func())) (<-chan BlockResult, error) {
	ch := make(chan BlockResult, chanSize)

	db, err := leveldb.New(sourceDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		return nil, fmt.Errorf("failed to create leveldb database from %q: %w", sourceDir, err)
	}
	cleanup(func() {
		db.Close()
	})

	go func() {
		defer close(ch)

		iter := db.NewIteratorWithStart(BlockKey(startBlock))
		defer iter.Release()

		currentHeight := startBlock

		for iter.Next() {
			key := iter.Key()
			if len(key) != database.Uint64Size {
				ch <- BlockResult{
					BlockBytes: nil,
					Err:        fmt.Errorf("expected key length %d while looking for block at height %d, got %d", database.Uint64Size, currentHeight, len(key)),
				}
				return
			}
			height := binary.BigEndian.Uint64(key)
			if height != currentHeight {
				ch <- BlockResult{
					BlockBytes: nil,
					Err:        fmt.Errorf("expected next height %d, got %d", currentHeight, height),
				}
				return
			}
			ch <- BlockResult{
				BlockBytes: iter.Value(),
				Height:     height,
			}
			currentHeight++
			if currentHeight > endBlock {
				break
			}
		}
		if iter.Error() != nil {
			ch <- BlockResult{
				BlockBytes: nil,
				Err:        fmt.Errorf("failed to iterate over blocks at height %d: %w", currentHeight, iter.Error()),
			}
			return
		}
	}()

	return ch, nil
}

// BlockKey converts a block height to its corresponding database key.
func BlockKey(height uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, height)
}
