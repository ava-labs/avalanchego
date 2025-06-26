// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"fmt"

	"github.com/ava-labs/simplex"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/utils/hashing"

	pSimplex "github.com/ava-labs/avalanchego/proto/pb/simplex"
)

var _ simplex.VerifiedBlock = (*VerifiedBlock)(nil)

type VerifiedBlock struct {
	digest simplex.Digest // cached, not serialized

	metadata   simplex.ProtocolMetadata
	innerBlock []byte // inner block bytes
}

// BlockHeader returns the block header for the verified block.
func (v *VerifiedBlock) BlockHeader() simplex.BlockHeader {
	return simplex.BlockHeader{
		ProtocolMetadata: v.metadata,
		Digest:           v.digest,
	}
}

// Bytes returns the serialized bytes of the verified block
// as the proto encoding of `encodedVerifiedBlock`.
// TODO: we should use a canonical encoding since proto is not guaranteed to be canonical.
func (v *VerifiedBlock) Bytes() ([]byte, error) {
	cBlock := pSimplex.VerifiedBlock{
		Metadata: v.metadata.Bytes(),
		Block:    v.innerBlock,
	}

	buff, err := proto.Marshal(&cBlock)
	if err != nil {
		return nil, err
	}

	return buff, nil
}

// computeDigest computes the digest of the block.
func (v *VerifiedBlock) computeDigest() (simplex.Digest, error) {
	bytes, err := v.Bytes()
	if err != nil {
		return simplex.Digest{}, fmt.Errorf("failed to serialize verified block: %w", err)
	}

	return hashing.ComputeHash256Array(bytes), nil
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

	digest, err := v.computeDigest()
	if err != nil {
		return nil, fmt.Errorf("failed to compute digest: %w", err)
	}

	v.digest = digest

	return v, nil
}
