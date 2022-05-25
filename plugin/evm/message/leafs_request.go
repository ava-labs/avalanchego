// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
)

var _ Request = LeafsRequest{}

// LeafsRequest is a request to receive trie leaves at specified Root within Start and End byte range
// Limit outlines maximum number of leaves to returns starting at Start
type LeafsRequest struct {
	Root  common.Hash `serialize:"true"`
	Start []byte      `serialize:"true"`
	End   []byte      `serialize:"true"`
	Limit uint16      `serialize:"true"`
}

func (l LeafsRequest) String() string {
	return fmt.Sprintf(
		"LeafsRequest(Root=%s, Start=%s, End %s, Limit=%d)",
		l.Root, common.Bytes2Hex(l.Start), common.Bytes2Hex(l.End), l.Limit,
	)
}

func (l LeafsRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error) {
	return handler.HandleTrieLeafsRequest(ctx, nodeID, requestID, l)
}

// LeafsResponse is a response to a LeafsRequest
// Keys must be within LeafsRequest.Start and LeafsRequest.End and sorted in lexicographical order.
//
// ProofKeys and ProofVals are expected to be non-nil and valid range proofs if the key-value pairs
// in the response are not the entire trie.
// If the key-value pairs make up the entire trie, ProofKeys and ProofVals should be empty since the
// root will be sufficient to prove that the leaves are included in the trie.
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

	// ProofKeys and ProofVals are the key-value pairs used in the range proof of the provided key-value
	// pairs.
	ProofKeys [][]byte `serialize:"true"`
	ProofVals [][]byte `serialize:"true"`
}
