// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/common"
)

const MaxCodeHashesPerRequest = 5

var _ Request = LeafsRequest{}

// LeafsRequest is a request to receive trie leaves at specified Root within Start and End byte range
// Limit outlines maximum number of leaves to returns starting at Start
type LeafsRequest struct {
	Root    common.Hash `serialize:"true"`
	Account common.Hash `serialize:"true"`
	Start   []byte      `serialize:"true"`
	End     []byte      `serialize:"true"`
	Limit   uint16      `serialize:"true"`
}

func (l LeafsRequest) String() string {
	return fmt.Sprintf(
		"LeafsRequest(Root=%s, Account=%s, Start=%s, End %s, Limit=%d)",
		l.Root, l.Account, common.Bytes2Hex(l.Start), common.Bytes2Hex(l.End), l.Limit,
	)
}

func (l LeafsRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error) {
	return handler.HandleStateTrieLeafsRequest(ctx, nodeID, requestID, l)
}

// LeafsResponse is a response to a LeafsRequest
// Keys must be within LeafsRequest.Start and LeafsRequest.End and sorted in lexicographical order.
//
// ProofVals must be non-empty and contain a valid range proof unless the key-value pairs in the
// response are the entire trie.
// If the key-value pairs make up the entire trie, ProofVals should be empty since the root will be
// sufficient to prove that the leaves are included in the trie.
//
// More is a flag set in the client after verifying the response, which indicates if the last key-value
// pair in the response has any more elements to its right within the trie.
type LeafsResponse struct {
	// Keys and Vals provides the key-value pairs in the trie in the response.
	Keys [][]byte `serialize:"true"`
	Vals [][]byte `serialize:"true"`

	// More indicates if there are more leaves to the right of the last value in this response.
	//
	// This is not serialized since it is set in the client after verifying the response via
	// VerifyRangeProof and determining if there are in fact more leaves to the right of the
	// last value in this response.
	More bool

	// ProofVals contain the edge merkle-proofs for the range of keys included in the response.
	// The keys for the proof are simply the keccak256 hashes of the values, so they are not included in the response to save bandwidth.
	ProofVals [][]byte `serialize:"true"`
}
