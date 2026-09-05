// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/DataDog/zstd"
	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/logging"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	indexFileName          = "blockdb.idx"
	dataFileNameFormat     = "blockdb_%d.dat"
	defaultFilePermissions = 0o666

	// Since 0 is a valid height, math.MaxUint64 is used to indicate unset height.
	// It is not possible for block height to be max uint64 as it would overflow the index entry offset
	unsetHeight = math.MaxUint64

	// IndexFileVersion is the version of the index file format.
	IndexFileVersion uint64 = 1

	// BlockEntryVersion is the version of the block entry.
	BlockEntryVersion uint16 = 1
)

// BlockHeight defines the type for block heights.
type BlockHeight = uint64

// BlockData defines the type for block data.
type BlockData = []byte

var (
	_ database.HeightIndex = (*Database)(nil)

	_ encoding.BinaryMarshaler   = (*blockEntryHeader)(nil)
	_ encoding.BinaryUnmarshaler = (*blockEntryHeader)(nil)
	_ encoding.BinaryMarshaler   = (*indexEntry)(nil)
	_ encoding.BinaryUnmarshaler = (*indexEntry)(nil)
	_ encoding.BinaryMarshaler   = (*indexFileHeader)(nil)
	_ encoding.BinaryUnmarshaler = (*indexFileHeader)(nil)

	sizeOfBlockEntryHeader = uint32(binary.Size(blockEntryHeader{}))
	sizeOfIndexEntry       = uint64(binary.Size(indexEntry{}))
	sizeOfIndexFileHeader  = uint64(binary.Size(indexFileHeader{}))
)

// blockEntryHeader is the header of a block entry in the data file.
// This is not the header portion of the block data itself.
type blockEntryHeader struct {
	Height   BlockHeight
	Size     uint32
	Checksum uint64
	Version  uint16
}

// MarshalBinary implements the encoding.BinaryMarshaler interface.
func (beh blockEntryHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, sizeOfBlockEntryHeader)
	binary.LittleEndian.PutUint64(buf[0:], beh.Height)
	binary.LittleEndian.PutUint32(buf[8:], beh.Size)
	binary.LittleEndian.PutUint64(buf[12:], beh.Checksum)
	binary.LittleEndian.PutUint16(buf[20:], beh.Version)
	return buf, nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
func (beh *blockEntryHeader) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfBlockEntryHeader) {
		return fmt.Errorf("%w: incorrect data length to unmarshal blockEntryHeader: got %d bytes, need exactly %d", ErrCorrupted, len(data), sizeOfBlockEntryHeader)
	}
	beh.Height = binary.LittleEndian.Uint64(data[0:])
	beh.Size = binary.LittleEndian.Uint32(data[8:])
	beh.Checksum = binary.LittleEndian.Uint64(data[12:])
	beh.Version = binary.LittleEndian.Uint16(data[20:])
	return nil
}

// indexEntry represents an entry in the index file.
type indexEntry struct {
	// Offset is the byte offset in the data file where the block's header starts.
	Offset uint64
	// Size is the length in bytes of the block's data (excluding the blockHeader).
	Size uint32
	// Reserved for future use and ensures alignment
	Reserved [4]byte
}

// IsEmpty returns true if this entry is uninitialized.
// This indicates a slot where no block has been written.
func (e indexEntry) IsEmpty() bool {
	return e.Offset == 0 && e.Size == 0
}

// MarshalBinary implements encoding.BinaryMarshaler for indexEntry.
func (e indexEntry) MarshalBinary() ([]byte, error) {
	buf := make([]byte, sizeOfIndexEntry)
	binary.LittleEndian.PutUint64(buf[0:], e.Offset)
	binary.LittleEndian.PutUint32(buf[8:], e.Size)
	return buf, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler for indexEntry.
func (e *indexEntry) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfIndexEntry) {
		return fmt.Errorf("%w: incorrect data length to unmarshal indexEntry: got %d bytes, need exactly %d", ErrCorrupted, len(data), sizeOfIndexEntry)
	}
	e.Offset = binary.LittleEndian.Uint64(data[0:])
	e.Size = binary.LittleEndian.Uint32(data[8:])
	return nil
}

// indexFileHeader is the header of the index file.
type indexFileHeader struct {
	Version         uint64
	MaxDataFileSize uint64
	MinHeight       BlockHeight
	MaxHeight       BlockHeight
	NextWriteOffset uint64
	// reserve remaining 24 bytes for future use while keeping the
	// size of the index file header multiple of sizeOfIndexEntry.
	Reserved [24]byte
}

// MarshalBinary implements encoding.BinaryMarshaler for indexFileHeader.
func (h indexFileHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, sizeOfIndexFileHeader)
	binary.LittleEndian.PutUint64(buf[0:], h.Version)
	binary.LittleEndian.PutUint64(buf[8:], h.MaxDataFileSize)
	binary.LittleEndian.PutUint64(buf[16:], h.MinHeight)
	binary.LittleEndian.PutUint64(buf[24:], h.MaxHeight)
	binary.LittleEndian.PutUint64(buf[32:], h.NextWriteOffset)
	return buf, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler for indexFileHeader.
func (h *indexFileHeader) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfIndexFileHeader) {
		return fmt.Errorf(
			"%w: incorrect data length to unmarshal indexFileHeader: got %d bytes, need exactly %d",
			ErrCorrupted, len(data), sizeOfIndexFileHeader,
		)
	}
	h.Version = binary.LittleEndian.Uint64(data[0:])
	h.MaxDataFileSize = binary.LittleEndian.Uint64(data[8:])
	h.MinHeight = binary.LittleEndian.Uint64(data[16:])
	h.MaxHeight = binary.LittleEndian.Uint64(data[24:])
	h.NextWriteOffset = binary.LittleEndian.Uint64(data[32:])
	return nil
}

