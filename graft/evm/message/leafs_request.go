// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
)

const MaxCodeHashesPerRequest = 5

var (
	_ LeafsRequest = SubnetEVMLeafsRequest{}
	_ LeafsRequest = CorethLeafsRequest{}
)

// NodeType outlines the trie that a leaf node belongs to
// handlers.LeafsRequestHandler uses this information to determine
// which trie type to fetch the information from
type NodeType uint8

const (
	StateTrieNode      = NodeType(1)
	StateTrieKeyLength = common.HashLength
)

// LeafsRequest defines the interface for leaf sync requests.
type LeafsRequest interface {
	Request
	RootHash() common.Hash
	AccountHash() common.Hash
	StartKey() []byte
	EndKey() []byte
	KeyLimit() uint16
	LeafType() NodeType
}

// LeafsRequestType selects which wire format to use when building leafs requests.
type LeafsRequestType int

const (
	CorethLeafsRequestType LeafsRequestType = iota
	SubnetEVMLeafsRequestType
)

// NewLeafsRequest builds a leafs request using the requested wire format.
func NewLeafsRequest(leafReqType LeafsRequestType, root, account common.Hash, start, end []byte, limit uint16, nodeType NodeType) (LeafsRequest, error) {
	switch leafReqType {
	case SubnetEVMLeafsRequestType:
		return SubnetEVMLeafsRequest{
			Root:     root,
			Account:  account,
			Start:    start,
			End:      end,
			Limit:    limit,
			NodeType: nodeType,
		}, nil
	case CorethLeafsRequestType:
		return CorethLeafsRequest{
			Root:     root,
			Account:  account,
			Start:    start,
			End:      end,
			Limit:    limit,
			NodeType: nodeType,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported leafs request type: %q", leafReqType)
	}
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
