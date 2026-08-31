// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestRecoveryRebuildsMissingIndex(t *testing.T) {
	block := []byte("block")
	db := newDatabase(t, DefaultConfig())
	for height := range uint64(3) {
		require.NoError(t, db.Put(height, block))
	}
	require.NoError(t, db.Close())
	require.NoError(t, os.Remove(db.indexFile.Name()))

	db = newDatabase(t, db.config)
	for height := range uint64(3) {
		got, err := db.Get(height)
		require.NoError(t, err)
		require.Equal(t, block, got)
	}
	require.NoError(t, db.Close())
}

func TestRecoveryRebuildsTruncatedIndex(t *testing.T) {
	block := []byte("block")
	db := newDatabase(t, DefaultConfig())
	for height := range uint64(3) {
		require.NoError(t, db.Put(height, block))
	}
	firstEntry, err := db.readIndexEntry(0)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	checkpointOffset := firstEntry.Offset + uint64(sizeOfBlockEntryHeader) + uint64(firstEntry.Size)
	// Simulate a checkpoint and index that include only the first block.
	require.NoError(t, writeIndexFileHeader(db, 0, checkpointOffset))
	require.NoError(t, os.Truncate(db.indexFile.Name(), int64(sizeOfIndexFileHeader+sizeOfIndexEntry)))

	db = newDatabase(t, db.config)
	for height := range uint64(3) {
		got, err := db.Get(height)
		require.NoError(t, err)
		require.Equal(t, block, got)
	}
	require.NoError(t, db.Close())
}

func TestRecoveryRebuildsTruncatedIndexAcrossDataFiles(t *testing.T) {
	config := DefaultConfig().WithMaxDataFileSize(1024)
	db := newDatabase(t, config)
	// Fixed pseudo-random blocks force the second record into data file 1.
	rng := rand.NewChaCha8([32]byte{})
	blocks := make([][]byte, 2)
	for height := range blocks {
		blocks[height] = make([]byte, 512)
		_, err := rng.Read(blocks[height])
		require.NoError(t, err)
		require.NoError(t, db.Put(uint64(height), blocks[height]))
	}
	firstEntry, err := db.readIndexEntry(0)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.FileExists(t, db.dataFilePath(1))

	checkpointOffset := firstEntry.Offset + uint64(sizeOfBlockEntryHeader) + uint64(firstEntry.Size)
	require.NoError(t, writeIndexFileHeader(db, 0, checkpointOffset))
	require.NoError(t, os.Truncate(db.indexFile.Name(), int64(sizeOfIndexFileHeader+sizeOfIndexEntry)))

	db = newDatabase(t, db.config)
	for height, block := range blocks {
		got, err := db.Get(uint64(height))
		require.NoError(t, err)
		require.Equal(t, block, got)
	}
	require.NoError(t, db.Close())
}

func TestRecoveryStructuralCorruption(t *testing.T) {
	tests := []struct {
		name            string
		maxDataFileSize uint64
		nextWriteOffset uint64
		dataFileSizes   map[int]int
	}{
		{
			name:            "checkpoint_past_physical_data",
			maxDataFileSize: 1024,
			nextWriteOffset: 2,
			dataFileSizes:   map[int]int{0: 1},
		},
		{
			name:            "checkpoint_references_missing_data",
			maxDataFileSize: 1024,
			nextWriteOffset: 1,
		},
		{
			name:            "missing_intermediate_data_file",
			maxDataFileSize: 1024,
			dataFileSizes: map[int]int{
				0: 1024,
				2: 1,
			},
		},
		{
			name:            "multiple_data_files_in_single_file_mode",
			maxDataFileSize: math.MaxUint64,
			dataFileSizes: map[int]int{
				0: 1,
				1: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			header := indexFileHeader{
				Version:         IndexFileVersion,
				MaxDataFileSize: tt.maxDataFileSize,
				MaxHeight:       unsetHeight,
				NextWriteOffset: tt.nextWriteOffset,
			}
			headerBytes, err := header.MarshalBinary()
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, indexFileName),
				headerBytes,
				defaultFilePermissions,
			))
			for index, size := range tt.dataFileSizes {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, fmt.Sprintf(dataFileNameFormat, index)),
					make([]byte, size),
					defaultFilePermissions,
				))
			}

			config := DefaultConfig().
				WithDir(dir).
				WithMaxDataFileSize(tt.maxDataFileSize)
			_, err = New(config, logging.NoLog{})
			require.ErrorIs(t, err, ErrCorrupted)
		})
	}
}

