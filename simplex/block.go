// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"fmt"
	"sync"

	"github.com/ava-labs/simplex"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/utils/hashing"

	pSimplex "github.com/ava-labs/avalanchego/proto/pb/simplex"
)

var _ simplex.VerifiedBlock = (*VerifiedBlock)(nil)

type VerifiedBlock struct {
	computeDigestOnce sync.Once
	digest            simplex.Digest // cached, not serialized

	metadata   simplex.ProtocolMetadata
	innerBlock []byte // inner block bytes
}

// BlockHeader returns the block header for the verified block.
func (v *VerifiedBlock) BlockHeader() simplex.BlockHeader {
	v.computeDigestOnce.Do(v.computeDigest)
	return simplex.BlockHeader{
		ProtocolMetadata: v.metadata,
		Digest:           v.digest,
	}
}

// Bytes returns the serialized bytes of the verified block
// as the asn1 encoding of `encodedVerifiedBlock`.
func (v *VerifiedBlock) Bytes() []byte {
	cBlock := pSimplex.VerifiedBlock{
		Metadata: v.metadata.Bytes(),
		Block:    v.innerBlock,
	}

	buff, err := proto.Marshal(&cBlock)
	if err != nil {
		panic(fmt.Errorf("failed to marshal verified block: %w", err))
	}

	return buff
}

// computeDigest computes the digest of the block.
func (v *VerifiedBlock) computeDigest() {
	v.digest = hashing.ComputeHash256Array(v.Bytes())
}

func verifiedBlockFromBytes(buff []byte) (*VerifiedBlock, error) {
	var protoBlock pSimplex.VerifiedBlock

	if err := proto.Unmarshal(buff, &protoBlock); err != nil {
		return nil, fmt.Errorf("failed to unmarshal verified block: %w", err)
	}

	md, err := simplex.ProtocolMetadataFromBytes(protoBlock.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse protocol metadata: %w", err)
	}

	v := &VerifiedBlock{
		metadata:   *md,
		innerBlock: protoBlock.Block,
	}

	return v, nil
}
