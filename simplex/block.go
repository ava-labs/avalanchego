package simplex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"simplex"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type Block struct {
	e             *Engine
	verifiedBlock VerifiedBlock
	vm            block.ChainVM
}

func (b *Block) BlockHeader() simplex.BlockHeader {
	return b.verifiedBlock.BlockHeader()
}

func (b *Block) Verify(ctx context.Context) (simplex.VerifiedBlock, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	block, err := b.vm.ParseBlock(ctx, b.verifiedBlock.innerBlock)
	if err != nil {
		return nil, err
	}

	md := b.BlockHeader()
	rejection := func(ctx context.Context) error {
		b.e.removeDigestToIDMapping(md.Digest)
		return block.Reject(ctx)
	}
	b.e.blockTracker.trackBlock(md.Round, md.Digest, rejection)
	b.verifiedBlock.accept = func(ctx context.Context) error {
		b.e.removeDigestToIDMapping(md.Digest)
		b.e.ChainVM.SetPreference(context.Background(), block.ID())
		b.e.blockTracker.rejectSiblingsAndUncles(md.Round, md.Digest)
		return block.Accept(ctx)
	}

	err = block.Verify(ctx)

	if err == nil {
		b.e.observeDigestToIDMapping(md.Digest, block.ID())
	}

	return &b.verifiedBlock, err
}

type VerifiedBlock struct {
	computeDigestOnce sync.Once
	digest            simplex.Digest // cached, not serialized

	metadata   simplex.ProtocolMetadata
	innerBlock []byte
	accept     func(context.Context) error
}

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

type blockRejection struct {
	digest simplex.Digest
	reject func(context.Context) error
}

// blockTracker maps rounds to blocks verified in that round that may be rejected
type blockTracker struct {
	lock            sync.Mutex
	round2Rejection map[uint64][]blockRejection
}

func (bt *blockTracker) init() {
	bt.round2Rejection = make(map[uint64][]blockRejection)
}

func (bt *blockTracker) rejectSiblingsAndUncles(round uint64, acceptedDigest simplex.Digest) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	bt.disposeOfUncles(round)
	bt.disposeOfSiblings(round, acceptedDigest)
	// Completely get rid of the round, to make sure that the accepted block is not rejected in future rounds.
	delete(bt.round2Rejection, round)
}

func (bt *blockTracker) disposeOfSiblings(round uint64, acceptedDigest simplex.Digest) {
	for _, rejection := range bt.round2Rejection[round] {
		if !bytes.Equal(rejection.digest[:], acceptedDigest[:]) {
			rejection.reject(context.Background())
		}
	}
}

func (bt *blockTracker) disposeOfUncles(round uint64) {
	for r, rejections := range bt.round2Rejection {
		if r < round {
			for _, rejection := range rejections {
				rejection.reject(context.Background())
			}
			delete(bt.round2Rejection, r)
		}
	}
}

func (bt *blockTracker) trackBlock(round uint64, digest simplex.Digest, reject func(context.Context) error) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	bt.round2Rejection[round] = append(bt.round2Rejection[round], blockRejection{
		digest: digest,
		reject: reject,
	})
}
