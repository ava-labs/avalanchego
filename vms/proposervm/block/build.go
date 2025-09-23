// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"crypto/rand"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func BuildUnsigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	epoch Epoch,
	blockBytes []byte,
) (SignedBlock, error) {
	var (
		preGraniteStatelessUnsignedBlock = statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  nil,
			Block:        blockBytes,
		}
		block SignedBlock
	)
	if epoch.Number == 0 {
		block = &statelessBlock{
			StatelessBlock: preGraniteStatelessUnsignedBlock,
		}
	} else {
		block = &statelessGraniteBlock{
			StatelessBlock: statelessUnsignedGraniteBlock{
				StatelessBlock: preGraniteStatelessUnsignedBlock,
				Epoch:          epoch,
			},
		}
	}

	bytes, err := Codec.Marshal(CodecVersion, &block)
	if err != nil {
		return nil, err
	}

	return block, block.initialize(bytes)
}

func Build(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	epoch Epoch,
	cert *staking.Certificate,
	blockBytes []byte,
	chainID ids.ID,
	key crypto.Signer,
) (SignedBlock, error) {
	var (
		preGraniteStatelessUnsignedBlock = statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  cert.Raw,
			Block:        blockBytes,
		}
		block    SignedBlock
		sig      *[]byte
		metadata = &statelessBlockMetadata{
			timestamp: timestamp,
			cert:      cert,
			proposer:  ids.NodeIDFromCert(cert),
		}
	)
	if epoch.Number == 0 {
		blk := &statelessBlock{
			StatelessBlock:         preGraniteStatelessUnsignedBlock,
			statelessBlockMetadata: *metadata,
		}
		block = blk
		sig = &blk.Signature
		metadata = &blk.statelessBlockMetadata
	} else {
		blk := &statelessGraniteBlock{
			StatelessBlock: statelessUnsignedGraniteBlock{
				StatelessBlock: preGraniteStatelessUnsignedBlock,
				Epoch:          epoch,
			},
			statelessBlockMetadata: *metadata,
		}
		block = blk
		sig = &blk.Signature
		metadata = &blk.statelessBlockMetadata
	}

	unsignedBytesWithEmptySignature, err := Codec.Marshal(CodecVersion, &block)
	if err != nil {
		return nil, err
	}

	// The serialized form of the block is the unsignedBytes followed by the
	// signature, which is prefixed by a uint32. Because we are marshalling the
	// block with an empty signature, we only need to strip off the length
	// prefix to get the unsigned bytes.
	lenUnsignedBytes := len(unsignedBytesWithEmptySignature) - wrappers.IntLen
	unsignedBytes := unsignedBytesWithEmptySignature[:lenUnsignedBytes]

	// Set the block ID
	metadata.id = hashing.ComputeHash256Array(unsignedBytes)
	header, err := BuildHeader(chainID, parentID, metadata.id)
	if err != nil {
		return nil, err
	}

	headerHash := hashing.ComputeHash256(header.Bytes())
	signature, err := key.Sign(rand.Reader, headerHash, crypto.SHA256)
	if err != nil {
		return nil, err
	}

	// Set the signature
	*sig = signature

	// Marshal the final block with signature
	finalBytes, err := Codec.Marshal(CodecVersion, &block)
	if err != nil {
		return nil, err
	}

	// Set the final bytes
	metadata.bytes = finalBytes
	return block, nil
}

func BuildHeader(
	chainID ids.ID,
	parentID ids.ID,
	bodyID ids.ID,
) (Header, error) {
	header := statelessHeader{
		Chain:  chainID,
		Parent: parentID,
		Body:   bodyID,
	}

	bytes, err := Codec.Marshal(CodecVersion, &header)
	header.bytes = bytes
	return &header, err
}

// BuildOption the option block
// [parentID] is the ID of this option's wrapper parent block
// [innerBytes] is the byte representation of a child option block
func BuildOption(
	parentID ids.ID,
	innerBytes []byte,
) (Block, error) {
	var block Block = &option{
		PrntID:     parentID,
		InnerBytes: innerBytes,
	}

	bytes, err := Codec.Marshal(CodecVersion, &block)
	if err != nil {
		return nil, err
	}

	return block, block.initialize(bytes)
}