// Database stores blockchain blocks on disk and provides methods to read and write blocks.
type Database struct {
	indexFile  *os.File
	locks      *dbLocks
	config     DatabaseConfig
	header     indexFileHeader
	log        logging.Logger
	closed     bool
	fileCache  *lru.Cache[int, *os.File]
	compressor compression.Compressor

	// closeMu prevents the database from being closed while in use and prevents
	// use of a closed database.
	closeMu sync.RWMutex

	// fileOpenMu prevents race conditions when multiple threads try to open the same data file
	fileOpenMu sync.Mutex
	// checkpointMu keeps a checkpoint from observing an incomplete Put.
	checkpointMu sync.RWMutex

	// maxBlockHeight tracks the highest block height written
	maxBlockHeight atomic.Uint64
	// nextDataReservationOffset tracks the next position available for a data write.
	nextDataReservationOffset atomic.Uint64
}

// New creates a block database.
// Parameters:
//   - config: Configuration parameters
//   - log: Logger instance for structured logging
func New(config DatabaseConfig, log logging.Logger) (_ database.HeightIndex, err error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	databaseLog := log
	if databaseLog == nil {
		databaseLog = logging.NoLog{}
	}

	// from benchmarks, zstd.BestSpeed is about 100% faster than the default
	// compression level while giving us ~5% better compression ratio than Snappy.
	compressor, err := compression.NewZstdCompressorWithLevel(math.MaxUint32, zstd.BestSpeed)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize compressor: %w", err)
	}

	s := &Database{
		config: config,
		log:    databaseLog,
		fileCache: lru.NewCacheWithOnEvict(config.MaxDataFiles, func(_ int, f *os.File) {
			f.Close()
		}),
		compressor: compressor,
	}

	s.log.Info("Initializing BlockDB",
		zap.String("indexDir", config.IndexDir),
		zap.String("dataDir", config.DataDir),
		zap.Uint64("maxDataFileSize", config.MaxDataFileSize),
		zap.Int("maxDataFiles", config.MaxDataFiles),
		zap.Uint16("blockCacheSize", config.BlockCacheSize),
	)

	// Locks must be acquired before any database file is opened or recovery is
	// attempted; otherwise two processes can race on recovery and corrupt the index.
	s.locks, err = acquireDBLocks(config.IndexDir, config.DataDir)
	if err != nil {
		s.log.Error("Failed to acquire directory lock", zap.Error(err))
		return nil, err
	}
	defer func() {
		if err != nil {
			if rErr := s.locks.Release(); rErr != nil {
				s.log.Error("Failed to release directory lock after failed initialization", zap.Error(rErr))
			}
		}
	}()

	if err := s.openAndInitializeIndex(); err != nil {
		s.log.Error("Failed to initialize database: failed to initialize index", zap.Error(err))
		return nil, err
	}

	if err := s.recover(); err != nil {
		s.log.Error("Failed to initialize database: recovery failed", zap.Error(err))
		s.closeFiles()
		return nil, fmt.Errorf("recovery failed: %w", err)
	}

	maxHeight := s.maxBlockHeight.Load()
	s.log.Info("BlockDB initialized successfully",
		zap.Uint64("nextDataReservationOffset", s.nextDataReservationOffset.Load()),
		zap.Uint64("maxBlockHeight", maxHeight),
	)

	if config.BlockCacheSize > 0 {
		return newCacheDB(s, config.BlockCacheSize), nil
	}
	return s, nil
}

// Close flushes pending writes and closes the store files.
func (s *Database) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	if s.closed {
		return database.ErrClosed
	}
	s.closed = true

	err := s.persistIndexHeader()
	if err != nil {
		s.log.Error("Failed to persist index header", zap.Error(err))
	}

	s.closeFiles()

	if rErr := s.locks.Release(); rErr != nil {
		s.log.Error("Failed to release directory lock", zap.Error(rErr))
	}

	s.log.Info("Block database closed successfully")
	return err
}

