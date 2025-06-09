package blockdb

import (
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/cespare/xxhash/v2"
)

const MaxBlockDataSize = 1 << 30 // 1 GB

var (
	_ encoding.BinaryMarshaler   = blockHeader{}
	_ encoding.BinaryUnmarshaler = &blockHeader{}

	sizeOfBlockHeader = uint64(binary.Size(blockHeader{}))
)

// blockHeader is prepended to each block in the data file.
type blockHeader struct {
	Height uint64 // todo: can this be omitted? currently only used for verification
	// Size of the raw block data (excluding this blockHeader).
	Size     uint64
	Checksum uint64
}

// MarshalBinary implements the encoding.BinaryMarshaler interface.
func (bh blockHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, sizeOfBlockHeader)
	binary.LittleEndian.PutUint64(buf[0:], bh.Height)
	binary.LittleEndian.PutUint64(buf[8:], bh.Size)
	binary.LittleEndian.PutUint64(buf[16:], bh.Checksum)
	return buf, nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
func (bh *blockHeader) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfBlockHeader) {
		return fmt.Errorf("incorrect data length to unmarshal blockHeader: got %d bytes, need exactly %d", len(data), sizeOfBlockHeader)
	}
	bh.Height = binary.LittleEndian.Uint64(data[0:])
	bh.Size = binary.LittleEndian.Uint64(data[8:])
	bh.Checksum = binary.LittleEndian.Uint64(data[16:])
	return nil
}

// WriteBlock inserts a block into the store at the given height.
// Returns an error if the store is closed, the block is empty, or the write fails.
func (s *Database) WriteBlock(height BlockHeight, block Block) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrDatabaseClosed
	}

	if len(block) == 0 {
		return ErrBlockEmpty
	}

	if len(block) > MaxBlockDataSize {
		return ErrBlockTooLarge
	}

	indexFileOffset, err := s.indexEntryOffset(height)
	if err != nil {
		return err
	}

	blockDataLen := uint64(len(block))
	sizeWithDataHeader := sizeOfBlockHeader + blockDataLen
	writeDataOffset, err := s.allocateBlockSpace(sizeWithDataHeader)
	if err != nil {
		return err
	}

	bh := blockHeader{
		Height:   height,
		Size:     uint64(len(block)),
		Checksum: calculateChecksum(block),
	}
	if err := s.writeBlockAtOffset(writeDataOffset, bh, block); err != nil {
		return err
	}

	if err := s.writeIndexEntryAt(indexFileOffset, writeDataOffset, blockDataLen); err != nil {
		return err
	}

	return s.updateBlockHeights(height)
}

// ReadBlock retrieves a block by its height.
// Returns the block data or an error if not found or block data is corrupted.
func (s *Database) ReadBlock(height BlockHeight) (Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrDatabaseClosed
	}

	indexEntry, err := s.readIndexEntry(height)
	if err != nil {
		if errors.Is(err, ErrInvalidBlockHeight) {
			return nil, ErrBlockNotFound
		}
		return nil, fmt.Errorf("failed to prepare for reading index entry for height %d: %w", height, err)
	}
	if indexEntry.IsEmpty() {
		return nil, ErrBlockNotFound
	}

	bh, err := s.readAndVerifyBlockHeader(indexEntry, height)
	if err != nil {
		return nil, err
	}

	return s.readAndVerifyBlockData(indexEntry, bh)
}

func (s *Database) readAndVerifyBlockHeader(indexEntry IndexEntry, expectedHeight BlockHeight) (blockHeader, error) {
	var bh blockHeader
	dataHeaderBuf := make([]byte, sizeOfBlockHeader)
	_, err := s.dataFile.ReadAt(dataHeaderBuf, int64(indexEntry.Offset))
	if err != nil {
		return bh, fmt.Errorf("failed to read block header from data file for height %d: %w", expectedHeight, err)
	}

	if err := bh.UnmarshalBinary(dataHeaderBuf); err != nil {
		return bh, fmt.Errorf("failed to deserialize block header for height %d: %w", expectedHeight, err)
	}

	if bh.Size != indexEntry.Size {
		return bh, fmt.Errorf("%w: for height %d, index size %d, data header size %d", ErrBlockSizeMismatch, expectedHeight, indexEntry.Size, bh.Size)
	}
	if bh.Height != expectedHeight {
		return bh, fmt.Errorf("internal error: requested %d, data header contains %d", expectedHeight, bh.Height)
	}
	return bh, nil
}

