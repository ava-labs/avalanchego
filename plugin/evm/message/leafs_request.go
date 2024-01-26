// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const MaxCodeHashesPerRequest = 5

var _ Request = LeafsRequest{}

// NodeType outlines the trie that a leaf node belongs to
// handlers.LeafsRequestHandler uses this information to determine
// which of the two tries (state/atomic) to fetch the information from
type NodeType uint8

const (
	// StateTrieNode represents a leaf node that belongs to the coreth State trie
	StateTrieNode NodeType = iota + 1
	// AtomicTrieNode represents a leaf node that belongs to the coreth evm.AtomicTrie
	AtomicTrieNode
)

func (nt NodeType) String() string {
	switch nt {
	case StateTrieNode:
		return "StateTrie"
	case AtomicTrieNode:
		return "AtomicTrie"
	default:
		return "Unknown"
	}
}

// LeafsRequest is a request to receive trie leaves at specified Root within Start and End byte range
// Limit outlines maximum number of leaves to returns starting at Start
// NodeType outlines which trie to read from state/atomic.
type LeafsRequest struct {
	Root     common.Hash `serialize:"true"`
	Account  common.Hash `serialize:"true"`
	Start    []byte      `serialize:"true"`
	End      []byte      `serialize:"true"`
	Limit    uint16      `serialize:"true"`
	NodeType NodeType    `serialize:"true"`
}

func (l LeafsRequest) String() string {
	return fmt.Sprintf(
		"LeafsRequest(Root=%s, Account=%s, Start=%s, End=%s, Limit=%d, NodeType=%s)",
		l.Root, l.Account, common.Bytes2Hex(l.Start), common.Bytes2Hex(l.End), l.Limit, l.NodeType,
	)
}

func (l LeafsRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error) {
	switch l.NodeType {
	case StateTrieNode:
		return handler.HandleStateTrieLeafsRequest(ctx, nodeID, requestID, l)
	case AtomicTrieNode:
		return handler.HandleAtomicTrieLeafsRequest(ctx, nodeID, requestID, l)
	}

	log.Debug("node type is not recognised, dropping request", "nodeID", nodeID, "requestID", requestID, "nodeType", l.NodeType)
	return nil, nil
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
