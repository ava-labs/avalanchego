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

	// maxBlockHeight tracks the highest block height written
	maxBlockHeight atomic.Uint64
	// nextDataWriteOffset tracks the next position to write new data in the data file.
	nextDataWriteOffset atomic.Uint64
	// headerWriteOccupied prevents concurrent writes to the index header
	headerWriteOccupied atomic.Bool
}

// New creates a block database.
// Parameters:
//   - config: Configuration parameters
//   - log: Logger instance for structured logging
func New(config DatabaseConfig, log logging.Logger) (database.HeightIndex, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	databaseLog := log
	if databaseLog == nil {
		databaseLog = logging.NoLog{}
	}

	// from benchmarks, zstd.BestSpeed is about 100% faster than the default
	// compression level while giving us ~5% better compression ratio than Snappy.
	var err error
	compressor, err := compression.NewZstdCompressorWithLevel(math.MaxUint32, zstd.BestSpeed)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize compressor: %w", err)
	}

	s := &Database{
		config: config,
		log:    databaseLog,
		fileCache: lru.NewCacheWithOnEvict(config.MaxDataFiles, func(_ int, f *os.File) {
			if f != nil {
				f.Close()
			}
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

	if err := s.openAndInitializeIndex(); err != nil {
		s.log.Error("Failed to initialize database: failed to initialize index", zap.Error(err))
		return nil, err
	}

	if err := s.initializeDataFiles(); err != nil {
		s.log.Error("Failed to initialize database: failed to initialize data files", zap.Error(err))
		s.closeFiles()
		return nil, err
	}

	if err := s.recover(); err != nil {
		s.log.Error("Failed to initialize database: recovery failed", zap.Error(err))
		s.closeFiles()
		return nil, fmt.Errorf("recovery failed: %w", err)
	}

	maxHeight := s.maxBlockHeight.Load()
	s.log.Info("BlockDB initialized successfully",
		zap.Uint64("nextWriteOffset", s.nextDataWriteOffset.Load()),
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
		s.log.Error("Failed to close database: failed to persist index header", zap.Error(err))
	}

	s.closeFiles()

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
	writeDataOffset, err := s.allocateBlockSpace(sizeWithDataHeader)
	if err != nil {
		s.log.Error("Failed to write block: failed to allocate block space",
			zap.Uint64("height", height),
			zap.Uint32("totalSize", sizeWithDataHeader),
			zap.Error(err),
		)
		return err
	}

	bh := blockEntryHeader{
		Height:   height,
		Size:     blockDataLen,
		Checksum: calculateChecksum(block),
		Version:  BlockEntryVersion,
	}
	if err := s.writeBlockAt(writeDataOffset, bh, blockToWrite); err != nil {
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

	if err := s.updateBlockMaxHeight(height); err != nil {
		s.log.Error("Failed to write block: error updating max block height",
			zap.Uint64("height", height),
			zap.Error(err),
		)
		return err
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

	totalReadSize, err := safemath.Add(uint64(sizeOfBlockEntryHeader), uint64(indexEntry.Size))
	if err != nil {
		return nil, fmt.Errorf("failed to compute total read size: %w", err)
	}
	buf := make([]byte, int(totalReadSize))

	// loop to retry fetching the data file if it got closed between get and read.
	// If not closed, we read the block header and data.
	for {
		dataFile, localOffset, fileIndex, err := s.getDataFileAndOffset(indexEntry.Offset)
		if err != nil {
			return nil, fmt.Errorf("failed to get data file and offset: %w", err)
		}
		if _, err := dataFile.ReadAt(buf, int64(localOffset)); err != nil {
			if errors.Is(err, os.ErrClosed) {
				s.fileCache.Evict(fileIndex)
				continue
			}
			s.log.Error("Failed to read block: failed to read block data from file",
				zap.Uint64("height", height),
				zap.Uint64("localOffset", localOffset),
				zap.Uint32("blockSize", indexEntry.Size),
				zap.Error(err),
			)
			return nil, fmt.Errorf("failed to read block header and data: %w", err)
		}
		break
	}

	var bh blockEntryHeader
	if err := bh.UnmarshalBinary(buf[:int(sizeOfBlockEntryHeader)]); err != nil {
		return nil, fmt.Errorf("failed to deserialize block header: %w", err)
	}
	compressedData := buf[int(sizeOfBlockEntryHeader):]
	decompressed, err := s.compressor.Decompress(compressedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress block data: %w", err)
	}

	// Verify checksum on uncompressed data
	calculatedChecksum := calculateChecksum(decompressed)
	if calculatedChecksum != bh.Checksum {
		return nil, fmt.Errorf("checksum mismatch: calculated %d, stored %d", calculatedChecksum, bh.Checksum)
	}

	return decompressed, nil
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
	_, _, idx, err := s.getDataFileAndOffset(entry.Offset)
	if err != nil {
		return 0, fmt.Errorf("failed to get data file index for height %d: %w", height, err)
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
		f, err := s.getOrOpenDataFile(idx)
		if err != nil {
			return fmt.Errorf("failed to open data file %d: %w", idx, err)
		}
		if err := f.Sync(); err != nil {
			return fmt.Errorf("failed to sync data file %d: %w", idx, err)
		}
	}

	return nil
}

func (s *Database) indexEntryOffset(height BlockHeight) (uint64, error) {
	if height < s.header.MinHeight {
		return 0, fmt.Errorf("%w: failed to get index entry offset for block at height %d, minimum height is %d", ErrInvalidBlockHeight, height, s.header.MinHeight)
	}

	relativeHeight := height - s.header.MinHeight
	offsetFromHeaderStart, err := safemath.Mul(relativeHeight, sizeOfIndexEntry)
	if err != nil {
		return 0, fmt.Errorf("%w: block height %d is too large", ErrInvalidBlockHeight, height)
	}
	finalOffset, err := safemath.Add(sizeOfIndexFileHeader, offsetFromHeaderStart)
	if err != nil {
		return 0, fmt.Errorf("%w: block height %d is too large", ErrInvalidBlockHeight, height)
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
			return entry, fmt.Errorf("%w: EOF reading index entry at offset %d for height %d", database.ErrNotFound, offset, height)
		}
		return entry, fmt.Errorf("failed to read index entry at offset %d for height %d: %w", offset, height, err)
	}
	if err := entry.UnmarshalBinary(buf); err != nil {
		return entry, fmt.Errorf("failed to deserialize index entry for height %d: %w", height, err)
	}

	if entry.IsEmpty() {
		return entry, fmt.Errorf("%w: empty index entry for height %d", database.ErrNotFound, height)
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
	if s.headerWriteOccupied.CompareAndSwap(false, true) {
		defer s.headerWriteOccupied.Store(false)
		return s.persistIndexHeaderInternal()
	}
	s.log.Info("Skipping persistIndexHeader due to concurrent header write")
	return nil
}

func (s *Database) persistIndexHeaderInternal() error {
	// The index file must be fsync'd before the header is written to prevent
	// a state where the header is persisted but the index entries it refers to
	// are not. This could lead to data inconsistency on recovery.
	if s.config.SyncToDisk {
		if err := s.indexFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync index file before writing header state: %w", err)
		}
	}

	header := s.header

	// Update the header with the current state of the database.
	header.NextWriteOffset = s.nextDataWriteOffset.Load()
	header.MaxHeight = s.maxBlockHeight.Load()
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to serialize header for writing state: %w", err)
	}
	if _, err := s.indexFile.WriteAt(headerBytes, 0); err != nil {
		return fmt.Errorf("failed to write header state to index file: %w", err)
	}
	return nil
}

// recover detects and recovers unindexed blocks by scanning data files and updating the index.
// It compares the actual data file sizes on disk with the indexed data size to detect
// blocks that were written but not properly indexed.
// For each unindexed block found, it validates the block, then
// writes the corresponding index entry and updates block height tracking.
func (s *Database) recover() error {
	dataFiles, maxIndex, err := s.listDataFiles()
	if err != nil {
		return fmt.Errorf("failed to list data files for recovery: %w", err)
	}

	if len(dataFiles) == 0 {
		return nil
	}

	if s.header.MaxDataFileSize == math.MaxUint64 && len(dataFiles) > 1 {
		return fmt.Errorf("%w: only one data file expected when MaxDataFileSize is max uint64, got %d files with max index %d", ErrCorrupted, len(dataFiles), maxIndex)
	}

	// ensure no data files are missing
	// If any data files are missing, we would need to recalculate the max height.
	// This can be supported in the future but for now to keep things simple,
	// we will just error if the data files are not as expected.
	for i := 0; i <= maxIndex; i++ {
		if _, exists := dataFiles[i]; !exists {
			return fmt.Errorf("%w: data file at index %d is missing", ErrCorrupted, i)
		}
	}

	// Calculate the expected next write offset based on the data on disk.
	var calculatedNextDataWriteOffset uint64
	fileSizeContribution, err := safemath.Mul(uint64(maxIndex), s.header.MaxDataFileSize)
	if err != nil {
		return fmt.Errorf("calculating file size contribution would overflow: %w", err)
	}
	calculatedNextDataWriteOffset = fileSizeContribution

	lastFileInfo, err := os.Stat(dataFiles[maxIndex])
	if err != nil {
		return fmt.Errorf("failed to get stats for last data file %s: %w", dataFiles[maxIndex], err)
	}
	calculatedNextDataWriteOffset, err = safemath.Add(calculatedNextDataWriteOffset, uint64(lastFileInfo.Size()))
	if err != nil {
		return fmt.Errorf("adding last file size would overflow: %w", err)
	}

	nextDataWriteOffset := s.nextDataWriteOffset.Load()
	switch {
	case calculatedNextDataWriteOffset == nextDataWriteOffset:
		s.log.Debug("Recovery: data files match index header, no recovery needed.")
		return nil

	case calculatedNextDataWriteOffset < nextDataWriteOffset:
		// this happens when the index claims to have more data than is actually on disk
		return fmt.Errorf("%w: index header claims to have more data than is actually on disk "+
			"(calculated: %d bytes, index header: %d bytes)",
			ErrCorrupted, calculatedNextDataWriteOffset, nextDataWriteOffset)
	default:
		// The data on disk is ahead of the index. We need to recover unindexed blocks.
		if err := s.recoverUnindexedBlocks(nextDataWriteOffset, calculatedNextDataWriteOffset); err != nil {
			return err
		}
	}
	return nil
}

// recoverUnindexedBlocks scans data files from the given offset and recovers blocks that were written but not indexed.
func (s *Database) recoverUnindexedBlocks(startOffset, endOffset uint64) error {
	s.log.Info("Recovery: data files are ahead of index; recovering unindexed blocks.",
		zap.Uint64("startOffset", startOffset),
		zap.Uint64("endOffset", endOffset),
	)

	var (
		// Start scan from where the index left off.
		currentScanOffset   = startOffset
		numRecoveredHeights int
		maxRecoveredHeight  BlockHeight
	)
	for currentScanOffset < endOffset {
		bh, err := s.recoverBlockAtOffset(currentScanOffset, endOffset)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Reached end of this file, try to read the next file
				currentFileIndex := int(currentScanOffset / s.header.MaxDataFileSize)
				nextFileIndex, err := safemath.Add(uint64(currentFileIndex), 1)
				if err != nil {
					return fmt.Errorf("recovery: overflow in file index calculation: %w", err)
				}
				if currentScanOffset, err = safemath.Mul(nextFileIndex, s.header.MaxDataFileSize); err != nil {
					return fmt.Errorf("recovery: overflow in scan offset calculation: %w", err)
				}
				continue
			}
			return err
		}
		s.log.Debug("Recovery: Successfully validated and indexed block",
			zap.Uint64("height", bh.Height),
			zap.Uint32("blockSize", bh.Size),
			zap.Uint64("dataOffset", currentScanOffset),
		)
		numRecoveredHeights++
		maxRecoveredHeight = max(maxRecoveredHeight, bh.Height)
		blockTotalSize, err := safemath.Add(uint64(sizeOfBlockEntryHeader), uint64(bh.Size))
		if err != nil {
			return fmt.Errorf("recovery: overflow in block size calculation: %w", err)
		}
		currentScanOffset, err = safemath.Add(currentScanOffset, blockTotalSize)
		if err != nil {
			return fmt.Errorf("recovery: overflow in scan offset calculation: %w", err)
		}
	}
	s.nextDataWriteOffset.Store(currentScanOffset)

	// Update the max block height if max recovered height is greater than
	// the current max height.
	if numRecoveredHeights > 0 {
		currentMaxHeight := s.maxBlockHeight.Load()
		if maxRecoveredHeight > currentMaxHeight || currentMaxHeight == unsetHeight {
			s.maxBlockHeight.Store(maxRecoveredHeight)
		}
	}

	if err := s.persistIndexHeader(); err != nil {
		return fmt.Errorf("recovery: failed to save index header after recovery scan: %w", err)
	}

	maxHeight := s.maxBlockHeight.Load()
	s.log.Info("Recovery: Scan finished",
		zap.Int("recoveredBlocks", numRecoveredHeights),
		zap.Uint64("finalNextWriteOffset", s.nextDataWriteOffset.Load()),
		zap.Uint64("maxBlockHeight", maxHeight),
	)
	return nil
}