// Put inserts a block into the store at the given height.
func (s *Database) Put(height BlockHeight, block BlockData) error {
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()

	if s.closed {
		s.log.Error("Failed Put: database closed", zap.Uint64("height", height))
		return database.ErrClosed
	}

	blockSize := len(block)
	if blockSize > math.MaxUint32 {
		s.log.Error("Failed to write block: block size exceeds max size for uint32",
			zap.Uint64("height", height),
			zap.Int("blockSize", blockSize),
		)
		return fmt.Errorf("%w: block size cannot exceed %d bytes", ErrBlockTooLarge, math.MaxUint32)
	}

	indexFileOffset, err := s.indexEntryOffset(height)
	if err != nil {
		s.log.Error("Failed to write block: failed to calculate index entry offset",
			zap.Uint64("height", height),
			zap.Error(err),
		)
		return fmt.Errorf("failed to get index entry offset for block at height %d: %w", height, err)
	}

	blockToWrite, err := s.compressor.Compress(block)
	if err != nil {
		s.log.Error("Failed to write block: error compressing block data",
			zap.Uint64("height", height),
			zap.Error(err),
		)
		return fmt.Errorf("failed to compress block data: %w", err)
	}
	blockDataLen := uint32(len(blockToWrite))

	sizeWithDataHeader, err := safemath.Add(sizeOfBlockEntryHeader, blockDataLen)
	if err != nil {
		s.log.Error("Failed to write block: block size calculation overflow",
			zap.Uint64("height", height),
			zap.Uint32("blockSize", blockDataLen),
			zap.Error(err),
		)
		return fmt.Errorf("calculating total block size would overflow for block at height %d: %w", height, err)
	}
	s.checkpointMu.RLock()
	checkpointLocked := true
	defer func() {
		if checkpointLocked {
			s.checkpointMu.RUnlock()
		}
	}()

	reservation, err := s.allocateBlockSpace(sizeWithDataHeader)
	if err != nil {
		s.log.Error("Failed to write block: failed to allocate block space",
			zap.Uint64("height", height),
			zap.Uint32("totalSize", sizeWithDataHeader),
			zap.Error(err),
		)
		return err
	}
	writeDataOffset := reservation.writeOffset

	bh := blockEntryHeader{
		Height:   height,
		Size:     blockDataLen,
		Checksum: calculateChecksum(block),
		Version:  BlockEntryVersion,
	}
	if err := s.writeBlockAt(writeDataOffset, bh, blockToWrite); err != nil {
		// Reclaim the range only if no later Put has reserved past it.
		s.nextDataReservationOffset.CompareAndSwap(reservation.endOffset, reservation.previousOffset)
		s.log.Error("Failed to write block: error writing block data",
			zap.Uint64("height", height),
			zap.Uint64("dataOffset", writeDataOffset),
			zap.Error(err),
		)
		return err
	}

	if err := s.writeIndexEntryAt(indexFileOffset, writeDataOffset, blockDataLen); err != nil {
		s.log.Error("Failed to write block: error writing index entry",
			zap.Uint64("height", height),
			zap.Uint64("indexOffset", indexFileOffset),
			zap.Uint64("dataOffset", writeDataOffset),
			zap.Error(err),
		)
		return err
	}

	s.updateBlockMaxHeight(height)
	// The index entry is complete, so a checkpoint may proceed.
	s.checkpointMu.RUnlock()
	checkpointLocked = false

	if height%s.config.CheckpointInterval == 0 {
		if err := s.persistIndexHeader(); err != nil {
			err = fmt.Errorf("block %d was written, but checkpointing failed: %w", height, err)
			s.log.Error("Failed to checkpoint written block",
				zap.Uint64("height", height),
				zap.Error(err),
			)
			return err
		}
	}

	s.log.Debug("Block written successfully",
		zap.Uint64("height", height),
		zap.Uint32("blockSize", blockDataLen),
		zap.Uint64("dataOffset", writeDataOffset),
	)

	return nil
}

// readBlockIndex reads the index entry for the given height.
// It returns database.ErrNotFound if the block does not exist.
func (s *Database) readBlockIndex(height BlockHeight) (indexEntry, error) {
	var entry indexEntry

	// Skip the index entry read if we know the block is past the max height.
	maxHeight := s.maxBlockHeight.Load()
	if maxHeight == unsetHeight {
		s.log.Debug("Block not found",
			zap.Uint64("height", height),
			zap.String("reason", "no blocks written yet"),
		)
		return entry, fmt.Errorf("%w: no blocks written yet", database.ErrNotFound)
	}
	if height > maxHeight {
		s.log.Debug("Block not found",
			zap.Uint64("height", height),
			zap.Uint64("maxHeight", maxHeight),
			zap.String("reason", "height beyond max"),
		)
		return entry, fmt.Errorf("%w: height %d is beyond max height %d", database.ErrNotFound, height, maxHeight)
	}

	entry, err := s.readIndexEntry(height)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			s.log.Debug("Block not found",
				zap.Uint64("height", height),
				zap.String("reason", "no index entry found"),
				zap.Error(err),
			)
		} else {
			s.log.Error("Failed to read block index: failed to read index entry",
				zap.Uint64("height", height),
				zap.Error(err),
			)
		}
		return entry, err
	}

	return entry, nil
}

// Get retrieves a block by its height.
// Returns database.ErrNotFound if the block is not found.
func (s *Database) Get(height BlockHeight) (BlockData, error) {
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()

	if s.closed {
		s.log.Error("Failed Get: database closed", zap.Uint64("height", height))
		return nil, database.ErrClosed
	}

	indexEntry, err := s.readBlockIndex(height)
	if err != nil {
		return nil, err
	}

	dataEnd, err := s.dataFileEndForOffset(indexEntry.Offset)
	if err != nil {
		if errors.Is(err, ErrCorrupted) {
			return nil, s.blockUnavailableError(height, indexEntry, err)
		}
		return nil, err
	}
	bh, block, err := s.readBlockAtOffset(indexEntry.Offset, dataEnd, &indexEntry.Size)
	if err != nil {
		if errors.Is(err, ErrCorrupted) {
			return nil, s.blockUnavailableError(height, indexEntry, err)
		}
		return nil, err
	}
	if bh.Height != height {
		return nil, s.blockUnavailableError(height, indexEntry, fmt.Errorf(
			"%w: requested block height %d does not match stored height %d",
			ErrCorrupted,
			height,
			bh.Height,
		))
	}

	return block, nil
}

func (s *Database) blockUnavailableError(height BlockHeight, entry indexEntry, err error) error {
	s.log.Error("Indexed block data is unavailable",
		zap.Uint64("height", height),
		zap.Uint64("dataOffset", entry.Offset),
		zap.Uint32("indexedSize", entry.Size),
		zap.Error(err),
	)
	return fmt.Errorf("block at height %d is unavailable: %w", height, err)
}

// Has checks if a block exists at the given height.
func (s *Database) Has(height BlockHeight) (bool, error) {
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()

	if s.closed {
		s.log.Error("Failed Has: database closed", zap.Uint64("height", height))
		return false, database.ErrClosed
	}

	return s.hasWithoutLock(height)
}

func (s *Database) hasWithoutLock(height BlockHeight) (bool, error) {
	_, err := s.readBlockIndex(height)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) || errors.Is(err, ErrInvalidBlockHeight) {
			return false, nil
		}
		s.log.Error("Failed to check if block exists: failed to read index entry",
			zap.Uint64("height", height),
			zap.Error(err),
		)
		return false, err
	}
	return true, nil
}

