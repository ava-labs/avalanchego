package simplex

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"simplex"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)
var maxBlockVerificationTime = time.Second * 30

type Block struct {
	e             *Engine
	verifiedBlock VerifiedBlock
}

func (b *Block) BlockHeader() simplex.BlockHeader {
	return b.verifiedBlock.BlockHeader()
}

func (b *Block) Verify(ctx context.Context) (simplex.VerifiedBlock, error) {
	// TODO: Implement the logic to verify the block.
	return &b.verifiedBlock, nil
}

type VerifiedBlock struct {
	computeDigestOnce sync.Once
	digest            simplex.Digest // cached, not serialized

	metadata   simplex.ProtocolMetadata
	innerBlock []byte
	accept     func(context.Context) error
}

// BlockHeader returns the block header for the verified block.
func (v *VerifiedBlock) BlockHeader() simplex.BlockHeader {
	v.computeDigestOnce.Do(v.computeDigest)
	return simplex.BlockHeader{
		ProtocolMetadata: v.metadata,
		Digest:           v.digest,
	}
}

func (v *VerifiedBlock) FromBytes(buff []byte) error {
	if len(buff) < 57 {
		return fmt.Errorf("buff too small, expected at least 57 bytes, got %d", len(buff))
	}
	v.metadataFromBytes(buff)
	v.innerBlock = buff[57:]
	return nil
}

func (v *VerifiedBlock) metadataFromBytes(buff []byte) {
	var pos int

	v.metadata.Version = buff[pos]
	pos++

	v.metadata.Epoch = binary.BigEndian.Uint64(buff[pos:])
	pos += 8

	v.metadata.Round = binary.BigEndian.Uint64(buff[pos:])
	pos += 8

	v.metadata.Seq = binary.BigEndian.Uint64(buff[pos:])
	pos += 8

	copy(v.metadata.Prev[:], buff[pos:pos+32])
}

func (v *VerifiedBlock) Bytes() []byte {
	mdBytes := v.metadataBytes()
	buff := make([]byte, len(mdBytes)+len(v.innerBlock))
	copy(buff, mdBytes)
	copy(buff[len(mdBytes):], v.innerBlock)
	return buff
}

// computeDigest computes the digest of the block.
func (v *VerifiedBlock) computeDigest() {
	mdBytes := v.metadataBytes()
	h := sha256.New()
	h.Write(v.innerBlock)
	h.Write(mdBytes)
	digest := h.Sum(nil)
	v.digest = simplex.Digest(digest[:])
}

func (v *VerifiedBlock) metadataBytes() []byte {
	buff := make([]byte, 57)
	var pos int

	buff[pos] = v.metadata.Version
	pos++

	binary.BigEndian.PutUint64(buff[pos:], v.metadata.Epoch)
	pos += 8

	binary.BigEndian.PutUint64(buff[pos:], v.metadata.Round)
	pos += 8

	binary.BigEndian.PutUint64(buff[pos:], v.metadata.Seq)
	pos += 8

	copy(buff[pos:], v.metadata.Prev[:])
	return buff
}

type blockDeserializer struct {
	vm block.ChainVM
}

func (b *blockDeserializer) DeserializeBlock(bytes []byte) (simplex.VerifiedBlock, error) {
	var vb VerifiedBlock
	if err := vb.FromBytes(bytes); err != nil {
		return nil, err
	}

	_, err := b.vm.ParseBlock(context.Background(), vb.innerBlock)
	if err != nil {
		return nil, err
	}

	vb.accept = func(ctx context.Context) error {
		panic("should not be called yet")
	}

	return &vb, vb.FromBytes(bytes)
}