func TestRecoveryLeavesPartialRecordSuffix(t *testing.T) {
	firstBlock := []byte("first block")
	secondBlock := []byte("second block")
	db := newDatabase(t, DefaultConfig())
	require.NoError(t, db.Put(0, firstBlock))
	require.NoError(t, db.Put(1, secondBlock))

	firstEntry, err := db.readIndexEntry(0)
	require.NoError(t, err)
	secondEntry, err := db.readIndexEntry(1)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	checkpointOffset := firstEntry.Offset + uint64(sizeOfBlockEntryHeader) + uint64(firstEntry.Size)
	// Treat the first block as checkpointed and truncate the later record mid-write.
	require.NoError(t, writeIndexFileHeader(db, 0, checkpointOffset))
	partialBlockEnd := secondEntry.Offset + uint64(sizeOfBlockEntryHeader) + uint64(secondEntry.Size)/2
	require.NoError(t, os.Truncate(db.dataFilePath(0), int64(partialBlockEnd)))

	db = newDatabase(t, db.config)
	got, err := db.Get(0)
	require.NoError(t, err)
	require.Equal(t, firstBlock, got)
	_, err = db.Get(1)
	require.ErrorIs(t, err, database.ErrNotFound)

	replacement := []byte("replacement block")
	require.NoError(t, db.Put(1, replacement))
	got, err = db.Get(1)
	require.NoError(t, err)
	require.Equal(t, replacement, got)
	require.NoError(t, db.Close())
}

func TestRecoveryLeavesIndexedDataAfterChecksumMismatch(t *testing.T) {
	checkpointBlock := []byte("checkpoint block")
	malformedBlock := []byte("malformed block")
	indexedBlock := []byte("indexed block")
	db := newDatabase(t, DefaultConfig())
	require.NoError(t, db.Put(10, checkpointBlock))
	require.NoError(t, db.Put(3, malformedBlock))
	require.NoError(t, db.Put(5, indexedBlock))
	malformedEntry, err := db.readIndexEntry(3)
	require.NoError(t, err)
	checkpointEntry, err := db.readIndexEntry(10)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	checkpointOffset := checkpointEntry.Offset + uint64(sizeOfBlockEntryHeader) + uint64(checkpointEntry.Size)
	// The later index entry remains usable even though recovery stops at height 3.
	require.NoError(t, writeIndexFileHeader(db, 10, checkpointOffset))
	require.NoError(t, writeBlockHeader(db, int64(malformedEntry.Offset), blockEntryHeader{
		Height:   3,
		Size:     malformedEntry.Size,
		Checksum: calculateChecksum(malformedBlock) + 1,
		Version:  BlockEntryVersion,
	}))

	db = newDatabase(t, db.config)
	got, err := db.Get(5)
	require.NoError(t, err)
	require.Equal(t, indexedBlock, got)
	_, err = db.Get(3)
	require.ErrorIs(t, err, ErrCorrupted)

	replacement := randomBlock(t)
	// A later Put must append after the preserved suffix without overwriting height 5.
	require.NoError(t, db.Put(6, replacement))
	got, err = db.Get(5)
	require.NoError(t, err)
	require.Equal(t, indexedBlock, got)
	require.NoError(t, db.Close())
}

func writeIndexFileHeader(db *Database, maxHeight, nextWriteOffset uint64) error {
	indexPath := db.indexFile.Name()
	indexFile, err := os.OpenFile(indexPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer indexFile.Close()

	header := db.header
	header.MaxHeight = maxHeight
	header.NextWriteOffset = nextWriteOffset

	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = indexFile.WriteAt(headerBytes, 0)
	return err
}

func writeBlockHeader(db *Database, offset int64, bh blockEntryHeader) error {
	fileIndex := int(offset / int64(db.header.MaxDataFileSize))
	localOffset := offset % int64(db.header.MaxDataFileSize)
	dataFilePath := db.dataFilePath(fileIndex)
	dataFile, err := os.OpenFile(dataFilePath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer dataFile.Close()

	headerBytes, err := bh.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = dataFile.WriteAt(headerBytes, localOffset)
	return err
}
