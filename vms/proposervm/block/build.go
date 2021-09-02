// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func BuildUnsigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	blockBytes []byte,
) (Block, error) {
	block := statelessBlock{
		StatelessBlock: statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  nil,
			Block:        blockBytes,
		},
		timestamp: timestamp,
	}

	bytes, err := c.Marshal(version, &block)
	if err != nil {
		return nil, err
	}
	block.bytes = bytes

	block.id = hashing.ComputeHash256Array(bytes)
	return &block, nil
}

func Build(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	cert *x509.Certificate,
	blockBytes []byte,
	chainID ids.ID,
	key crypto.Signer,
) (Block, error) {
	block := statelessBlock{
		StatelessBlock: statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  cert.Raw,
			Block:        blockBytes,
		},
		timestamp: timestamp,
		cert:      cert,
		proposer:  hashing.ComputeHash160Array(hashing.ComputeHash256(cert.Raw)),
	}

	unsignedBytes, err := c.Marshal(version, &block.StatelessBlock)
	if err != nil {
		return nil, err
	}

	unsignedHash := hashing.ComputeHash256Array(unsignedBytes)
	header, err := BuildHeader(chainID, parentID, unsignedHash)
	if err != nil {
		return nil, err
	}

	headerHash := hashing.ComputeHash256(header.Bytes())
	block.Signature, err = key.Sign(rand.Reader, headerHash, crypto.SHA256)
	if err != nil {
		return nil, err
	}

	block.bytes, err = c.Marshal(version, &block)
	if err != nil {
		return nil, err
	}

	block.id = hashing.ComputeHash256Array(block.bytes)
	return &block, nil
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

	bytes, err := c.Marshal(version, &header)
	header.bytes = bytes
	return &header, err
}
