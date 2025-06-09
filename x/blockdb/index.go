package blockdb

import (
	"encoding"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	IndexFileVersion uint64 = 1
)

var (
	_ encoding.BinaryMarshaler   = IndexEntry{}
	_ encoding.BinaryUnmarshaler = &IndexEntry{}

	sizeOfIndexEntry      = uint64(binary.Size(IndexEntry{}))
	sizeOfIndexFileHeader = uint64(binary.Size(IndexFileHeader{}))
)

// IndexEntry locates a block within the data file.
type IndexEntry struct {
	// Offset is the byte offset in the data file where the block's header starts.
	Offset uint64
	// Size is the length in bytes of the block's data (not including the header).
	Size uint64 // todo: can this be omitted? currently is this only used to verify the block size, but we are already doing checksum verification. Removing this can double the amount of data in the index file.
}

// IsEmpty returns true if this entry is uninitialized.
// This indicates a slot where no block has been written.
func (e IndexEntry) IsEmpty() bool {
	return e.Offset == 0 && e.Size == 0
}

// MarshalBinary implements encoding.BinaryMarshaler for IndexEntry.
func (e IndexEntry) MarshalBinary() ([]byte, error) {
	buf := make([]byte, sizeOfIndexEntry)
	binary.LittleEndian.PutUint64(buf[0:], e.Offset)
	binary.LittleEndian.PutUint64(buf[8:], e.Size)
	return buf, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler for IndexEntry.
func (e *IndexEntry) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfIndexEntry) {
		return fmt.Errorf("incorrect data length to unmarshal IndexEntry: got %d bytes, need exactly %d", len(data), sizeOfIndexEntry)
	}
	e.Offset = binary.LittleEndian.Uint64(data[0:])
	e.Size = binary.LittleEndian.Uint64(data[8:])
	return nil
}

// IndexFileHeader is the header of the index file.
type IndexFileHeader struct {
	Version                  uint64
	MaxDataFileSize          uint64
	MaxBlockHeight           uint64
	MinBlockHeight           BlockHeight
	MaxContiguousBlockHeight BlockHeight
	DataFileSize             uint64
}

// Add MarshalBinary for IndexFileHeader
func (h IndexFileHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, sizeOfIndexFileHeader)
	binary.LittleEndian.PutUint64(buf[0:], h.Version)
	binary.LittleEndian.PutUint64(buf[8:], h.MaxDataFileSize)
	binary.LittleEndian.PutUint64(buf[16:], h.MaxBlockHeight)
	binary.LittleEndian.PutUint64(buf[24:], h.MinBlockHeight)
	binary.LittleEndian.PutUint64(buf[32:], h.MaxContiguousBlockHeight)
	binary.LittleEndian.PutUint64(buf[40:], h.DataFileSize)
	return buf, nil
}

// Add UnmarshalBinary for IndexFileHeader
func (h *IndexFileHeader) UnmarshalBinary(data []byte) error {
	if len(data) != int(sizeOfIndexFileHeader) {
		return fmt.Errorf(
			"incorrect data length to unmarshal IndexFileHeader: got %d bytes, need exactly %d",
			len(data), sizeOfIndexFileHeader,
		)
	}
	h.Version = binary.LittleEndian.Uint64(data[0:])
	h.MaxDataFileSize = binary.LittleEndian.Uint64(data[8:])
	h.MaxBlockHeight = binary.LittleEndian.Uint64(data[16:])
	h.MinBlockHeight = binary.LittleEndian.Uint64(data[24:])
	h.MaxContiguousBlockHeight = binary.LittleEndian.Uint64(data[32:])
	h.DataFileSize = binary.LittleEndian.Uint64(data[40:])
	return nil
}