func (s *Database) recoverBlockAtOffset(offset, totalDataSize uint64) (blockEntryHeader, error) {
	var bh blockEntryHeader
	if totalDataSize-offset < uint64(sizeOfBlockEntryHeader) {
		return bh, fmt.Errorf("%w: not enough data for block header at offset %d", ErrCorrupted, offset)
	}

	dataFile, localOffset, _, err := s.getDataFileAndOffset(offset)
	if err != nil {
		return bh, fmt.Errorf("recovery: failed to get data file for offset %d: %w", offset, err)
	}
	bhBuf := make([]byte, sizeOfBlockEntryHeader)
	if _, err := dataFile.ReadAt(bhBuf, int64(localOffset)); err != nil {
		return bh, fmt.Errorf("%w: error reading block header at offset %d: %w", ErrCorrupted, offset, err)
	}
	if err := bh.UnmarshalBinary(bhBuf); err != nil {
		return bh, fmt.Errorf("%w: error deserializing block header at offset %d: %w", ErrCorrupted, offset, err)
	}
	if bh.Size == 0 {
		return bh, fmt.Errorf("%w: invalid block size in header at offset %d: %d", ErrCorrupted, offset, bh.Size)
	}
	if bh.Version > BlockEntryVersion {
		return bh, fmt.Errorf("%w: invalid block entry version at offset %d, version %d is greater than the current version %d", ErrCorrupted, offset, bh.Version, BlockEntryVersion)
	}
	if bh.Height < s.header.MinHeight || bh.Height == unsetHeight {
		return bh, fmt.Errorf(
			"%w: invalid block height in header at offset %d: found %d, expected >= %d",
			ErrCorrupted, offset, bh.Height, s.header.MinHeight,
		)
	}
	expectedBlockEndOffset, err := safemath.Add(offset, uint64(sizeOfBlockEntryHeader))
	if err != nil {
		return bh, fmt.Errorf("calculating block end offset would overflow at offset %d: %w", offset, err)
	}
	expectedBlockEndOffset, err = safemath.Add(expectedBlockEndOffset, uint64(bh.Size))
	if err != nil {
		return bh, fmt.Errorf("calculating block end offset would overflow at offset %d: %w", offset, err)
	}
	if expectedBlockEndOffset > totalDataSize {
		return bh, fmt.Errorf("%w: block data out of bounds at offset %d", ErrCorrupted, offset)
	}
	blockData := make([]byte, bh.Size)
	blockDataOffset, err := safemath.Add(localOffset, uint64(sizeOfBlockEntryHeader))
	if err != nil {
		return bh, fmt.Errorf("calculating block data offset would overflow at offset %d: %w", offset, err)
	}
	if _, err := dataFile.ReadAt(blockData, int64(blockDataOffset)); err != nil {
		return bh, fmt.Errorf("%w: failed to read block data at offset %d: %w", ErrCorrupted, offset, err)
	}
	// Decompress block data and verify checksum
	decompressed, err := s.compressor.Decompress(blockData)
	if err != nil {
		return bh, fmt.Errorf("%w: failed to decompress block at offset %d: %w", ErrCorrupted, offset, err)
	}
	calculatedChecksum := calculateChecksum(decompressed)
	if calculatedChecksum != bh.Checksum {
		return bh, fmt.Errorf("%w: checksum mismatch for block at offset %d", ErrCorrupted, offset)
	}

	// Write index entry for this block
	indexFileOffset, idxErr := s.indexEntryOffset(bh.Height)
	if idxErr != nil {
		return bh, fmt.Errorf("cannot get index offset for recovered block %d: %w", bh.Height, idxErr)
	}
	if err := s.writeIndexEntryAt(indexFileOffset, offset, bh.Size); err != nil {
		return bh, fmt.Errorf("failed to update index for recovered block %d: %w", bh.Height, err)
	}
	return bh, nil
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
	if err := os.MkdirAll(s.config.IndexDir, 0o755); err != nil {
		return fmt.Errorf("failed to create index directory %s: %w", s.config.IndexDir, err)
	}
	openFlags := os.O_RDWR | os.O_CREATE
	var err error
	s.indexFile, err = os.OpenFile(indexPath, openFlags, defaultFilePermissions)
	if err != nil {
		return fmt.Errorf("failed to open index file %s: %w", indexPath, err)
	}
	return s.loadOrInitializeHeader()
}