func (s *Database) getDataFileIndexForHeight(height BlockHeight) (int, error) {
	entry, err := s.readBlockIndex(height)
	if err != nil {
		return 0, err
	}
	idx, _, err := s.dataFileIndexAndOffset(entry.Offset)
	if err != nil {
		return 0, fmt.Errorf("%w: calculating data file index for height %d: %w", ErrCorrupted, height, err)
	}
	return idx, nil
}

// Sync calls sync on all data files in the range [start, end],
// assuming data are written in-order. If no data exists at start or end,
// nothing is synced.
func (s *Database) Sync(start, end uint64) error {
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()

	if s.closed {
		s.log.Error("Failed Sync: database closed",
			zap.Uint64("start", start),
			zap.Uint64("end", end),
		)
		return database.ErrClosed
	}

	firstIdx, err := s.getDataFileIndexForHeight(start)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil
		}
		return err
	}
	lastIdx, err := s.getDataFileIndexForHeight(end)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil
		}
		return err
	}

	for idx := firstIdx; idx <= lastIdx; idx++ {
		f, err := s.getDataFile(idx, os.O_RDWR)
		if err != nil {
			return fmt.Errorf("failed to open data file %d: %w", idx, err)
		}
		if err := s.retryDataFileOperation(idx, f, (*os.File).Sync); err != nil {
			return fmt.Errorf("failed to sync data file %d: %w", idx, err)
		}
	}

	return nil
}

func (s *Database) indexEntryOffset(height BlockHeight) (uint64, error) {
	if height < s.header.MinHeight {
		return 0, fmt.Errorf("%w: block height %d is below minimum height %d", ErrInvalidBlockHeight, height, s.header.MinHeight)
	}

	relativeHeight := height - s.header.MinHeight
	offsetFromHeaderStart, err := safemath.Mul(relativeHeight, sizeOfIndexEntry)
	if err != nil {
		return 0, fmt.Errorf("%w: block height %d is too large to calculate its index file offset", ErrInvalidBlockHeight, height)
	}
	finalOffset, err := safemath.Add(sizeOfIndexFileHeader, offsetFromHeaderStart)
	if err != nil {
		return 0, fmt.Errorf("%w: block height %d is too large to calculate its index file offset", ErrInvalidBlockHeight, height)
	}

	return finalOffset, nil
}

// readIndexEntry reads the index entry for the given height from the index file.
// Returns database.ErrNotFound if the block does not exist.
func (s *Database) readIndexEntry(height BlockHeight) (indexEntry, error) {
	var entry indexEntry

	offset, err := s.indexEntryOffset(height)
	if err != nil {
		return entry, err
	}

	buf := make([]byte, sizeOfIndexEntry)
	_, err = s.indexFile.ReadAt(buf, int64(offset))
	if err != nil {
		// Return database.ErrNotFound if trying to read past the end of the index file
		// for a block that has not been indexed yet.
		if errors.Is(err, io.EOF) {
			return entry, fmt.Errorf("%w: block at height %d is not indexed", database.ErrNotFound, height)
		}
		return entry, fmt.Errorf("failed to read index entry at offset %d for height %d: %w", offset, height, err)
	}
	if err := entry.UnmarshalBinary(buf); err != nil {
		return entry, fmt.Errorf("failed to deserialize index entry for height %d: %w", height, err)
	}

	if entry.IsEmpty() {
		return entry, fmt.Errorf("%w: block at height %d is not indexed", database.ErrNotFound, height)
	}

	return entry, nil
}

func (s *Database) writeIndexEntryAt(indexFileOffset, dataFileBlockOffset uint64, blockDataLen uint32) error {
	indexEntry := indexEntry{
		Offset: dataFileBlockOffset,
		Size:   blockDataLen,
	}

	entryBytes, err := indexEntry.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to serialize index entry: %w", err)
	}

	if _, err := s.indexFile.WriteAt(entryBytes, int64(indexFileOffset)); err != nil {
		return fmt.Errorf("failed to write index entry: %w", err)
	}
	return nil
}

func (s *Database) persistIndexHeader() error {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()

	// Persist the physical extent, not the reservation frontier, because a failed
	// WriteAt may have created only part of its reserved record.
	files, maxIdx, err := s.listDataFiles()
	if err != nil {
		return fmt.Errorf("failed to list data files before checkpointing: %w", err)
	}
	dataEnd, err := s.calculatePhysicalDataEnd(files, maxIdx)
	if err != nil {
		return err
	}
	checkpoint := max(dataEnd, s.header.NextWriteOffset)

	// The index file must be fsync'd before the header is written to prevent
	// a state where the header is persisted but the index entries it refers to
	// are not. This could lead to data inconsistency on recovery.
	if s.config.SyncToDisk {
		if err := s.indexFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync index entries before checkpointing: %w", err)
		}
	}

	header := s.header

	// Update the header with the current state of the database.
	header.NextWriteOffset = checkpoint
	header.MaxHeight = s.maxBlockHeight.Load()
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint header: %w", err)
	}
	if _, err := s.indexFile.WriteAt(headerBytes, 0); err != nil {
		return fmt.Errorf("failed to write checkpoint header: %w", err)
	}
	if s.config.SyncToDisk {
		if err := s.indexFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync checkpoint header: %w", err)
		}
	}
	s.header.NextWriteOffset = checkpoint
	return nil
}

func (s *Database) calculatePhysicalDataEnd(files map[int]string, maxIdx int) (uint64, error) {
	if maxIdx < 0 {
		return 0, nil
	}
	path, ok := files[maxIdx]
	if !ok {
		return 0, fmt.Errorf("%w: data file at index %d is missing", ErrCorrupted, maxIdx)
	}
	return s.dataFileEnd(maxIdx, path)
}

