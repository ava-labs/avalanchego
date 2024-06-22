// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// Parse a block and verify that the signature attached to the block is valid
// for the certificate provided in the block.
func Parse(bytes []byte, chainID ids.ID) (Block, error) {
	block, err := ParseWithoutVerification(bytes)
	if err != nil {
		return nil, err
	}
	return block, block.verify(chainID)
}

// ParseWithoutVerification parses a block without verifying that the signature
// on the block is correct.
func ParseWithoutVerification(bytes []byte) (Block, error) {
	var block Block
	parsedVersion, err := Codec.Unmarshal(bytes, &block)
	if err != nil {
		return nil, err
	}
	switch parsedVersion {
	case PreBlockSigCodecVersion:
		switch typedBlock := block.(type) {
		case *preBlockSigStatelessBlock:
			// we've received an instance of preBlockSigStatelessBlock, which we need to populate into statelessBlock
			block = &statelessBlock{
				StatelessBlock: statelessUnsignedBlock{
					ParentID:     typedBlock.StatelessBlock.ParentID,
					Timestamp:    typedBlock.StatelessBlock.Timestamp,
					PChainHeight: typedBlock.StatelessBlock.PChainHeight,
					Certificate:  typedBlock.StatelessBlock.Certificate,
					Block:        typedBlock.StatelessBlock.Block,
				},
				Signature: typedBlock.Signature,
			}
		case *option:
			// we're good to go.
		default:
			panic(nil)
		}
	case CurrentCodecVersion:
		// great, nothing to do here.
	default:
		return nil, fmt.Errorf("expected codec version %d or %d but got %d", CurrentCodecVersion, PreBlockSigCodecVersion, parsedVersion)
	}
	return block, block.initialize(bytes)
}

func ParseHeader(bytes []byte) (Header, error) {
	header := statelessHeader{}
	parsedVersion, err := Codec.Unmarshal(bytes, &header)
	if err != nil {
		return nil, err
	}

	if parsedVersion != PreBlockSigCodecVersion {
		return nil, fmt.Errorf("expected codec version %d but got %d", PreBlockSigCodecVersion, parsedVersion)
	}
	header.bytes = bytes
	return &header, nil
}
