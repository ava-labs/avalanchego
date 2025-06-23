// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"
)

var (
	_ encoding.BinaryMarshaler   = (*blockHeader)(nil)
	_ encoding.BinaryUnmarshaler = (*blockHeader)(nil)

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

	blockDataLen := uint64(len(block))
	if blockDataLen == 0 {
		return ErrBlockEmpty
	}

	if blockDataLen > MaxBlockDataSize {
		return ErrBlockTooLarge
	}

	if uint64(headerSize) >= uint64(len(block)) {
		return ErrHeaderSizeTooLarge
	}

	indexFileOffset, err := s.indexEntryOffset(height)
	if err != nil {
		return err
	}

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
	dataFile, localOffset, err := s.getDataFileAndOffset(indexEntry.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get data file for block at height %d: %w", height, err)
	}
	_, err = dataFile.ReadAt(blockData, int64(localOffset+sizeOfBlockHeader))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
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
	dataFile, localOffset, err := s.getDataFileAndOffset(indexEntry.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get data file for block header at height %d: %w", height, err)
	}
	_, err = dataFile.ReadAt(headerData, int64(localOffset+sizeOfBlockHeader))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
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
	dataFile, localOffset, err := s.getDataFileAndOffset(indexEntry.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get data file for block body at height %d: %w", height, err)
	}
	bodyOffset := localOffset + sizeOfBlockHeader + uint64(indexEntry.HeaderSize)
	_, err = dataFile.ReadAt(bodyData, int64(bodyOffset))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
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

	dataFile, localOffset, err := s.getDataFileAndOffset(offset)
	if err != nil {
		return fmt.Errorf("failed to get data file for writing block %d: %w", bh.Height, err)
	}

	// Allocate combined buffer for header and block data and write it to the data file
	combinedBuf := make([]byte, sizeOfBlockHeader+uint64(len(block)))
	copy(combinedBuf, headerBytes)
	copy(combinedBuf[sizeOfBlockHeader:], block)
	if _, err := dataFile.WriteAt(combinedBuf, int64(localOffset)); err != nil {
		return fmt.Errorf("failed to write block to data file at offset %d: %w", offset, err)
	}

	if s.syncToDisk {
		if err := dataFile.Sync(); err != nil {
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

func (s *Database) allocateBlockSpace(totalSize uint64) (writeDataOffset uint64, err error) {
	maxDataFileSize := s.header.MaxDataFileSize

	// Check if a single block would exceed the max data file size
	if maxDataFileSize > 0 && totalSize > maxDataFileSize {
		return 0, ErrBlockTooLarge
	}

	for {
		currentOffset := s.nextDataWriteOffset.Load()
		if currentOffset > math.MaxUint64-totalSize {
			return 0, fmt.Errorf(
				"adding block of size %d to offset %d would overflow uint64 data file pointer",
				totalSize, currentOffset,
			)
		}

		writeOffset := currentOffset
		newOffset := currentOffset + totalSize

		if maxDataFileSize > 0 {
			fileIndex := int(currentOffset / maxDataFileSize)
			localOffset := currentOffset % maxDataFileSize

			if localOffset+totalSize > maxDataFileSize {
				writeOffset = (uint64(fileIndex) + 1) * maxDataFileSize
				newOffset = writeOffset + totalSize
			}
		}

		if s.nextDataWriteOffset.CompareAndSwap(currentOffset, newOffset) {
			return writeOffset, nil
		}
	}
}

func (s *Database) getDataFileAndOffset(globalOffset uint64) (*os.File, uint64, error) {
	maxFileSize := s.header.MaxDataFileSize
	if maxFileSize == 0 {
		handle, err := s.getOrOpenDataFile(0)
		return handle, globalOffset, err
	}

	fileIndex := int(globalOffset / maxFileSize)
	localOffset := globalOffset % maxFileSize
	handle, err := s.getOrOpenDataFile(fileIndex)
	return handle, localOffset, err
}