func (s *Database) dataFileEnd(idx int, path string) (uint64, error) {
	fileOffset, err := safemath.Mul(uint64(idx), s.header.MaxDataFileSize)
	if err != nil {
		return 0, fmt.Errorf("%w: calculating data file %d offset would overflow: %w", ErrCorrupted, idx, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("failed to get stats for data file %d: %w", idx, err)
	}
	fileEnd, err := safemath.Add(fileOffset, uint64(info.Size()))
	if err != nil {
		return 0, fmt.Errorf("%w: calculating data file %d end would overflow: %w", ErrCorrupted, idx, err)
	}
	return fileEnd, nil
}

func (s *Database) dataFileEndForOffset(offset uint64) (uint64, error) {
	idx, _, err := s.dataFileIndexAndOffset(offset)
	if err != nil {
		return 0, fmt.Errorf("%w: calculating data file index for offset %d: %w", ErrCorrupted, offset, err)
	}
	fileEnd, err := s.dataFileEnd(idx, s.dataFilePath(idx))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("%w: indexed data file %d does not exist", ErrCorrupted, idx)
		}
		return 0, err
	}
	if offset >= fileEnd {
		return 0, fmt.Errorf("%w: index offset %d is outside data file %d", ErrCorrupted, offset, idx)
	}
	return fileEnd, nil
}

// recover detects and recovers unindexed blocks by scanning data files and updating the index.
// It compares the actual data file sizes on disk with the indexed data size to detect
// blocks that were written but not properly indexed.
// For each unindexed block found, it validates the block, then
// writes the corresponding index entry and updates block height tracking.
func (s *Database) recover() error {
	checkpoint := s.header.NextWriteOffset
	files, maxIdx, err := s.listDataFiles()
	if err != nil {
		return fmt.Errorf("failed to list data files for recovery: %w", err)
	}

	if len(files) == 0 {
		if checkpoint > 0 {
			return fmt.Errorf(
				"%w: index checkpoint is %d bytes, but no data files exist",
				ErrCorrupted,
				checkpoint,
			)
		}
		return nil
	}

	if s.header.MaxDataFileSize == math.MaxUint64 && len(files) > 1 {
		return fmt.Errorf("%w: only one data file expected when MaxDataFileSize is max uint64, got %d files with max index %d", ErrCorrupted, len(files), maxIdx)
	}

	// File presence and extent are cheap structural checks. Recovery does not
	// rescan record contents before the persisted checkpoint.
	for i := 0; i <= maxIdx; i++ {
		if _, ok := files[i]; !ok {
			return fmt.Errorf("%w: data file at index %d is missing", ErrCorrupted, i)
		}
	}

	dataEnd, err := s.calculatePhysicalDataEnd(files, maxIdx)
	if err != nil {
		return err
	}

	switch {
	case dataEnd == checkpoint:
		s.log.Debug("Recovery: data files match index header, no recovery needed.")
		return nil

	case dataEnd < checkpoint:
		return fmt.Errorf("%w: index checkpoint is ahead of physical data "+
			"(physical data end: %d bytes, checkpoint: %d bytes)",
			ErrCorrupted, dataEnd, checkpoint)
	default:
		// The data on disk is ahead of the index. We need to recover unindexed blocks.
		if err := s.recoverUnindexedBlocks(checkpoint, dataEnd); err != nil {
			return err
		}
	}
	return nil
}

// recoverUnindexedBlocks scans data written after the persisted checkpoint and rebuilds missing index entries.
func (s *Database) recoverUnindexedBlocks(startOffset, endOffset uint64) error {
	s.log.Info("Recovery: data files are ahead of index; recovering unindexed blocks.",
		zap.Uint64("startOffset", startOffset),
		zap.Uint64("endOffset", endOffset),
	)

	var (
		// Start at the persisted checkpoint, where the index was last synchronized.
		currentScanOffset   = startOffset
		numRecoveredHeights int
		currentFileIndex    = -1
		currentFileEnd      uint64
		badOffset           uint64
		badErr              error
	)
	for currentScanOffset < endOffset {
		idx, _, err := s.dataFileIndexAndOffset(currentScanOffset)
		if err != nil {
			return fmt.Errorf("recovery: %w: calculating data file index for offset %d: %w", ErrCorrupted, currentScanOffset, err)
		}
		if idx != currentFileIndex {
			currentFileIndex = idx
			fileEnd, err := s.dataFileEnd(currentFileIndex, s.dataFilePath(currentFileIndex))
			if err != nil {
				return fmt.Errorf("recovery: %w", err)
			}
			currentFileEnd = fileEnd
		}
		if currentScanOffset >= currentFileEnd {
			// A block that crosses a file boundary leaves this file's remaining range unused.
			nextIdx, err := safemath.Add(uint64(currentFileIndex), 1)
			if err != nil {
				return fmt.Errorf("recovery: overflow in file index calculation: %w", err)
			}
			if currentScanOffset, err = safemath.Mul(nextIdx, s.header.MaxDataFileSize); err != nil {
				return fmt.Errorf("recovery: overflow in scan offset calculation: %w", err)
			}
			continue
		}

		bh, err := s.recoverBlockAtOffset(currentScanOffset, currentFileEnd)
		if err != nil {
			if !errors.Is(err, ErrCorrupted) {
				return err
			}
			badOffset = currentScanOffset
			badErr = err
			// The checkpoint header can lag a later index entry, so preserve this suffix.
			currentScanOffset = endOffset
			break
		}
		s.log.Debug("Recovery: Successfully validated and indexed block",
			zap.Uint64("height", bh.Height),
			zap.Uint32("blockSize", bh.Size),
			zap.Uint64("dataOffset", currentScanOffset),
		)
		numRecoveredHeights++
		s.updateBlockMaxHeight(bh.Height)
		blockSize, err := safemath.Add(uint64(sizeOfBlockEntryHeader), uint64(bh.Size))
		if err != nil {
			return fmt.Errorf("recovery: overflow in block size calculation: %w", err)
		}
		currentScanOffset, err = safemath.Add(currentScanOffset, blockSize)
		if err != nil {
			return fmt.Errorf("recovery: overflow in scan offset calculation: %w", err)
		}
	}
	// Append after recovered data and any malformed suffix left as an orphan.
	s.nextDataReservationOffset.Store(currentScanOffset)

	if err := s.persistIndexHeader(); err != nil {
		return fmt.Errorf("recovery: failed to save index header after recovery scan: %w", err)
	}
	if badErr != nil {
		s.log.Warn("Recovery stopped at malformed data; remaining suffix left orphaned",
			zap.Uint64("dataOffset", badOffset),
			zap.Uint64("dataEnd", endOffset),
			zap.Error(badErr),
		)
	}

	maxHeight := s.maxBlockHeight.Load()
	s.log.Info("Recovery: Scan finished",
		zap.Int("recoveredBlocks", numRecoveredHeights),
		zap.Uint64("finalNextDataReservationOffset", s.nextDataReservationOffset.Load()),
		zap.Uint64("maxBlockHeight", maxHeight),
	)
	return nil
}

