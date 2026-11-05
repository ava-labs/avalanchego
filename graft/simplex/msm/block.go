// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"crypto/sha256"
	"fmt"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
)

//go:generate go run github.com/StephenButtolph/canoto/canoto block.go

type BlockType uint8

// A StateMachineBlock is a representation of a parsed OuterBlock, containing the inner block and the metadata.
type StateMachineBlock struct {
	// InnerBlock is the VM-level block, or nil if this is a block without an inner block (e.g., a Telock block).
	InnerBlock avalanchego.VMBlock
	// Metadata contains the state machine metadata associated with this block.
	Metadata StateMachineMetadata
}

// RawBlock is the serialized form of a StateMachineBlock.
type RawBlock struct {
	Metadata        StateMachineMetadata `canoto:"value,1"`
	InnerBlockBytes []byte               `canoto:"bytes,2"`

	canotoData canotoData_RawBlock
}

// Clone returns a shallow copy of the block, skipping the canoto caches
// so it is safe to call while the original is being marshaled.
func (smb *StateMachineBlock) Clone() StateMachineBlock {
	return StateMachineBlock{
		InnerBlock: smb.InnerBlock,
		Metadata:   smb.Metadata.Clone(),
	}
}

// Digest returns the SHA-256 hash of the combined inner block digest and metadata digest.
func (smb *StateMachineBlock) Digest() common.Digest {
	var blockDigest [32]byte
	if smb.InnerBlock != nil {
		blockDigest = smb.InnerBlock.Digest()
	} else {
		blockDigest = [32]byte{}
	}
	md := smb.Metadata.Clone()
	mdDigest := sha256.Sum256(md.MarshalCanoto())
	combined := make([]byte, 64)
	copy(combined[:32], blockDigest[:])
	copy(combined[32:], mdDigest[:])
	return sha256.Sum256(combined)
}

const (
	BlockTypeNormal        BlockType = iota + 1 // In-epoch block with no epoch transition in progress
	BlockTypeZero                               // The first ever Simplex block; establishes the first epoch
	BlockTypeTelock                             // Built after the sealing block to extend its epoch until the sealing block finalizes
	BlockTypeSealing                            // Seals its epoch; only Telocks may follow it
	BlockTypeTransitioning                      // An epoch transition is in progress (collecting aux info and approvals) but the epoch is not yet sealed
)

func (bt BlockType) String() string {
	switch bt {
	case BlockTypeNormal:
		return "Normal"
	case BlockTypeZero:
		return "Zero"
	case BlockTypeTelock:
		return "Telock"
	case BlockTypeSealing:
		return "Sealing"
	case BlockTypeTransitioning:
		return "Transitioning"
	default:
		return fmt.Sprintf("UnknownBlockType(%d)", bt)
	}
}

var emptyHash common.Digest

func (smb *StateMachineBlock) Type() BlockType {
	sei := smb.Metadata.SimplexEpochInfo

	// Only sealing blocks carry block validation descriptors
	if sei.BlockValidationDescriptor != nil {
		// The zeroth block has a descriptor but points to an empty digest
		if sei.PrevSealingBlockHash == emptyHash {
			return BlockTypeZero
		}
		return BlockTypeSealing
	}

	if sei.SealingBlockSeq != 0 {
		return BlockTypeTelock
	}

	if sei.NextPChainReferenceHeight > 0 {
		return BlockTypeTransitioning
	}

	// Otherwise, we do not fall into any of these cases, so it's a normal block
	return BlockTypeNormal
}

// SealingBlockInfo returns the information derived from this block's BlockValidationDescriptor:
// the validator set of the next epoch and the hash of the previous sealing block.
// It returns the zero value for blocks that carry no descriptor (any block that is neither
// a sealing block nor the zero block).
func (smb *StateMachineBlock) SealingBlockInfo() *common.SealingBlockInfo {
	bvd := smb.Metadata.SimplexEpochInfo.BlockValidationDescriptor
	if bvd == nil {
		return nil
	}

	nodes := make(common.Nodes, 0, len(bvd.AggregatedMembership.Members))
	for i := range bvd.AggregatedMembership.Members {
		vdr := &bvd.AggregatedMembership.Members[i]
		nodes = append(nodes, common.Node{
			Id:     vdr.NodeID[:],
			Weight: vdr.Weight,
			PK:     vdr.BLSKey,
		})
	}

	return &common.SealingBlockInfo{
		ValidatorSet:         nodes,
		PrevSealingBlockHash: smb.Metadata.SimplexEpochInfo.PrevSealingBlockHash,
	}
}
