// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type VerifiedBlock struct {
	computeDigestOnce sync.Once
	digest            simplex.Digest // cached, not serialized

	metadata   simplex.ProtocolMetadata
	innerBlock []byte
}

// BlockHeader returns the block header for the verified block.
func (v *VerifiedBlock) BlockHeader() simplex.BlockHeader {
	v.computeDigestOnce.Do(v.computeDigest)
	return simplex.BlockHeader{
		ProtocolMetadata: v.metadata,
		Digest:           v.digest,
	}
}

// Bytes returns the serialized bytes of the verified block.
// It concatenates the metadata bytes followed by the inner block bytes.
func (v *VerifiedBlock) Bytes() []byte {
	mdBytes := v.metadata.Bytes()
	buff := make([]byte, len(mdBytes)+len(v.innerBlock))
	copy(buff, mdBytes)
	copy(buff[len(mdBytes):], v.innerBlock)
	return buff
}

// computeDigest computes the digest of the block.
func (v *VerifiedBlock) computeDigest() {
	mdBytes := v.metadata.Bytes()
	h := sha256.New()
	h.Write(v.innerBlock)
	h.Write(mdBytes)
	digest := h.Sum(nil)
	v.digest = simplex.Digest(digest)
}

type blockDeserializer struct {
	vm block.ChainVM
}

func (b *blockDeserializer) DeserializeBlock(bytes []byte) (simplex.VerifiedBlock, error) {
	vb, err := verifiedBlockFromBytes(bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize verified block: %w", err)
	}

	_, err = b.vm.ParseBlock(context.Background(), vb.innerBlock)
	if err != nil {
		return nil, err
	}

	return vb, nil
}

func verifiedBlockFromBytes(buff []byte) (*VerifiedBlock, error) {
	if len(buff) < simplex.ProtocolMetadataLen {
		return nil, fmt.Errorf("buff too small, expected at least %d bytes, got %d", simplex.ProtocolMetadataLen, len(buff))
	}

	md, err := simplex.ProtocolMetadataFromBytes(buff[:simplex.ProtocolMetadataLen])
	if err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	v := &VerifiedBlock{
		metadata:   *md,
		innerBlock: buff[simplex.ProtocolMetadataLen:],
	}

	return v, nil
}
