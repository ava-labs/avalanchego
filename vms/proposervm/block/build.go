// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
		statelessUnsignedBlock = statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  nil,
			Block:        blockBytes,
		}
		block SignedBlock
	)
	if epoch == (Epoch{}) {
		block = &statelessBlock{
			StatelessBlock: statelessUnsignedBlock,
		}
	} else {
		block = &statelessGraniteBlock{
			StatelessGraniteBlock: statelessUnsignedGraniteBlock{
				StatelessBlock: statelessUnsignedBlock,
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
		statelessUnsignedBlock = statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  cert.Raw,
			Block:        blockBytes,
		}
		metadata  *statelessBlockMetadata
		signature *[]byte
		block     SignedBlock
	)
	if epoch == (Epoch{}) {
		b := &statelessBlock{
			StatelessBlock: statelessUnsignedBlock,
		}
		metadata = &b.statelessBlockMetadata
		signature = &b.Signature
		block = b
	} else {
		b := &statelessGraniteBlock{
			StatelessGraniteBlock: statelessUnsignedGraniteBlock{
				StatelessBlock: statelessUnsignedBlock,
				Epoch:          epoch,
			},
		}
		metadata = &b.statelessBlockMetadata
		signature = &b.Signature
		block = b
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

	id := hashing.ComputeHash256Array(unsignedBytes)
	header, err := BuildHeader(chainID, parentID, id)
	if err != nil {
		return nil, err
	}

	headerHash := hashing.ComputeHash256(header.Bytes())
	*signature, err = key.Sign(rand.Reader, headerHash, crypto.SHA256)
	if err != nil {
		return nil, err
	}

	// Marshal the final block with signature
	finalBytes, err := Codec.Marshal(CodecVersion, &block)
	if err != nil {
		return nil, err
	}

	// Set the metadata
	*metadata = statelessBlockMetadata{
		id:        id,
		timestamp: timestamp,
		cert:      cert,
		proposer:  ids.NodeIDFromCert(cert),
		bytes:     finalBytes,
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