func (s *Database) recoverBlockAtOffset(offset, dataEnd uint64) (blockEntryHeader, error) {
	bh, _, err := s.readBlockAtOffset(offset, dataEnd, nil)
	if err != nil {
		return bh, err
	}
	indexOffset, err := s.indexEntryOffset(bh.Height)
	if err != nil {
		return bh, fmt.Errorf("%w: cannot get index offset for recovered block %d: %w", ErrCorrupted, bh.Height, err)
	}
	if err := s.writeIndexEntryAt(indexOffset, offset, bh.Size); err != nil {
		return bh, fmt.Errorf("failed to write index entry for recovered block %d: %w", bh.Height, err)
	}
	return bh, nil
}

func (s *Database) readBlockAtOffset(offset, dataEnd uint64, indexedSize *uint32) (blockEntryHeader, BlockData, error) {
	var bh blockEntryHeader
	if dataEnd-offset < uint64(sizeOfBlockEntryHeader) {
		return bh, nil, fmt.Errorf("%w: not enough data for block header at offset %d", ErrCorrupted, offset)
	}

	idx, localOffset, err := s.dataFileIndexAndOffset(offset)
	if err != nil {
		return bh, nil, fmt.Errorf("%w: calculating data file index for offset %d: %w", ErrCorrupted, offset, err)
	}
	f, err := s.getDataFile(idx, os.O_RDWR)
	if err != nil {
		return bh, nil, blockReadError("header", offset, err)
	}
	bhBuf := make([]byte, sizeOfBlockEntryHeader)
	if err := s.readDataFileAt(idx, f, bhBuf, int64(localOffset)); err != nil {
		return bh, nil, blockReadError("header", offset, err)
	}
	if err := bh.UnmarshalBinary(bhBuf); err != nil {
		return bh, nil, fmt.Errorf("%w: error deserializing block header at offset %d: %w", ErrCorrupted, offset, err)
	}
	if bh.Size == 0 {
		return bh, nil, fmt.Errorf("%w: invalid block size in header at offset %d: %d", ErrCorrupted, offset, bh.Size)
	}
	// Validate the indexed size before allocating based on the stored header.
	if indexedSize != nil && bh.Size != *indexedSize {
		return bh, nil, fmt.Errorf("%w: indexed block size %d does not match stored size %d", ErrCorrupted, *indexedSize, bh.Size)
	}
	if bh.Version > BlockEntryVersion {
		return bh, nil, fmt.Errorf("%w: invalid block entry version at offset %d, version %d is greater than the current version %d", ErrCorrupted, offset, bh.Version, BlockEntryVersion)
	}
	if bh.Height < s.header.MinHeight || bh.Height == unsetHeight {
		return bh, nil, fmt.Errorf(
			"%w: invalid block height in header at offset %d: found %d, expected >= %d",
			ErrCorrupted, offset, bh.Height, s.header.MinHeight,
		)
	}
	blockEnd, err := safemath.Add(offset, uint64(sizeOfBlockEntryHeader))
	if err != nil {
		return bh, nil, fmt.Errorf("%w: calculating block end offset would overflow at offset %d: %w", ErrCorrupted, offset, err)
	}
	blockEnd, err = safemath.Add(blockEnd, uint64(bh.Size))
	if err != nil {
		return bh, nil, fmt.Errorf("%w: calculating block end offset would overflow at offset %d: %w", ErrCorrupted, offset, err)
	}
	if blockEnd > dataEnd {
		return bh, nil, fmt.Errorf("%w: block data out of bounds at offset %d", ErrCorrupted, offset)
	}
	compressed := make([]byte, bh.Size)
	dataOffset, err := safemath.Add(localOffset, uint64(sizeOfBlockEntryHeader))
	if err != nil {
		return bh, nil, fmt.Errorf("%w: calculating block data offset would overflow at offset %d: %w", ErrCorrupted, offset, err)
	}
	if err := s.readDataFileAt(idx, f, compressed, int64(dataOffset)); err != nil {
		return bh, nil, blockReadError("data", offset, err)
	}
	block, err := s.compressor.Decompress(compressed)
	if err != nil {
		return bh, nil, fmt.Errorf("%w: failed to decompress block at offset %d: %w", ErrCorrupted, offset, err)
	}
	checksum := calculateChecksum(block)
	if checksum != bh.Checksum {
		return bh, nil, fmt.Errorf("%w: checksum mismatch for block at offset %d", ErrCorrupted, offset)
	}

	return bh, block, nil
}

