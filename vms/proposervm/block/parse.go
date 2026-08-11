// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// parentIDOffset is the byte offset of a block's parent ID within its
	// serialized form: a codec version followed by the type ID assigned by
	// [Codec].
	//
	// Every type registered with [Codec] serializes its parent ID as its first
	// field, so this offset does not depend on which type the bytes hold.
	// TestParentIDOffset enforces that invariant against all registered types.
	parentIDOffset = wrappers.ShortLen + wrappers.IntLen
	parentIDEnd    = parentIDOffset + ids.IDLen
)

var errTooShortForParentID = errors.New("insufficient bytes to contain a parent ID")

// ParentID returns the parent ID of a serialized block without decoding the
// block.
//
// It exists for callers that walk a chain of blocks but only need their bytes -
// notably serving GetAncestors - and lets them skip the reflection-based
// decoding, ID hashing, and X.509 certificate parsing that
// [ParseWithoutVerification] performs.
//
// The bytes are assumed to have been produced by [Codec]. ParentID validates
// the codec version and the length, but not the block's structure; callers
// handling untrusted bytes must use [Parse].
func ParentID(b []byte) (ids.ID, error) {
	if len(b) < parentIDEnd {
		return ids.Empty, fmt.Errorf("%w: got %d bytes, need %d", errTooShortForParentID, len(b), parentIDEnd)
	}
	if version := binary.BigEndian.Uint16(b); version != CodecVersion {
		return ids.Empty, fmt.Errorf("expected codec version %d but got %d", CodecVersion, version)
	}
	return ids.ID(b[parentIDOffset:parentIDEnd]), nil
}

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
