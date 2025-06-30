// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ encoding.BinaryMarshaler   = (*blockHeader)(nil)
	_ encoding.BinaryUnmarshaler = (*blockHeader)(nil)

	sizeOfBlockHeader = uint32(binary.Size(blockHeader{}))
)

// BlockHeight defines the type for block heights.
type BlockHeight = uint64

// BlockData defines the type for block data.
type BlockData = []byte

// BlockHeaderSize is the size of the header in the block data.
type BlockHeaderSize = uint32

// MaxBlockDataSize is the maximum size of a block in bytes (16 MB).
const MaxBlockDataSize = 1 << 24

// blockHeader is prepended to each block in the data file.
type blockHeader struct {
	Height     BlockHeight
	Checksum   uint64
	Size       uint32
	HeaderSize BlockHeaderSize
}

// MarshalBinary implements the encoding.BinaryMarshaler interface.
func (bh blockHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, sizeOfBlockHeader)
	binary.LittleEndian.PutUint64(buf[0:], bh.Height)
	binary.LittleEndian.PutUint64(buf[8:], bh.Checksum)
	binary.LittleEndian.PutUint32(buf[16:], bh.Size)
	binary.LittleEndian.PutUint32(buf[20:], bh.HeaderSize)
	return buf, nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
func (bh *blockHeader) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfBlockHeader) {
		return fmt.Errorf("incorrect data length to unmarshal blockHeader: got %d bytes, need exactly %d", len(data), sizeOfBlockHeader)
	}
	bh.Height = binary.LittleEndian.Uint64(data[0:])
	bh.Checksum = binary.LittleEndian.Uint64(data[8:])
	bh.Size = binary.LittleEndian.Uint32(data[16:])
	bh.HeaderSize = binary.LittleEndian.Uint32(data[20:])
	return nil
}

// WriteBlock inserts a block into the store at the given height with the specified header size.
func (s *Database) WriteBlock(height BlockHeight, block BlockData, headerSize BlockHeaderSize) error {
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()

	if s.closed {
		return ErrDatabaseClosed
	}

	blockDataLen := uint32(len(block))
	if blockDataLen == 0 {
		return ErrBlockEmpty
	}

	if blockDataLen > MaxBlockDataSize {
		return ErrBlockTooLarge
	}

	if headerSize >= blockDataLen {
		return ErrHeaderSizeTooLarge
	}

	indexFileOffset, err := s.indexEntryOffset(height)
	if err != nil {
		return err
	}

	sizeWithDataHeader, err := safemath.Add(sizeOfBlockHeader, blockDataLen)
	if err != nil {
		return fmt.Errorf("calculating total block size would overflow for block at height %d: %w", height, err)
	}
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
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()

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
	_, err = dataFile.ReadAt(blockData, int64(localOffset+uint64(sizeOfBlockHeader)))
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
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()

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
	if indexEntry.HeaderSize > indexEntry.Size {
		return nil, fmt.Errorf("invalid header size %d exceeds block size %d", indexEntry.HeaderSize, indexEntry.Size)
	}

	// Read only the header portion
	headerData := make([]byte, indexEntry.HeaderSize)
	dataFile, localOffset, err := s.getDataFileAndOffset(indexEntry.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get data file for block header at height %d: %w", height, err)
	}
	_, err = dataFile.ReadAt(headerData, int64(localOffset+uint64(sizeOfBlockHeader)))
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
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()

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

	bodySize := indexEntry.Size - indexEntry.HeaderSize
	bodyData := make([]byte, bodySize)
	dataFile, localOffset, err := s.getDataFileAndOffset(indexEntry.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get data file for block body at height %d: %w", height, err)
	}
	headerOffset, err := safemath.Add(localOffset, uint64(sizeOfBlockHeader))
	if err != nil {
		return nil, fmt.Errorf("calculating header offset would overflow for block at height %d: %w", height, err)
	}
	bodyOffset, err := safemath.Add(headerOffset, uint64(indexEntry.HeaderSize))
	if err != nil {
		return nil, fmt.Errorf("calculating body offset would overflow for block at height %d: %w", height, err)
	}

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
	combinedBufSize, err := safemath.Add(uint64(sizeOfBlockHeader), uint64(len(block)))
	if err != nil {
		return fmt.Errorf("calculating combined buffer size would overflow for block %d: %w", bh.Height, err)
	}
	combinedBuf := make([]byte, combinedBufSize)
	copy(combinedBuf, headerBytes)
	copy(combinedBuf[sizeOfBlockHeader:], block)
	if _, err := dataFile.WriteAt(combinedBuf, int64(localOffset)); err != nil {
		return fmt.Errorf("failed to write block to data file at offset %d: %w", offset, err)
	}

	if s.options.SyncToDisk {
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
			nextHeightToVerify, err := safemath.Add(currentMax, 1)
			if err != nil {
				s.log.Error("overflow in height calculation when updating max contiguous height")
				break
			}
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
	if maxDataFileSize > 0 && uint64(totalSize) > maxDataFileSize {
		return 0, ErrBlockTooLarge
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