func (s *Database) initializeDataFiles() error {
	if err := os.MkdirAll(s.config.DataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data directory %s: %w", s.config.DataDir, err)
	}

	// Pre-load the data file for the next write offset.
	nextOffset := s.nextDataWriteOffset.Load()
	if nextOffset > 0 {
		_, _, _, err := s.getDataFileAndOffset(nextOffset)
		if err != nil {
			return fmt.Errorf("failed to pre-load data file for offset %d: %w", nextOffset, err)
		}
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
	s.nextDataWriteOffset.Store(s.header.NextWriteOffset)
	s.maxBlockHeight.Store(s.header.MaxHeight)
	s.logConfigAndHeaderMismatches()

	return nil
}

func (s *Database) logConfigAndHeaderMismatches() {
	// Some config values cannot be changed after index initialization.
	// If they do not match the index header, log an info that
	// the index header values will be used instead.
	if s.config.MinimumHeight != s.header.MinHeight {
		s.log.Info(
			"MinimumHeight in config does not match the index header. The MinimumHeight in the index header will be used.",
			zap.Uint64("configMinimumHeight", s.config.MinimumHeight),
			zap.Uint64("headerMinimumHeight", s.header.MinHeight),
		)
	}
	if s.config.MaxDataFileSize != s.header.MaxDataFileSize {
		s.log.Info(
			"MaxDataFileSize in config does not match the index header. The MaxDataFileSize in the index header will be used.",
			zap.Uint64("configMaxDataFileSize", s.config.MaxDataFileSize),
			zap.Uint64("headerMaxDataFileSize", s.header.MaxDataFileSize),
		)
	}
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

func (s *Database) getOrOpenDataFile(fileIndex int) (*os.File, error) {
	if handle, ok := s.fileCache.Get(fileIndex); ok {
		return handle, nil
	}

	// Prevent race conditions when multiple threads try to open the same file
	s.fileOpenMu.Lock()
	defer s.fileOpenMu.Unlock()

	// Double-check the cache after acquiring the lock
	if handle, ok := s.fileCache.Get(fileIndex); ok {
		return handle, nil
	}

	filePath := s.dataFilePath(fileIndex)
	handle, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, defaultFilePermissions)
	if err != nil {
		s.log.Error("Failed to open data file",
			zap.Int("fileIndex", fileIndex),
			zap.String("filePath", filePath),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to open data file %s: %w", filePath, err)
	}
	s.fileCache.Put(fileIndex, handle)

	s.log.Debug("Opened data file",
		zap.Int("fileIndex", fileIndex),
		zap.String("filePath", filePath),
	)

	return handle, nil
}

func calculateChecksum(data []byte) uint64 {
	return xxhash.Sum64(data)
}

func (s *Database) writeBlockAt(offset uint64, bh blockEntryHeader, block BlockData) error {
	headerBytes, err := bh.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to serialize block header: %w", err)
	}

	// Allocate combined buffer for header and block data and write it to the data file
	combinedBufSize, err := safemath.Add(uint64(sizeOfBlockEntryHeader), uint64(len(block)))
	if err != nil {
		return fmt.Errorf("calculating combined buffer size would overflow for block %d: %w", bh.Height, err)
	}
	combinedBuf := make([]byte, combinedBufSize)
	copy(combinedBuf, headerBytes)
	copy(combinedBuf[sizeOfBlockEntryHeader:], block)

	// loop to retry fetching the data file if it got closed between get and write.
	// If not closed, we write the block and return.
	for {
		dataFile, localOffset, fileIndex, err := s.getDataFileAndOffset(offset)
		if err != nil {
			return fmt.Errorf("failed to get data file for writing block %d: %w", bh.Height, err)
		}

		if _, err := dataFile.WriteAt(combinedBuf, int64(localOffset)); err != nil {
			if errors.Is(err, os.ErrClosed) {
				// ensure the file is evicted, otherwise we'll retry forever
				s.fileCache.Evict(fileIndex)
				continue
			}
			return fmt.Errorf("failed to write block to data file at offset %d: %w", offset, err)
		}

		if s.config.SyncToDisk {
			if err := dataFile.Sync(); err != nil {
				if errors.Is(err, os.ErrClosed) {
					s.fileCache.Evict(fileIndex)
					continue
				}
				return fmt.Errorf("failed to sync data file after writing block %d: %w", bh.Height, err)
			}
		}
		return nil
	}
}

func (s *Database) updateBlockMaxHeight(writtenBlockHeight BlockHeight) error {
	for {
		maxHeight := s.maxBlockHeight.Load()
		if writtenBlockHeight <= maxHeight && maxHeight != unsetHeight {
			break
		}
		if s.maxBlockHeight.CompareAndSwap(maxHeight, writtenBlockHeight) {
			break
		}
		// If CAS failed, retry with the new max height
	}

	// Check if we need to persist header on checkpoint interval
	if writtenBlockHeight%s.config.CheckpointInterval == 0 {
		if err := s.persistIndexHeader(); err != nil {
			return fmt.Errorf("block %d written, but checkpoint failed: %w", writtenBlockHeight, err)
		}
	}

	return nil
}

// allocateBlockSpace reserves space for a block and returns the data file offset where it should be written.
//
// This function atomically reserves space by updating the nextWriteOffset and handles
// file splitting by advancing the nextWriteOffset when a data file would be exceeded.
//
// Parameters:
//   - totalSize: The total size in bytes needed for the block
//
// Returns:
//   - writeDataOffset: The data file offset where the block should be written
//   - err: Error if allocation fails (e.g., block too large, overflow, etc.)
func (s *Database) allocateBlockSpace(totalSize uint32) (writeDataOffset uint64, err error) {
	maxDataFileSize := s.header.MaxDataFileSize

	// Check if a single block would exceed the max data file size
	if uint64(totalSize) > maxDataFileSize {
		return 0, fmt.Errorf("%w: block of size %d exceeds max data file size of %d", ErrBlockTooLarge, totalSize, maxDataFileSize)
	}

	for {
		currentOffset := s.nextDataWriteOffset.Load()

		// Calculate where this block would end if written at current offset
		blockEndOffset, err := safemath.Add(currentOffset, uint64(totalSize))
		if err != nil {
			return 0, fmt.Errorf(
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
			currentFileIndex := int(currentOffset / maxDataFileSize)
			offsetWithinCurrentFile := currentOffset % maxDataFileSize

			// Check if this block would span across file boundaries
			blockEndWithinFile, err := safemath.Add(offsetWithinCurrentFile, uint64(totalSize))
			if err != nil {
				return 0, fmt.Errorf(
					"calculating block end within file would overflow: %w",
					err,
				)
			}
			if blockEndWithinFile > maxDataFileSize {
				// Advance the current write offset to the start of the next file since
				// it would exceed the current file size.
				nextFileStartOffset, err := safemath.Mul(uint64(currentFileIndex+1), maxDataFileSize)
				if err != nil {
					return 0, fmt.Errorf(
						"calculating next file offset would overflow: %w",
						err,
					)
				}
				actualWriteOffset = nextFileStartOffset

				// Recalculate the end offset for the block space to set the next write offset
				if actualBlockEndOffset, err = safemath.Add(actualWriteOffset, uint64(totalSize)); err != nil {
					return 0, fmt.Errorf(
						"adding block of size %d to new file offset %d would overflow: %w",
						totalSize, actualWriteOffset, err,
					)
				}
			}
		}

		if s.nextDataWriteOffset.CompareAndSwap(currentOffset, actualBlockEndOffset) {
			return actualWriteOffset, nil
		}
	}
}

func (s *Database) getDataFileAndOffset(globalOffset uint64) (*os.File, uint64, int, error) {
	maxFileSize := s.header.MaxDataFileSize
	fileIndex := int(globalOffset / maxFileSize)
	localOffset := globalOffset % maxFileSize
	handle, err := s.getOrOpenDataFile(fileIndex)
	return handle, localOffset, fileIndex, err
}