func (s *Database) indexEntryOffset(height BlockHeight) (uint64, error) {
	if height < s.header.MinBlockHeight {
		return 0, fmt.Errorf("%w: height %d is less than minimum block height %d", ErrInvalidBlockHeight, height, s.header.MinBlockHeight)
	}
	relativeHeight := height - s.header.MinBlockHeight
	if relativeHeight > (math.MaxUint64-sizeOfIndexFileHeader)/sizeOfIndexEntry {
		return 0, fmt.Errorf("%w: index entry offset multiplication overflow for height %d", ErrInvalidBlockHeight, height)
	}
	offsetFromHeaderStart := relativeHeight * sizeOfIndexEntry
	finalOffset := sizeOfIndexFileHeader + offsetFromHeaderStart
	if finalOffset < sizeOfIndexFileHeader {
		return 0, fmt.Errorf("%w: index entry offset addition overflow for height %d", ErrInvalidBlockHeight, height)
	}
	return finalOffset, nil
}

func (s *Database) readIndexEntry(height BlockHeight) (IndexEntry, error) {
	offset, err := s.indexEntryOffset(height)
	if err != nil {
		return IndexEntry{}, err
	}

	var entry IndexEntry
	buf := make([]byte, sizeOfIndexEntry)
	_, err = s.indexFile.ReadAt(buf, int64(offset))
	if err != nil {
		if err == io.EOF {
			return entry, nil
		}
		return IndexEntry{}, fmt.Errorf("failed to read index entry at offset %d for height %d: %w", offset, height, err)
	}
	if err := entry.UnmarshalBinary(buf); err != nil {
		return IndexEntry{}, fmt.Errorf("failed to deserialize index entry for height %d: %w", height, err)
	}
	return entry, nil
}

func (s *Database) writeIndexEntryAt(indexFileOffset, dataFileBlockOffset, blockDataLen uint64) error {
	indexEntry := IndexEntry{
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

func (s *Database) persistIndexHeader(syncToDisk bool) error {
	// Why fsync indexFile before writing its header?
	// To prevent a critical inconsistency: the header must not describe a state
	// more advanced than what's durably stored in the index entries.
	//
	// 1. Writes Are Buffered: OS buffers index entry writes; they aren't immediately on disk.
	// 2. Header Reflects New State: The header is updated with new DataFileSize (for data file)
	//    and MaxContiguousBlockHeight (based on index entries).
	// 3. THE RISK IF HEADER IS WRITTEN/FLUSHED FIRST (before fsyncing entries):
	//    If the OS flushes the updated header to disk *before* it flushes the buffered
	//    index entries that justify the header's new state, then a crash would mean:
	//    - The on-disk header claims certain blocks/entries exist (up to new DataFileSize/MCH).
	//    - But the corresponding index entries themselves were lost (still in buffer at crash).
	//    This results in the header pointing to the updated DataFileSize in the data file
	//    but the index entries are not yet on disk, leading to missing blocks in the index file.
	//
	// By fsyncing indexFile *first*, we ensure all index entries are durably on disk.
	// Only then is the header written, guaranteeing it reflects a truly persisted state.
	if syncToDisk {
		if s.indexFile != nil {
			if err := s.indexFile.Sync(); err != nil {
				return fmt.Errorf("failed to sync index file before writing header state: %w", err)
			}
		} else {
			return fmt.Errorf("index file is nil, cannot sync or write header state")
		}
	}

	header := s.header
	header.DataFileSize = s.nextDataWriteOffset.Load()
	header.MaxContiguousBlockHeight = s.maxContiguousHeight.Load()
	header.MaxBlockHeight = s.maxBlockHeight.Load()
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to serialize header for writing state: %w", err)
	}
	if uint64(len(headerBytes)) != sizeOfIndexFileHeader {
		return fmt.Errorf("internal error: serialized header state size %d, expected %d", len(headerBytes), sizeOfIndexFileHeader)
	}

	if s.indexFile == nil {
		return fmt.Errorf("index file is nil, cannot write header state")
	}
	_, err = s.indexFile.WriteAt(headerBytes, 0)
	if err != nil {
		return fmt.Errorf("failed to write header state to index file: %w", err)
	}
	return nil
}
