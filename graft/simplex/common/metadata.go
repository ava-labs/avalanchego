// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"bytes"
	"fmt"
)

const (
	metadataDigestLen = 32
)

const (
	digestFormatSize = 10
)

// ProtocolMetadata encodes information about the protocol state at a given point in time.
type ProtocolMetadata struct {
	// Version defines the version of the protocol this block was created with.
	Version uint8 `canoto:"uint,1"`
	// Epoch returns the epoch in which the block was proposed
	Epoch uint64 `canoto:"uint,2"`
	// Round returns the round number in which the block was proposed.
	// Can also be an empty block.
	Round uint64 `canoto:"uint,3"`
	// Seq is the order of the block among all blocks in the blockchain.
	// Cannot correspond to an empty block.
	Seq uint64 `canoto:"uint,4"`
	// Prev returns the digest of the previous data block
	Prev Digest `canoto:"fixed bytes,5"`

	canotoData canotoData_ProtocolMetadata
}

// BlockHeader encodes a succinct and collision-free representation of a block.
// It's included in votes and finalizations in order to convey which block is voted on,
// or which block is finalized.
type BlockHeader struct {
	ProtocolMetadata `canoto:"value,1"`

	// Digest returns a collision resistant short representation of the block's bytes
	Digest Digest `canoto:"fixed bytes,2"`

	canotoData canotoData_BlockHeader
}

type Digest [metadataDigestLen]byte

func (d Digest) String() string {
	return fmt.Sprintf("%x...", (d)[:digestFormatSize])
}

func (bh *BlockHeader) String() string {
	return fmt.Sprintf("BlockHeader{Digest: %s, Prev: %s, Epoch: %d, Round: %d, Seq: %d, Version: %d}",
		bh.Digest.String(), bh.Prev.String(), bh.Epoch, bh.Round, bh.Seq, bh.Version)
}

func (bh *BlockHeader) Equals(other *BlockHeader) bool {
	return bytes.Equal(bh.Digest[:], other.Digest[:]) &&
		bytes.Equal(bh.Prev[:], other.Prev[:]) && bh.Epoch == other.Epoch &&
		bh.Round == other.Round && bh.Seq == other.Seq && bh.Version == other.Version
}

func (bh *BlockHeader) Bytes() []byte {
	clone := BlockHeader{
		ProtocolMetadata: bh.Clone(),
		Digest:           bh.Digest,
	}
	return clone.MarshalCanoto()
}
func (bh *BlockHeader) Size() int {
	clone := BlockHeader{
		ProtocolMetadata: bh.Clone(),
		Digest:           bh.Digest,
	}
	return len(clone.Bytes())
}

func (bh *BlockHeader) FromBytes(buff []byte) error {
	return bh.UnmarshalCanoto(buff)
}

// Serializes a ProtocolMetadata from a byte slice.
func ProtocolMetadataFromBytes(buff []byte) (*ProtocolMetadata, error) {
	md := &ProtocolMetadata{}
	return md, md.UnmarshalCanoto(buff)
}

// Clone returns a copy of the ProtocolMetadata.
func (md *ProtocolMetadata) Clone() ProtocolMetadata {
	return ProtocolMetadata{
		Version: md.Version,
		Epoch:   md.Epoch,
		Round:   md.Round,
		Seq:     md.Seq,
		Prev:    md.Prev,
	}
}

// Bytes returns a byte encoding of the ProtocolMetadata.
func (md *ProtocolMetadata) Bytes() []byte {
	clone := md.Clone()
	return clone.MarshalCanoto()
}
