package blockdb

import (
	"encoding"
	"encoding/binary"
	"fmt"
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
	Size uint64
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
