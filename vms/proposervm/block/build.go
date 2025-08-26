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
	pChainEpoch PChainEpoch,
	blockBytes []byte,
) (SignedBlock, error) {
	var block SignedBlock
	if pChainEpoch.Number == 0 {
		block = &statelessBlock{
			StatelessBlock: statelessUnsignedBlock{
				ParentID:     parentID,
				Timestamp:    timestamp.Unix(),
				PChainHeight: pChainHeight,
				Certificate:  nil,
				Block:        blockBytes,
			},
			timestamp: timestamp,
		}
	} else {
		block = &statelessGraniteBlock{
			StatelessBlock: statelessUnsignedGraniteBlock{
				PChainEpoch:  pChainEpoch,
				ParentID:     parentID,
				Timestamp:    timestamp.UnixMilli(),
				PChainHeight: pChainHeight,
				Certificate:  nil,
				Block:        blockBytes,
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
	pChainEpoch PChainEpoch,
	cert *staking.Certificate,
	blockBytes []byte,
	chainID ids.ID,
	key crypto.Signer,
) (SignedBlock, error) {
	var block SignedBlock
	if pChainEpoch.Number == 0 {
		block = &statelessBlock{
			StatelessBlock: statelessUnsignedBlock{
				ParentID:     parentID,
				Timestamp:    timestamp.Unix(),
				PChainHeight: pChainHeight,
				Certificate:  cert.Raw,
				Block:        blockBytes,
			},
			timestamp: timestamp,
			cert:      cert,
			proposer:  ids.NodeIDFromCert(cert),
		}
	} else {
		block = &statelessGraniteBlock{
			StatelessBlock: statelessUnsignedGraniteBlock{
				PChainEpoch:  pChainEpoch,
				ParentID:     parentID,
				Timestamp:    timestamp.UnixMilli(),
				PChainHeight: pChainHeight,
				Certificate:  cert.Raw,
				Block:        blockBytes,
			},
			timestamp: timestamp,
			cert:      cert,
			proposer:  ids.NodeIDFromCert(cert),
		}
	}

	return initializeAndSignBlock(block, chainID, parentID, key)
}

// initializeAndSignBlock handles the common initialization and signing logic
// for both statelessBlock and statelessGraniteBlock types
func initializeAndSignBlock(block SignedBlock, chainID, parentID ids.ID, key crypto.Signer) (SignedBlock, error) {
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
	switch b := block.(type) {
	case *statelessBlock:
		b.id = hashing.ComputeHash256Array(unsignedBytes)
	case *statelessGraniteBlock:
		b.id = hashing.ComputeHash256Array(unsignedBytes)
	}

	header, err := BuildHeader(chainID, parentID, block.ID())
	if err != nil {
		return nil, err
	}

	headerHash := hashing.ComputeHash256(header.Bytes())
	signature, err := key.Sign(rand.Reader, headerHash, crypto.SHA256)
	if err != nil {
		return nil, err
	}

	// Set the signature
	switch b := block.(type) {
	case *statelessBlock:
		b.Signature = signature
	case *statelessGraniteBlock:
		b.Signature = signature
	}

	// Marshal the final block with signature
	finalBytes, err := Codec.Marshal(CodecVersion, &block)
	if err != nil {
		return nil, err
	}

	// Set the final bytes
	switch b := block.(type) {
	case *statelessBlock:
		b.bytes = finalBytes
	case *statelessGraniteBlock:
		b.bytes = finalBytes
	}

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
