// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

type ParseResult struct {
	Block Block
	Err   error
}

// ParseBlocks parses the given raw blocks into tuples of (Block, error).
// Each ParseResult is returned in the same order as its corresponding bytes in the input.
func ParseBlocks(blks [][]byte, chainID ids.ID) []ParseResult {
	results := make([]ParseResult, len(blks))

	var wg sync.WaitGroup
	wg.Add(len(blks))

	for i, blk := range blks {
		go func(i int, blkBytes []byte) {
			defer wg.Done()
			results[i].Block, results[i].Err = Parse(blkBytes, chainID)
		}(i, blk)
	}

	wg.Wait()

	return results
}

// Parse a block and verify that the signature attached to the block is valid
// for the certificate provided in the block and that the block has a valid
// representation.
func Parse(bytes []byte, chainID ids.ID) (Block, error) {
	block, err := ParseWithoutVerification(bytes)
	if err != nil {
		return nil, err
	}
	return block, block.verify(chainID)
}

// ParseWithoutVerification parses a block without verifying that the signature
// on the block is correct or has valid representation.
func ParseWithoutVerification(bytes []byte) (Block, error) {
	var block Block
	parsedVersion, err := Codec.Unmarshal(bytes, &block)
	if err != nil {
		return nil, err
	}
	if parsedVersion != CodecVersion {
		return nil, fmt.Errorf("expected codec version %d but got %d", CodecVersion, parsedVersion)
	}
	return block, block.initialize(bytes)
}

func ParseHeader(bytes []byte) (Header, error) {
	header := statelessHeader{}
	parsedVersion, err := Codec.Unmarshal(bytes, &header)
	if err != nil {
		return nil, err
	}
	if parsedVersion != CodecVersion {
		return nil, fmt.Errorf("expected codec version %d but got %d", CodecVersion, parsedVersion)
	}
	header.bytes = bytes
	return &header, nil
}
