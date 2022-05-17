// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// Maximum number of leaves to return in a message.LeafsResponse
// This parameter overrides any other Limit specified
// in message.LeafsRequest if it is greater than this value
const maxLeavesLimit = uint16(1024)

// LeafsRequestHandler is a peer.RequestHandler for types.LeafsRequest
// serving requested trie data
type LeafsRequestHandler struct {
	trieDB *trie.Database
	codec  codec.Manager
	stats  stats.LeafsRequestHandlerStats
}

func NewLeafsRequestHandler(trieDB *trie.Database, codec codec.Manager, syncerStats stats.LeafsRequestHandlerStats) *LeafsRequestHandler {
	return &LeafsRequestHandler{
		trieDB: trieDB,
		codec:  codec,
		stats:  syncerStats,
	}
}

// OnLeafsRequest returns encoded message.LeafsResponse for a given message.LeafsRequest
// Returns leaves with proofs for specified (Start-End) (both inclusive) ranges
// Returned message.LeafsResponse may contain partial leaves within requested Start and End range if:
// - ctx expired while fetching leafs
// - number of leaves read is greater than Limit (message.LeafsRequest)
// Specified Limit in message.LeafsRequest is overridden to maxLeavesLimit if it is greater than maxLeavesLimit
// Expects returned errors to be treated as FATAL
// Never returns errors
// Expects NodeType to be one of message.AtomicTrieNode or message.StateTrieNode
// Returns nothing if NodeType is invalid or requested trie root is not found
// Assumes ctx is active
func (lrh *LeafsRequestHandler) OnLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	startTime := time.Now()
	lrh.stats.IncLeafsRequest()

	if (len(leafsRequest.End) > 0 && bytes.Compare(leafsRequest.Start, leafsRequest.End) > 0) ||
		leafsRequest.Root == (common.Hash{}) ||
		leafsRequest.Root == types.EmptyRootHash ||
		leafsRequest.Limit == 0 {
		log.Debug("invalid leafs request, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest)
		lrh.stats.IncInvalidLeafsRequest()
		return nil, nil
	}

	t, err := trie.New(leafsRequest.Root, lrh.trieDB)
	if err != nil {
		log.Debug("error opening trie when processing request, dropping request", "nodeID", nodeID, "requestID", requestID, "root", leafsRequest.Root, "err", err)
		lrh.stats.IncMissingRoot()
		return nil, nil
	}

	// ensure metrics are captured properly on all return paths
	leafCount := uint16(0)
	defer func() {
		lrh.stats.UpdateLeafsRequestProcessingTime(time.Since(startTime))
		lrh.stats.UpdateLeafsReturned(leafCount)
	}()

	// create iterator to iterate the trie
	// Note that leafsRequest.Start could be an original start point
	// or leafsResponse.NextKey from partial response to previous request
	it := trie.NewIterator(t.NodeIterator(leafsRequest.Start))

	// override limit if it is greater than the configured maxLeavesLimit
	limit := leafsRequest.Limit
	if limit > maxLeavesLimit {
		limit = maxLeavesLimit
	}

	var leafsResponse message.LeafsResponse

	// more indicates whether there are more leaves in the trie
	more := false
	for it.Next() {
		// if we're at the end, break this loop
		if len(leafsRequest.End) > 0 && bytes.Compare(it.Key, leafsRequest.End) > 0 {
			more = true
			break
		}

		// If we've returned enough data or run out of time, set the more flag and exit
		// this flag will determine if the proof is generated or not
		if leafCount >= limit || ctx.Err() != nil {
			if leafCount == 0 {
				log.Debug("context err set before any leafs were iterated", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "ctxErr", ctx.Err())
				return nil, nil
			}
			more = true
			break
		}

		// collect data to return
		leafCount++
		leafsResponse.Keys = append(leafsResponse.Keys, it.Key)
		leafsResponse.Vals = append(leafsResponse.Vals, it.Value)
	}

	if it.Err != nil {
		log.Debug("failed to iterate trie, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", it.Err)
		lrh.stats.IncTrieError()
		return nil, nil
	}

	// only generate proof if we're not returning the full trie
	// we determine this based on if the starting point is nil and if the iterator
	// indicates that are more leaves in the trie.
	if len(leafsRequest.Start) > 0 || more {
		start := leafsRequest.Start
		// If [start] in the request is empty, populate it with the appropriate length
		// key starting at 0.
		if len(start) == 0 {
			keyLength, err := getKeyLength(leafsRequest.NodeType)
			if err != nil {
				// Note: LeafsRequest.Handle checks NodeType's validity so clients cannot cause the server to spam this error
				log.Error("Failed to get key length for leafs request", "err", err)
				return nil, nil
			}
			start = bytes.Repeat([]byte{0x00}, keyLength)
		}
		// If there is a non-zero number of keys, set [end] for the range proof to the
		// last key included in the response.
		end := leafsRequest.End
		if len(leafsResponse.Keys) > 0 {
			end = leafsResponse.Keys[len(leafsResponse.Keys)-1]
		}
		leafsResponse.ProofKeys, leafsResponse.ProofVals, err = GenerateRangeProof(t, start, end)
		// Generate the proof and add it to the response.
		if err != nil {
			log.Debug("failed to create valid proof serving leafs request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", err)
			lrh.stats.IncTrieError()
			return nil, nil
		}
	}

	responseBytes, err := lrh.codec.Marshal(message.Version, leafsResponse)
	if err != nil {
		log.Debug("failed to marshal LeafsResponse, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", err)
		return nil, nil
	}

	log.Debug("handled leafsRequest", "time", time.Since(startTime), "leafs", leafCount, "proofLen", len(leafsResponse.ProofKeys))
	return responseBytes, nil
}

// GenerateRangeProof returns the required proof key-values pairs for the range proof of
// [t] from [start, end].
func GenerateRangeProof(t *trie.Trie, start, end []byte) ([][]byte, [][]byte, error) {
	proof := memorydb.New()
	defer proof.Close() // Closing the memorydb should never error

	if err := t.Prove(start, 0, proof); err != nil {
		return nil, nil, err
	}

	if err := t.Prove(end, 0, proof); err != nil {
		return nil, nil, err
	}

	// dump proof into response
	proofIt := proof.NewIterator(nil, nil)
	defer proofIt.Release()

	keys := make([][]byte, 0, proof.Len())
	values := make([][]byte, 0, proof.Len())
	for proofIt.Next() {
		keys = append(keys, proofIt.Key())
		values = append(values, proofIt.Value())
	}

	return keys, values, proofIt.Error()
}

// getKeyLength returns trie key length for given nodeType
// expects nodeType to be one of message.AtomicTrieNode or message.StateTrieNode
func getKeyLength(nodeType message.NodeType) (int, error) {
	switch nodeType {
	case message.AtomicTrieNode:
		return wrappers.LongLen + common.HashLength, nil
	case message.StateTrieNode:
		return common.HashLength, nil
	}
	return 0, fmt.Errorf("cannot get key length for unknown node type: %s", nodeType)
}
