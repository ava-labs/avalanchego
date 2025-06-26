// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	IndexFileVersion uint64 = 1
)

var (
	_ encoding.BinaryMarshaler   = (*indexEntry)(nil)
	_ encoding.BinaryUnmarshaler = (*indexEntry)(nil)

	sizeOfIndexEntry      = uint64(binary.Size(indexEntry{}))
	sizeOfIndexFileHeader = uint64(binary.Size(indexFileHeader{}))
)

type indexEntry struct {
	// Offset is the byte offset in the data file where the block's header starts.
	Offset uint64
	// Size is the length in bytes of the block's data (excluding the blockHeader).
	Size uint32
	// HeaderSize is the size in bytes of the block's header portion within the data.
	HeaderSize BlockHeaderSize
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
	binary.LittleEndian.PutUint32(buf[12:], e.HeaderSize)
	return buf, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler for indexEntry.
func (e *indexEntry) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfIndexEntry) {
		return fmt.Errorf("incorrect data length to unmarshal indexEntry: got %d bytes, need exactly %d", len(data), sizeOfIndexEntry)
	}
	e.Offset = binary.LittleEndian.Uint64(data[0:])
	e.Size = binary.LittleEndian.Uint32(data[8:])
	e.HeaderSize = binary.LittleEndian.Uint32(data[12:])
	return nil
}

// indexFileHeader is the header of the index file.
type indexFileHeader struct {
	Version             uint64
	MaxDataFileSize     uint64
	MinHeight           BlockHeight
	MaxContiguousHeight BlockHeight
	MaxHeight           BlockHeight
	NextWriteOffset     uint64
	// reserve 32 bytes for future use
	Reserved [32]byte
}

// Add MarshalBinary for indexFileHeader
func (h indexFileHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, sizeOfIndexFileHeader)
	binary.LittleEndian.PutUint64(buf[0:], h.Version)
	binary.LittleEndian.PutUint64(buf[8:], h.MaxDataFileSize)
	binary.LittleEndian.PutUint64(buf[16:], h.MinHeight)
	binary.LittleEndian.PutUint64(buf[24:], h.MaxContiguousHeight)
	binary.LittleEndian.PutUint64(buf[32:], h.MaxHeight)
	binary.LittleEndian.PutUint64(buf[40:], h.NextWriteOffset)
	return buf, nil
}

// Add UnmarshalBinary for indexFileHeader
func (h *indexFileHeader) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfIndexFileHeader) {
		return fmt.Errorf(
			"incorrect data length to unmarshal indexFileHeader: got %d bytes, need exactly %d",
			len(data), sizeOfIndexFileHeader,
		)
	}
	h.Version = binary.LittleEndian.Uint64(data[0:])
	h.MaxDataFileSize = binary.LittleEndian.Uint64(data[8:])
	h.MinHeight = binary.LittleEndian.Uint64(data[16:])
	h.MaxContiguousHeight = binary.LittleEndian.Uint64(data[24:])
	h.MaxHeight = binary.LittleEndian.Uint64(data[32:])
	h.NextWriteOffset = binary.LittleEndian.Uint64(data[40:])
	return nil
}

func (s *Database) indexEntryOffset(height BlockHeight) (uint64, error) {
	if height < s.header.MinHeight {
		return 0, fmt.Errorf("%w: height %d is less than minimum block height %d", ErrInvalidBlockHeight, height, s.header.MinHeight)
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

func (s *Database) readIndexEntry(height BlockHeight) (indexEntry, error) {
	var entry indexEntry
	if height > s.maxBlockHeight.Load() {
		return entry, nil
	}

	offset, err := s.indexEntryOffset(height)
	if err != nil {
		return entry, err
	}

	buf := make([]byte, sizeOfIndexEntry)
	_, err = s.indexFile.ReadAt(buf, int64(offset))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return entry, nil
		}
		return entry, fmt.Errorf("failed to read index entry at offset %d for height %d: %w", offset, height, err)
	}
	if err := entry.UnmarshalBinary(buf); err != nil {
		return entry, fmt.Errorf("failed to deserialize index entry for height %d: %w", height, err)
	}

	return entry, nil
}

func (s *Database) writeIndexEntryAt(indexFileOffset, dataFileBlockOffset uint64, blockDataLen uint32, headerSize BlockHeaderSize) error {
	indexEntry := indexEntry{
		Offset:     dataFileBlockOffset,
		Size:       blockDataLen,
		HeaderSize: headerSize,
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
	// The index file must be fsync'd before the header is written to prevent
	// a state where the header is persisted but the index entries it refers to
	// are not. This could lead to data inconsistency on recovery.
	if s.options.SyncToDisk {
		if err := s.indexFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync index file before writing header state: %w", err)
		}
	}

	header := s.header
	header.NextWriteOffset = s.nextDataWriteOffset.Load()
	header.MaxContiguousHeight = s.maxContiguousHeight.Load()
	header.MaxHeight = s.maxBlockHeight.Load()
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to serialize header for writing state: %w", err)
	}
	if uint64(len(headerBytes)) != sizeOfIndexFileHeader {
		return fmt.Errorf("internal error: serialized header state size %d, expected %d", len(headerBytes), sizeOfIndexFileHeader)
	}

	if _, err := s.indexFile.WriteAt(headerBytes, 0); err != nil {
		return fmt.Errorf("failed to write header state to index file: %w", err)
	}
	return nil
}
