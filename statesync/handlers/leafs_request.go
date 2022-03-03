// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"time"

	"github.com/ava-labs/coreth/core/types"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/statesync/handlers/stats"
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
	stats  stats.HandlerStats
	codec  codec.Manager
}

func NewLeafsRequestHandler(trieDB *trie.Database, syncerStats stats.HandlerStats, codec codec.Manager) *LeafsRequestHandler {
	return &LeafsRequestHandler{
		trieDB: trieDB,
		stats:  syncerStats,
		codec:  codec,
	}
}

// OnLeafsRequest returns encoded message.LeafsResponse for a given message.LeafsRequest
// Returned message.LeafsResponse may contain partial leaves within requested Start and End range if:
// - ctx expired while fetching leafs
// - number of leaves read is greater than Limit (message.LeafsRequest)
// Specified Limit in message.LeafsRequest is overridden to maxLeavesLimit if it is greater than maxLeavesLimit
// Expects returned errors to be treated as FATAL
// Never returns errors
// Returns nothing if NodeType is invalid or requested trie root is not found
// Assumes ctx is active
func (lrh *LeafsRequestHandler) OnLeafsRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	startTime := time.Now()
	lrh.stats.IncLeafsRequest()

	leafCount := uint16(0)

	if bytes.Compare(leafsRequest.Start, leafsRequest.End) >= 0 ||
		leafsRequest.Root == (common.Hash{}) ||
		leafsRequest.Root == types.EmptyRootHash ||
		leafsRequest.Limit == 0 {
		log.Debug("invalid leafs request, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest)
		return nil, nil
	}

	t, err := trie.New(leafsRequest.Root, lrh.trieDB)
	if err != nil {
		log.Debug("error opening trie when processing request, dropping request", "nodeID", nodeID, "requestID", requestID, "root", leafsRequest.Root, "err", err)
		lrh.stats.IncMissingRoot()
		return nil, nil
	}

	// ensure metrics are captured properly on all return paths
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
	for it.Next() {
		// if we're at the end, break this loop
		if bytes.Compare(it.Key, leafsRequest.End) > 0 {
			break
		}

		// If we've returned enough data or run out of time, set the more flag and exit
		// this flag will determine where the proof ends
		if leafCount >= limit || ctx.Err() != nil {
			if leafCount == 0 {
				log.Debug("context err set before any leafs were iterated", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "ctxErr", ctx.Err())
				return nil, nil
			}
			break
		}

		// collect data to return
		leafCount++
		leafsResponse.Keys = append(leafsResponse.Keys, it.Key)
		leafsResponse.Vals = append(leafsResponse.Vals, it.Value)
	}

	if it.Err != nil {
		log.Debug("failed to iterate trie, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", it.Err)
		return nil, nil
	}

	// Generate the proof and add it to the response.
	if err := lrh.addProofKeys(t, &leafsRequest, &leafsResponse); err != nil {
		log.Debug("failed to create valid proof serving leafs request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", err)
		return nil, nil
	}

	responseBytes, err := lrh.codec.Marshal(message.Version, leafsResponse)
	if err != nil {
		log.Debug("failed to marshal LeafsResponse, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", err)
		return nil, nil
	}

	log.Debug("handled leafsRequest", "time", time.Since(startTime), "leafs", leafCount, "proofLen", len(leafsResponse.ProofKeys))
	return responseBytes, nil
}

func (lrh *LeafsRequestHandler) addProofKeys(t *trie.Trie, leafsRequest *message.LeafsRequest, leafsResponse *message.LeafsResponse) error {
	proof := memorydb.New()
	defer proof.Close() // Closing the memorydb should never error

	if err := t.Prove(leafsRequest.Start, 0, proof); err != nil {
		return err
	}

	// Set the end of the range proof to the right edge of the leaf request.
	end := leafsRequest.End
	// If there is a non-zero number of keys, set the end of the range proof to be the last
	// key-value pair in the response instead.
	if len(leafsResponse.Keys) > 0 {
		end = leafsResponse.Keys[len(leafsResponse.Keys)-1]
	}
	if err := t.Prove(end, 0, proof); err != nil {
		return err
	}

	// dump proof into response
	proofIt := proof.NewIterator(nil, nil)
	defer proofIt.Release()

	for proofIt.Next() {
		leafsResponse.ProofKeys = append(leafsResponse.ProofKeys, proofIt.Key())
		leafsResponse.ProofVals = append(leafsResponse.ProofVals, proofIt.Value())
	}

	return proofIt.Error()
}
