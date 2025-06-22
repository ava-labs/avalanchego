package blockdb

import (
	"encoding"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"
)

var (
	_ encoding.BinaryMarshaler   = blockHeader{}
	_ encoding.BinaryUnmarshaler = &blockHeader{}

	sizeOfBlockHeader = uint64(binary.Size(blockHeader{}))
)

// BlockHeight defines the type for block heights.
type BlockHeight = uint64

// BlockData defines the type for block data.
type BlockData = []byte

// BlockHeaderSize is the size of the header in the block data.
type BlockHeaderSize = uint16

// MaxBlockDataSize is the maximum size of a block in bytes (16 MB).
const MaxBlockDataSize = 1 << 24

// blockHeader is prepended to each block in the data file.
type blockHeader struct {
	Height     BlockHeight
	Size       uint64
	HeaderSize BlockHeaderSize
	Checksum   uint64
}

// MarshalBinary implements the encoding.BinaryMarshaler interface.
func (bh blockHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, sizeOfBlockHeader)
	binary.LittleEndian.PutUint64(buf[0:], bh.Height)
	binary.LittleEndian.PutUint64(buf[8:], bh.Size)
	binary.LittleEndian.PutUint16(buf[16:], bh.HeaderSize)
	binary.LittleEndian.PutUint64(buf[18:], bh.Checksum)
	return buf, nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
func (bh *blockHeader) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfBlockHeader) {
		return fmt.Errorf("incorrect data length to unmarshal blockHeader: got %d bytes, need exactly %d", len(data), sizeOfBlockHeader)
	}
	bh.Height = binary.LittleEndian.Uint64(data[0:])
	bh.Size = binary.LittleEndian.Uint64(data[8:])
	bh.HeaderSize = binary.LittleEndian.Uint16(data[16:])
	bh.Checksum = binary.LittleEndian.Uint64(data[18:])
	return nil
}

// WriteBlock inserts a block into the store at the given height with the specified header size.
func (s *Database) WriteBlock(height BlockHeight, block BlockData, headerSize BlockHeaderSize) error {
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

	if uint64(headerSize) >= uint64(len(block)) {
		return ErrHeaderSizeTooLarge
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
		Height:     height,
		Size:       blockDataLen,
		HeaderSize: headerSize,
		Checksum:   calculateChecksum(block),
	}
	if err := s.writeBlockAt(writeDataOffset, bh, block); err != nil {
		return err
	}

	if err := s.writeIndexEntryAt(indexFileOffset, writeDataOffset, blockDataLen, headerSize); err != nil {
		return err
	}

	return s.updateBlockHeights(height)
}

// ReadBlock retrieves a block by its height.
// Returns nil if the block is not found.
func (s *Database) ReadBlock(height BlockHeight) (BlockData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrDatabaseClosed
	}

	indexEntry, err := s.readIndexEntry(height)
	if err != nil {
		return nil, err
	}
	if indexEntry.IsEmpty() {
		return nil, nil
	}

	// Read the complete block data
	blockData := make(BlockData, indexEntry.Size)
	actualDataOffset := indexEntry.Offset + sizeOfBlockHeader
	if actualDataOffset < indexEntry.Offset {
		return nil, fmt.Errorf("internal error: block data offset calculation overflowed")
	}
	_, err = s.dataFile.ReadAt(blockData, int64(actualDataOffset))
	if err != nil {
		return nil, fmt.Errorf("failed to read block data from data file: %w", err)
	}

	return blockData, nil
}

// ReadHeader retrieves only the header portion of a block by its height.
// Returns nil if the block is not found or no header.
func (s *Database) ReadHeader(height BlockHeight) (BlockData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrDatabaseClosed
	}

	indexEntry, err := s.readIndexEntry(height)
	if err != nil {
		return nil, err
	}
	if indexEntry.IsEmpty() {
		return nil, nil
	}

	// Return nil if there's no header data
	if indexEntry.HeaderSize == 0 {
		return nil, nil
	}

	// Validate header size doesn't exceed total block size
	if uint64(indexEntry.HeaderSize) > indexEntry.Size {
		return nil, fmt.Errorf("invalid header size %d exceeds block size %d", indexEntry.HeaderSize, indexEntry.Size)
	}

	// Read only the header portion
	headerData := make([]byte, indexEntry.HeaderSize)
	actualDataOffset := indexEntry.Offset + sizeOfBlockHeader
	if actualDataOffset < indexEntry.Offset {
		return nil, fmt.Errorf("internal error: block data offset calculation overflowed")
	}
	_, err = s.dataFile.ReadAt(headerData, int64(actualDataOffset))
	if err != nil {
		return nil, fmt.Errorf("failed to read block header data from data file: %w", err)
	}

	return headerData, nil
}

// ReadBody retrieves only the body portion (excluding header) of a block by its height.
// Returns nil if the block is not found.
func (s *Database) ReadBody(height BlockHeight) (BlockData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrDatabaseClosed
	}

	indexEntry, err := s.readIndexEntry(height)
	if err != nil {
		return nil, err
	}
	if indexEntry.IsEmpty() {
		return nil, nil
	}

	bodySize := indexEntry.Size - uint64(indexEntry.HeaderSize)
	bodyData := make([]byte, bodySize)
	bodyOffset := indexEntry.Offset + sizeOfBlockHeader + uint64(indexEntry.HeaderSize)
	_, err = s.dataFile.ReadAt(bodyData, int64(bodyOffset))
	if err != nil {
		return nil, fmt.Errorf("failed to read block body data from data file: %w", err)
	}
	return bodyData, nil
}

func calculateChecksum(data []byte) uint64 {
	return xxhash.Sum64(data)
}

func (s *Database) writeBlockAt(offset uint64, bh blockHeader, block BlockData) error {
	headerBytes, err := bh.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to serialize block header: %w", err)
	}

	// Allocate combined buffer for header and block data and write it to the data file
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

func (s *Database) updateBlockHeights(writtenBlockHeight BlockHeight) error {
	prevContiguousCandidate := uint64(unsetHeight)
	if writtenBlockHeight > s.header.MinHeight {
		prevContiguousCandidate = writtenBlockHeight - 1
	}

	if s.maxContiguousHeight.CompareAndSwap(prevContiguousCandidate, writtenBlockHeight) {
		currentMax := writtenBlockHeight
		for {
			nextHeightToVerify := currentMax + 1
			entry, err := s.readIndexEntry(nextHeightToVerify)
			if err != nil {
				s.log.Error(
					"error reading index entry when updating max contiguous height",
					zap.Uint64("height", nextHeightToVerify),
					zap.Error(err),
				)
				break
			}
			if entry.IsEmpty() {
				break
			}
			if !s.maxContiguousHeight.CompareAndSwap(currentMax, nextHeightToVerify) {
				break // Someone else updated
			}
			currentMax = nextHeightToVerify
		}
	}

	// update max block height and persist header on checkpoint interval
	var oldMaxHeight BlockHeight
	for {
		oldMaxHeight = s.maxBlockHeight.Load()
		if writtenBlockHeight <= oldMaxHeight && oldMaxHeight != unsetHeight {
			break
		}
		if s.maxBlockHeight.CompareAndSwap(oldMaxHeight, writtenBlockHeight) {
			if writtenBlockHeight%s.options.CheckpointInterval == 0 {
				if err := s.persistIndexHeader(); err != nil {
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