func (s *Database) readDataFileAt(idx int, f *os.File, buf []byte, offset int64) error {
	return s.retryDataFileOperation(idx, f, func(f *os.File) error {
		_, err := f.ReadAt(buf, offset)
		return err
	})
}

func blockReadError(part string, offset uint64, err error) error {
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("%w: incomplete block %s at offset %d: %w", ErrCorrupted, part, offset, err)
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%w: data file for offset %d does not exist: %w", ErrCorrupted, offset, err)
	default:
		return fmt.Errorf("failed to read block %s at offset %d: %w", part, offset, err)
	}
}

func (s *Database) listDataFiles() (map[int]string, int, error) {
	files, err := os.ReadDir(s.config.DataDir)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to read data directory %s: %w", s.config.DataDir, err)
	}

	dataFiles := make(map[int]string)
	maxIndex := -1
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		var index int
		n, err := fmt.Sscanf(file.Name(), dataFileNameFormat, &index)
		if err != nil || n != 1 {
			s.log.Debug("non-data file found in data directory", zap.String("fileName", file.Name()), zap.Error(err))
			continue
		}
		dataFiles[index] = filepath.Join(s.config.DataDir, file.Name())
		if index > maxIndex {
			maxIndex = index
		}
	}

	return dataFiles, maxIndex, nil
}

func (s *Database) openAndInitializeIndex() error {
	indexPath := filepath.Join(s.config.IndexDir, indexFileName)
	openFlags := os.O_RDWR | os.O_CREATE
	var err error
	s.indexFile, err = os.OpenFile(indexPath, openFlags, defaultFilePermissions)
	if err != nil {
		return fmt.Errorf("failed to open index file %s: %w", indexPath, err)
	}
	if err := s.loadOrInitializeHeader(); err != nil {
		if closeErr := s.indexFile.Close(); closeErr != nil {
			return errors.Join(err, fmt.Errorf("failed to close index file after initialization failure: %w", closeErr))
		}
		return err
	}
	return nil
}

func (s *Database) loadOrInitializeHeader() error {
	fileInfo, err := s.indexFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get index file stats: %w", err)
	}

	// reset index file if its empty
	if fileInfo.Size() == 0 {
		s.log.Info("Index file is empty, writing initial index file header")
		s.header = indexFileHeader{
			Version:         IndexFileVersion,
			MinHeight:       s.config.MinimumHeight,
			MaxDataFileSize: s.config.MaxDataFileSize,
			MaxHeight:       unsetHeight,
			NextWriteOffset: 0,
		}
		s.maxBlockHeight.Store(unsetHeight)

		headerBytes, err := s.header.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to serialize new header: %w", err)
		}
		if uint64(len(headerBytes)) != sizeOfIndexFileHeader {
			return fmt.Errorf("internal error: serialized new header size %d, expected %d", len(headerBytes), sizeOfIndexFileHeader)
		}
		if _, err := s.indexFile.WriteAt(headerBytes, 0); err != nil {
			return fmt.Errorf("failed to write initial index header: %w", err)
		}

		return nil
	}

	headerBuf := make([]byte, sizeOfIndexFileHeader)
	_, readErr := s.indexFile.ReadAt(headerBuf, 0)
	if readErr != nil {
		return fmt.Errorf("failed to read index header (delete index file to reindex): %w", readErr)
	}
	if err := s.header.UnmarshalBinary(headerBuf); err != nil {
		return fmt.Errorf("failed to deserialize index header (delete index file to reindex): %w", err)
	}
	if s.header.Version != IndexFileVersion {
		return fmt.Errorf("mismatched index file version: found %d, expected %d", s.header.Version, IndexFileVersion)
	}
	s.nextDataReservationOffset.Store(s.header.NextWriteOffset)
	s.maxBlockHeight.Store(s.header.MaxHeight)
	return s.validateConfigMatchesHeader()
}

func (s *Database) validateConfigMatchesHeader() error {
	if s.config.MinimumHeight == s.header.MinHeight &&
		s.config.MaxDataFileSize == s.header.MaxDataFileSize {
		return nil
	}
	return fmt.Errorf(
		"%w: configured MinimumHeight=%d and MaxDataFileSize=%d bytes, persisted MinimumHeight=%d and MaxDataFileSize=%d bytes",
		errConfigMismatch,
		s.config.MinimumHeight,
		s.config.MaxDataFileSize,
		s.header.MinHeight,
		s.header.MaxDataFileSize,
	)
}

func (s *Database) closeFiles() {
	if s.indexFile != nil {
		s.indexFile.Close()
	}
	if s.fileCache != nil {
		// closes all data files
		s.fileCache.Flush()
	}
}

func (s *Database) dataFilePath(index int) string {
	return filepath.Join(s.config.DataDir, fmt.Sprintf(dataFileNameFormat, index))
}

func (s *Database) dataFileIndexAndOffset(offset uint64) (int, uint64, error) {
	maxFileSize := s.header.MaxDataFileSize
	idx := offset / maxFileSize
	if idx > math.MaxInt {
		return 0, 0, fmt.Errorf("data file index %d exceeds maximum %d: %w", idx, math.MaxInt, safemath.ErrOverflow)
	}
	return int(idx), offset % maxFileSize, nil
}

