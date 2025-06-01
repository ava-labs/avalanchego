package blockdb

import (
	"encoding"
	"encoding/binary"
	"fmt"
)

const MaxBlockDataSize = 1 << 30 // 1 GB

var (
	_ encoding.BinaryMarshaler   = blockHeader{}
	_ encoding.BinaryUnmarshaler = &blockHeader{}

	sizeOfBlockHeader = uint64(binary.Size(blockHeader{}))
)

// blockHeader is prepended to each block in the data file.
type blockHeader struct {
	Height uint64
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
func (s *Store) WriteBlock(height BlockHeight, block Block) error {
	// TODO
	return nil
}

// ReadBlock retrieves a block by its height.
// Returns the block data or an error if not found or block data is corrupted.
func (s *Store) ReadBlock(height BlockHeight) (Block, error) {
	// TODO
	return nil, nil
}