func (s *Database) readAndVerifyBlockData(indexEntry IndexEntry, bh blockHeader) (Block, error) {
	blockData := make(Block, bh.Size)
	actualDataOffset := indexEntry.Offset + sizeOfBlockHeader
	if actualDataOffset < indexEntry.Offset {
		return nil, fmt.Errorf("internal error: block data offset calculation overflowed for height %d", bh.Height)
	}

	_, err := s.dataFile.ReadAt(blockData, int64(actualDataOffset))
	if err != nil {
		return nil, fmt.Errorf("failed to read block data from data file for height %d: %w", bh.Height, err)
	}

	calculatedChecksum := calculateChecksum(blockData)
	if calculatedChecksum != bh.Checksum {
		return nil, fmt.Errorf("%w: for block height %d", ErrChecksumMismatch, bh.Height)
	}

	return blockData, nil
}

func calculateChecksum(data []byte) uint64 {
	return xxhash.Sum64(data)
}

func (s *Database) writeBlockAtOffset(offset uint64, bh blockHeader, block Block) error {
	headerBytes, err := bh.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to serialize block header: %w", err)
	}

	// Allocate combined buffer for header and block data
	// Using a single WriteAt instead of two separate calls for header and block
	// data reduces syscall overhead in high-concurrency environments.
	// The memory copy cost is lower than the syscall cost for typical block sizes.
	combinedBuf := make([]byte, sizeOfBlockHeader+uint64(len(block)))
	copy(combinedBuf, headerBytes)
	copy(combinedBuf[sizeOfBlockHeader:], block)
	if _, err := s.dataFile.WriteAt(combinedBuf, int64(offset)); err != nil {
		return fmt.Errorf("failed to write block to data file at offset %d: %w", offset, err)
	}

	if s.syncToDisk {
		if err := s.dataFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync data file after writing block %d: %w", bh.Height, err)
		}
	}
	return nil
}

func (s *Database) updateBlockHeights(writtenBlockHeight uint64) error {
	// update max contiguous height
	var prevContiguousCandidate uint64
	if writtenBlockHeight == s.header.MinBlockHeight {
		if s.header.MinBlockHeight > 0 {
			prevContiguousCandidate = s.header.MinBlockHeight - 1
		} else {
			prevContiguousCandidate = 0
		}
	} else if writtenBlockHeight > s.header.MinBlockHeight {
		prevContiguousCandidate = writtenBlockHeight - 1
	} else {
		return fmt.Errorf("internal error in MCH update: height %d < minimum %d", writtenBlockHeight, s.header.MinBlockHeight)
	}
	if s.maxContiguousHeight.CompareAndSwap(prevContiguousCandidate, writtenBlockHeight) {
		currentMax := writtenBlockHeight
		for {
			nextHeightToVerify := currentMax + 1
			idxEntry, readErr := s.readIndexEntry(nextHeightToVerify)
			if readErr != nil || idxEntry.IsEmpty() {
				break
			}
			if !s.maxContiguousHeight.CompareAndSwap(currentMax, nextHeightToVerify) {
				break // Someone else updated
			}
			currentMax = nextHeightToVerify
		}
	}

	// update max block height and persist header on checkpoint interval
	var oldMaxHeight uint64
	for {
		oldMaxHeight = s.maxBlockHeight.Load()
		if writtenBlockHeight <= oldMaxHeight {
			break
		}
		if s.maxBlockHeight.CompareAndSwap(oldMaxHeight, writtenBlockHeight) {
			// todo: consider separating checkpoint logic out of this function
			// a situation may arise where multiple blocks are written that trigger a checkpoint
			// in this case, we are persisting the header multiple times. But this can only happen during bootstrapping.
			// One solution is only checkpoint after x blocks are written, instead of at specific heights.
			if writtenBlockHeight%s.options.CheckpointInterval == 0 {
				if err := s.persistIndexHeader(s.syncToDisk); err != nil {
					return fmt.Errorf("block %d written, but checkpoint failed: %w", writtenBlockHeight, err)
				}
			}
			break
		}
	}
	return nil
}

func (s *Database) allocateBlockSpace(sizeWithDataHeader uint64) (writeDataOffset uint64, err error) {
	maxDataFileSize := s.header.MaxDataFileSize

	for {
		// Check if the new offset would overflow uint64.
		currentOffset := s.nextDataWriteOffset.Load()
		if currentOffset > math.MaxUint64-sizeWithDataHeader {
			return 0, fmt.Errorf(
				"adding block of size %d to offset %d would overflow uint64 data file pointer",
				sizeWithDataHeader, currentOffset,
			)
		}

		newOffset := currentOffset + sizeWithDataHeader
		if maxDataFileSize > 0 && newOffset > maxDataFileSize {
			return 0, fmt.Errorf(
				"adding block of size %d to offset %d (new offset %d) would exceed configured max data file size of %d bytes",
				sizeWithDataHeader, currentOffset, newOffset, maxDataFileSize,
			)
		}

		if s.nextDataWriteOffset.CompareAndSwap(currentOffset, newOffset) {
			return currentOffset, nil
		}
	}
}