func (s *Database) getDataFile(idx, flags int) (*os.File, error) {
	if f, ok := s.fileCache.Get(idx); ok {
		return f, nil
	}

	// Prevent race conditions when multiple threads try to open the same file
	s.fileOpenMu.Lock()
	defer s.fileOpenMu.Unlock()

	// Double-check the cache after acquiring the lock
	if f, ok := s.fileCache.Get(idx); ok {
		return f, nil
	}

	path := s.dataFilePath(idx)
	f, err := os.OpenFile(path, flags, defaultFilePermissions)
	if err != nil {
		s.log.Error("Failed to open data file",
			zap.Int("fileIndex", idx),
			zap.String("filePath", path),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to open data file %s: %w", path, err)
	}
	s.fileCache.Put(idx, f)

	s.log.Debug("Opened data file",
		zap.Int("fileIndex", idx),
		zap.String("filePath", path),
	)

	return f, nil
}

func (s *Database) retryDataFileOperation(idx int, f *os.File, op func(*os.File) error) error {
	for {
		err := op(f)
		if !errors.Is(err, os.ErrClosed) {
			return err
		}

		s.fileOpenMu.Lock()
		cached, ok := s.fileCache.Get(idx)
		if ok && cached == f {
			s.fileCache.Evict(idx)
		}
		s.fileOpenMu.Unlock()

		f, err = s.getDataFile(idx, os.O_RDWR)
		if err != nil {
			return err
		}
	}
}

func calculateChecksum(data []byte) uint64 {
	return xxhash.Sum64(data)
}

func (s *Database) writeBlockAt(offset uint64, bh blockEntryHeader, block BlockData) error {
	header, err := bh.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to serialize block header: %w", err)
	}

	// Allocate combined buffer for header and block data and write it to the data file
	bufSize, err := safemath.Add(uint64(sizeOfBlockEntryHeader), uint64(len(block)))
	if err != nil {
		return fmt.Errorf("calculating combined buffer size would overflow for block %d: %w", bh.Height, err)
	}
	buf := make([]byte, bufSize)
	copy(buf, header)
	copy(buf[sizeOfBlockEntryHeader:], block)

	idx, localOffset, err := s.dataFileIndexAndOffset(offset)
	if err != nil {
		return fmt.Errorf("failed to get data file index for writing block %d: %w", bh.Height, err)
	}
	f, err := s.getDataFile(idx, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return fmt.Errorf("failed to get data file for writing block %d: %w", bh.Height, err)
	}
	if err := s.retryDataFileOperation(idx, f, func(f *os.File) error {
		if _, err := f.WriteAt(buf, int64(localOffset)); err != nil {
			return fmt.Errorf("failed to write block data: %w", err)
		}
		if s.config.SyncToDisk {
			if err := f.Sync(); err != nil {
				return fmt.Errorf("failed to sync data file: %w", err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to write block to data file at offset %d: %w", offset, err)
	}
	return nil
}

func (s *Database) updateBlockMaxHeight(height BlockHeight) {
	for {
		maxHeight := s.maxBlockHeight.Load()
		if height <= maxHeight && maxHeight != unsetHeight {
			return
		}
		if s.maxBlockHeight.CompareAndSwap(maxHeight, height) {
			return
		}
		// If CAS failed, retry with the new max height
	}
}

type dataReservation struct {
	writeOffset    uint64
	endOffset      uint64
	previousOffset uint64
}

// allocateBlockSpace atomically reserves space for a block, skipping to the
// next data file when the block would cross a file boundary.
func (s *Database) allocateBlockSpace(totalSize uint32) (dataReservation, error) {
	maxDataFileSize := s.header.MaxDataFileSize

	// Check if a single block would exceed the max data file size
	if uint64(totalSize) > maxDataFileSize {
		return dataReservation{}, fmt.Errorf("%w: block of size %d exceeds max data file size of %d", ErrBlockTooLarge, totalSize, maxDataFileSize)
	}

	for {
		currentOffset := s.nextDataReservationOffset.Load()

		// Calculate where this block would end if written at current offset
		blockEndOffset, err := safemath.Add(currentOffset, uint64(totalSize))
		if err != nil {
			return dataReservation{}, fmt.Errorf(
				"adding block of size %d to offset %d would overflow uint64 data file pointer: %w",
				totalSize, currentOffset, err,
			)
		}

		// Determine the actual write offset for this block, taking into account
		// data file splitting when max data file size is reached.
		actualWriteOffset := currentOffset
		actualBlockEndOffset := blockEndOffset

		// If we have a max file size, check if we need to start a new file
		if maxDataFileSize > 0 {
			idx, localOffset, err := s.dataFileIndexAndOffset(currentOffset)
			if err != nil {
				return dataReservation{}, fmt.Errorf("calculating data file index for offset %d: %w", currentOffset, err)
			}

			// Check if this block would span across file boundaries
			localEnd, err := safemath.Add(localOffset, uint64(totalSize))
			if err != nil {
				return dataReservation{}, fmt.Errorf(
					"calculating block end within file would overflow: %w",
					err,
				)
			}
			if localEnd > maxDataFileSize {
				// Advance the current write offset to the start of the next file since
				// it would exceed the current file size.
				nextIdx, err := safemath.Add(uint64(idx), 1)
				if err != nil {
					return dataReservation{}, fmt.Errorf("calculating next data file index would overflow: %w", err)
				}
				if nextIdx > math.MaxInt {
					return dataReservation{}, fmt.Errorf("calculating next data file index would overflow: %w", safemath.ErrOverflow)
				}
				nextFileOffset, err := safemath.Mul(nextIdx, maxDataFileSize)
				if err != nil {
					return dataReservation{}, fmt.Errorf(
						"calculating next file offset would overflow: %w",
						err,
					)
				}
				actualWriteOffset = nextFileOffset

				// Recalculate the end offset for the block space to set the next write offset
				if actualBlockEndOffset, err = safemath.Add(actualWriteOffset, uint64(totalSize)); err != nil {
					return dataReservation{}, fmt.Errorf(
						"adding block of size %d to new file offset %d would overflow: %w",
						totalSize, actualWriteOffset, err,
					)
				}
			}
		}

		if s.nextDataReservationOffset.CompareAndSwap(currentOffset, actualBlockEndOffset) {
			return dataReservation{
				writeOffset:    actualWriteOffset,
				endOffset:      actualBlockEndOffset,
				previousOffset: currentOffset,
			}, nil
		}
	}
}
