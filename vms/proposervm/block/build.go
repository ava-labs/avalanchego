// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto/x509"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func BuildUnsigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	blockBytes []byte,
) (SignedBlock, error) {
	var block SignedBlock = &statelessCertSignedBlock{
		StatelessBlock: statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
		},
		Certificate: nil,
		InnerBlock:  blockBytes,
		timestamp:   timestamp,
	}

	bytes, err := c.Marshal(codecVersion, &block)
	if err != nil {
		return nil, err
	}
	return block, block.initialize(bytes)
}

// blkIDFromBlkWithNoSignature is an helper to calculate block ID for blocks to be built
func blkIDFromBlkWithNoSignature(blockWithEmptySignature SignedBlock) (ids.ID, error) {
	unsignedBytesWithEmptySignature, err := c.Marshal(codecVersion, &blockWithEmptySignature)
	if err != nil {
		return ids.Empty, err
	}

	// The serialized form of the block is the unsignedBytes followed by the
	// signature, which is prefixed by a uint32. Because we are marshalling the
	// block with an empty signature, we only need to strip off the length
	// prefix to get the unsigned bytes.
	lenUnsignedBytes := len(unsignedBytesWithEmptySignature) - wrappers.IntLen
	unsignedBytes := unsignedBytesWithEmptySignature[:lenUnsignedBytes]
	return hashing.ComputeHash256Array(unsignedBytes), nil
}

func BuildCertSigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	cert *x509.Certificate,
	blockBytes []byte,
	chainID ids.ID,
	tlsSigner *crypto.TLSSigner,
) (SignedBlock, error) {
	block := &statelessCertSignedBlock{
		StatelessBlock: statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
		},
		Certificate: cert.Raw,
		InnerBlock:  blockBytes,

		timestamp: timestamp,
		cert:      cert,
		proposer:  ids.NodeIDFromCert(cert),
	}

	var blockIntf SignedBlock = block
	blkID, err := blkIDFromBlkWithNoSignature(blockIntf)
	if err != nil {
		return nil, err
	}
	block.id = blkID

	header, err := buildHeader(chainID, parentID, block.id)
	if err != nil {
		return nil, err
	}

	block.Signature, err = tlsSigner.Sign(header.Bytes())
	if err != nil {
		return nil, err
	}

	block.bytes, err = c.Marshal(codecVersion, &blockIntf)
	return block, err
}

func BuildBlsSigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	nodeID ids.NodeID,
	innerBlockBytes []byte,
	chainID ids.ID,
	blsSigner crypto.BLSSigner,
) (SignedBlock, error) {
	block := &statelessBlsSignedBlock{
		StatelessBlock: statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
		},
		BlockProposer: nodeID,
		InnerBlock:    innerBlockBytes,

		timestamp: timestamp,
	}
	var blockIntf SignedBlock = block
	blkID, err := blkIDFromBlkWithNoSignature(blockIntf)
	if err != nil {
		return nil, err
	}
	block.id = blkID

	header, err := buildHeader(chainID, parentID, block.id)
	if err != nil {
		return nil, err
	}

	block.Signature = blsSigner.Sign(header.Bytes())
	block.bytes, err = c.Marshal(codecVersion, &blockIntf)
	return block, err
}

func buildHeader(
	chainID ids.ID,
	parentID ids.ID,
	bodyID ids.ID,
) (Header, error) {
	header := statelessHeader{
		Chain:  chainID,
		Parent: parentID,
		Body:   bodyID,
	}

	bytes, err := c.Marshal(codecVersion, &header)
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

	bytes, err := c.Marshal(codecVersion, &block)
	if err != nil {
		return nil, err
	}
	return block, block.initialize(bytes)
}
