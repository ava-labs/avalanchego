// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// Maximum number of leaves to return in a message.LeafsResponse
	// This parameter overrides any other Limit specified
	// in message.LeafsRequest if it is greater than this value
	maxLeavesLimit = uint16(1024)

	segmentLen = 64 // divide data from snapshot to segments of this size
)

// LeafsRequestHandler is a peer.RequestHandler for types.LeafsRequest
// serving requested trie data
type LeafsRequestHandler struct {
	trieDB           *trie.Database
	snapshotProvider SnapshotProvider
	codec            codec.Manager
	stats            stats.LeafsRequestHandlerStats
	pool             sync.Pool
}

func NewLeafsRequestHandler(trieDB *trie.Database, snapshotProvider SnapshotProvider, codec codec.Manager, syncerStats stats.LeafsRequestHandlerStats) *LeafsRequestHandler {
	return &LeafsRequestHandler{
		trieDB:           trieDB,
		snapshotProvider: snapshotProvider,
		codec:            codec,
		stats:            syncerStats,
		pool: sync.Pool{
			New: func() interface{} { return make([][]byte, 0, maxLeavesLimit) },
		},
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
	keyLength, err := getKeyLength(leafsRequest.NodeType)
	if err != nil {
		// Note: LeafsRequest.Handle checks NodeType's validity so clients cannot cause the server to spam this error
		log.Error("Failed to get key length for leafs request", "err", err)
		lrh.stats.IncInvalidLeafsRequest()
		return nil, nil
	}
	if len(leafsRequest.Start) != 0 && len(leafsRequest.Start) != keyLength ||
		len(leafsRequest.End) != 0 && len(leafsRequest.End) != keyLength {
		log.Debug("invalid length for leafs request range, dropping request", "startLen", len(leafsRequest.Start), "endLen", len(leafsRequest.End), "expected", keyLength)
		lrh.stats.IncInvalidLeafsRequest()
		return nil, nil
	}

	t, err := trie.New(leafsRequest.Root, lrh.trieDB)
	if err != nil {
		log.Debug("error opening trie when processing request, dropping request", "nodeID", nodeID, "requestID", requestID, "root", leafsRequest.Root, "err", err)
		lrh.stats.IncMissingRoot()
		return nil, nil
	}
	// override limit if it is greater than the configured maxLeavesLimit
	limit := leafsRequest.Limit
	if limit > maxLeavesLimit {
		limit = maxLeavesLimit
	}

	var leafsResponse message.LeafsResponse
	// pool response's key/val allocations
	leafsResponse.Keys = lrh.pool.Get().([][]byte)
	leafsResponse.Vals = lrh.pool.Get().([][]byte)
	defer func() {
		for i := range leafsResponse.Keys {
			// clear out slices before returning them to the pool
			// to avoid memory leak.
			leafsResponse.Keys[i] = nil
			leafsResponse.Vals[i] = nil
		}
		lrh.pool.Put(leafsResponse.Keys[:0])
		lrh.pool.Put(leafsResponse.Vals[:0])
	}()

	responseBuilder := &responseBuilder{
		request:   &leafsRequest,
		response:  &leafsResponse,
		t:         t,
		keyLength: keyLength,
		limit:     limit,
		stats:     lrh.stats,
	}
	// pass snapshot to responseBuilder if non-nil snapshot getter provided
	if lrh.snapshotProvider != nil {
		responseBuilder.snap = lrh.snapshotProvider.Snapshots()
	}
	err = responseBuilder.handleRequest(ctx)

	// ensure metrics are captured properly on all return paths
	defer func() {
		lrh.stats.UpdateLeafsRequestProcessingTime(time.Since(startTime))
		lrh.stats.UpdateLeafsReturned(uint16(len(leafsResponse.Keys)))
		lrh.stats.UpdateRangeProofKeysReturned(int64(len(leafsResponse.ProofKeys)))
		lrh.stats.UpdateGenerateRangeProofTime(responseBuilder.proofTime)
		lrh.stats.UpdateReadLeafsTime(responseBuilder.trieReadTime)
	}()
	if err != nil {
		log.Debug("failed to serve leafs request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", err)
		return nil, nil
	}
	if len(leafsResponse.Keys) == 0 && ctx.Err() != nil {
		log.Debug("context err set before any leafs were iterated", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "ctxErr", ctx.Err())
		return nil, nil
	}

	responseBytes, err := lrh.codec.Marshal(message.Version, leafsResponse)
	if err != nil {
		log.Debug("failed to marshal LeafsResponse, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest, "err", err)
		return nil, nil
	}

	log.Debug("handled leafsRequest", "time", time.Since(startTime), "leafs", len(leafsResponse.Keys), "proofLen", len(leafsResponse.ProofKeys))
	return responseBytes, nil
}

type responseBuilder struct {
	request   *message.LeafsRequest
	response  *message.LeafsResponse
	t         *trie.Trie
	snap      *snapshot.Tree
	keyLength int
	limit     uint16

	// stats
	trieReadTime time.Duration
	proofTime    time.Duration
	stats        stats.LeafsRequestHandlerStats
}

func (rb *responseBuilder) handleRequest(ctx context.Context) error {
	// Read from snapshot if a [snapshot.Tree] was provided in initialization
	if rb.snap != nil {
		if done, err := rb.fillFromSnapshot(ctx); err != nil {
			return err
		} else if done {
			return nil
		}
	}

	if len(rb.response.Keys) < int(rb.limit) {
		// more indicates whether there are more leaves in the trie
		more, err := rb.fillFromTrie(ctx, rb.request.End)
		if err != nil {
			rb.stats.IncTrieError()
			return err
		}
		if len(rb.request.Start) == 0 && !more {
			// omit proof via early return
			return nil
		}
	}

	// Generate the proof and add it to the response.
	proof, err := rb.generateRangeProof(rb.request.Start, rb.response.Keys)
	if err != nil {
		rb.stats.IncProofError()
		return err
	}
	defer proof.Close() // closing memdb does not error

	rb.response.ProofKeys, rb.response.ProofVals, err = iterateKeyVals(proof)
	if err != nil {
		rb.stats.IncProofError()
		return err
	}
	return nil
}

// fillFromSnapshot reads data from snapshot and returns true if the response is complete
// (otherwise the trie must be iterated further and a range proof may be needed)
func (rb *responseBuilder) fillFromSnapshot(ctx context.Context) (bool, error) {
	snapshotReadStart := time.Now()
	rb.stats.IncSnapshotReadAttempt()

	// Optimistically read leafs from the snapshot, assuming they have not been
	// modified since the requested root. If this assumption can be verified with
	// range proofs and data from the trie, we can skip iterating the trie as
	// an optimization.
	snapKeys, snapVals, more, err := rb.readLeafsFromSnapshot(ctx)
	// Update read snapshot time here, so that we include the case that an error occurred.
	rb.stats.UpdateSnapshotReadTime(time.Since(snapshotReadStart))
	if err != nil {
		rb.stats.IncSnapshotReadError()
		return false, err
	}

	// Check if the entire range read from the snapshot is valid according to the trie.
	proof, ok, err := rb.isRangeValid(snapKeys, snapVals, false)
	if err != nil {
		rb.stats.IncProofError()
		return false, err
	}
	defer proof.Close() // closing memdb does not error
	if ok {
		rb.response.Keys, rb.response.Vals = snapKeys, snapVals
		if len(rb.request.Start) == 0 && !more {
			// omit proof via early return
			rb.stats.IncSnapshotReadSuccess()
			return true, nil
		}
		rb.response.ProofKeys, rb.response.ProofVals, err = iterateKeyVals(proof)
		if err != nil {
			rb.stats.IncProofError()
			return false, err
		}
		rb.stats.IncSnapshotReadSuccess()
		return true, nil
	}
	// The data from the snapshot could not be validated as a whole. It is still likely
	// most of the data from the snapshot is useable, so we try to validate smaller
	// segments of the data and use them in the response.
	hasGap := false
	for i := 0; i < len(snapKeys); i += segmentLen {
		segmentEnd := math.Min(i+segmentLen, len(snapKeys))
		proof, ok, err := rb.isRangeValid(snapKeys[i:segmentEnd], snapVals[i:segmentEnd], hasGap)
		if err != nil {
			rb.stats.IncProofError()
			return false, err
		}
		_ = proof.Close() // we don't need this proof
		if !ok {
			// segment is not valid
			rb.stats.IncSnapshotSegmentInvalid()
			hasGap = true
			continue
		}

		// segment is valid
		rb.stats.IncSnapshotSegmentValid()
		if hasGap {
			// if there is a gap between valid segments, fill the gap with data from the trie
			_, err := rb.fillFromTrie(ctx, snapKeys[i])
			if err != nil {
				rb.stats.IncTrieError()
				return false, err
			}
			if len(rb.response.Keys) >= int(rb.limit) || ctx.Err() != nil {
				break
			}
			// remove the last key added since it is snapKeys[i] and will be added back
			// Note: this is safe because we were able to verify the range proof that
			// shows snapKeys[i] is part of the trie.
			rb.response.Keys = rb.response.Keys[:len(rb.response.Keys)-1]
			rb.response.Vals = rb.response.Vals[:len(rb.response.Vals)-1]
		}
		hasGap = false
		// all the key/vals in the segment are valid, but possibly shorten segmentEnd
		// here to respect limit. this is necessary in case the number of leafs we read
		// from the trie is more than the length of a segment which cannot be validated. limit
		segmentEnd = math.Min(segmentEnd, i+int(rb.limit)-len(rb.response.Keys))
		rb.response.Keys = append(rb.response.Keys, snapKeys[i:segmentEnd]...)
		rb.response.Vals = append(rb.response.Vals, snapVals[i:segmentEnd]...)

		if len(rb.response.Keys) >= int(rb.limit) {
			break
		}
	}
	return false, nil
}

// generateRangeProof returns a range proof for the range specified by [start] and [keys] using [t].
func (rb *responseBuilder) generateRangeProof(start []byte, keys [][]byte) (*memorydb.Database, error) {
	proof := memorydb.New()
	startTime := time.Now()
	defer func() { rb.proofTime += time.Since(startTime) }()

	// If [start] is empty, populate it with the appropriate length key starting at 0.
	if len(start) == 0 {
		start = bytes.Repeat([]byte{0x00}, rb.keyLength)
	}

	if err := rb.t.Prove(start, 0, proof); err != nil {
		_ = proof.Close() // closing memdb does not error
		return nil, err
	}
	if len(keys) > 0 {
		// If there is a non-zero number of keys, set [end] for the range proof to the last key.
		end := keys[len(keys)-1]
		if err := rb.t.Prove(end, 0, proof); err != nil {
			_ = proof.Close() // closing memdb does not error
			return nil, err
		}
	}
	return proof, nil
}

// verifyRangeProof verifies the provided range proof with [keys/vals], starting at [start].
// Returns nil on success.
func (rb *responseBuilder) verifyRangeProof(keys, vals [][]byte, start []byte, proof *memorydb.Database) error {
	startTime := time.Now()
	defer func() { rb.proofTime += time.Since(startTime) }()

	// If [start] is empty, populate it with the appropriate length key starting at 0.
	if len(start) == 0 {
		start = bytes.Repeat([]byte{0x00}, rb.keyLength)
	}
	var end []byte
	if len(keys) > 0 {
		end = keys[len(keys)-1]
	}
	_, err := trie.VerifyRangeProof(rb.request.Root, start, end, keys, vals, proof)
	return err
}

// iterateKeyVals returns the key-value pairs contained in [db]
func iterateKeyVals(db *memorydb.Database) ([][]byte, [][]byte, error) {
	if db == nil {
		return nil, nil, nil
	}
	// iterate db into [][]byte and return
	it := db.NewIterator(nil, nil)
	defer it.Release()

	keys := make([][]byte, 0, db.Len())
	vals := make([][]byte, 0, db.Len())
	for it.Next() {
		keys = append(keys, it.Key())
		vals = append(vals, it.Value())
	}

	return keys, vals, it.Error()
}

// isRangeValid generates and verifies a range proof, returning true if keys/vals are
// part of the trie. If [hasGap] is true, the range is validated independent of the
// existing response. If [hasGap] is false, the range proof begins at a key which
// guarantees the range can be appended to the response.
func (rb *responseBuilder) isRangeValid(keys, vals [][]byte, hasGap bool) (*memorydb.Database, bool, error) {
	var startKey []byte
	if hasGap {
		startKey = keys[0]
	} else {
		startKey = rb.nextKey()
	}

	proof, err := rb.generateRangeProof(startKey, keys)
	if err != nil {
		return nil, false, err
	}
	return proof, rb.verifyRangeProof(keys, vals, startKey, proof) == nil, nil
}

// nextKey returns the nextKey that could potentially be part of the response.
func (rb *responseBuilder) nextKey() []byte {
	if len(rb.response.Keys) == 0 {
		return rb.request.Start
	}
	nextKey := common.CopyBytes(rb.response.Keys[len(rb.response.Keys)-1])
	utils.IncrOne(nextKey)
	return nextKey
}

// fillFromTrie iterates key/values from the response builder's trie and appends
// them to the response. Iteration begins from the last key already in the response,
// or the request start if the response is empty. Iteration ends at [end] or if
// the number of leafs reaches the builder's limit.
// Returns true if there are more keys in the trie.
func (rb *responseBuilder) fillFromTrie(ctx context.Context, end []byte) (bool, error) {
	startTime := time.Now()
	defer func() { rb.trieReadTime += time.Since(startTime) }()

	// create iterator to iterate the trie
	it := trie.NewIterator(rb.t.NodeIterator(rb.nextKey()))
	more := false
	for it.Next() {
		// if we're at the end, break this loop
		if len(end) > 0 && bytes.Compare(it.Key, end) > 0 {
			more = true
			break
		}

		// If we've returned enough data or run out of time, set the more flag and exit
		// this flag will determine if the proof is generated or not
		if len(rb.response.Keys) >= int(rb.limit) || ctx.Err() != nil {
			more = true
			break
		}

		// append key/vals to the response
		rb.response.Keys = append(rb.response.Keys, it.Key)
		rb.response.Vals = append(rb.response.Vals, it.Value)
	}
	return more, it.Err
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

// readLeafsFromSnapshot iterates the storage snapshot of the requested account
// (or the main account trie if account is empty). Returns up to [rb.limit] key/value
// pairs with for keys that are in the request's range (inclusive), and a boolean
// indicating if there are more keys in the snapshot.
func (rb *responseBuilder) readLeafsFromSnapshot(ctx context.Context) ([][]byte, [][]byte, bool, error) {
	var (
		snapIt    ethdb.Iterator
		startHash = common.BytesToHash(rb.request.Start)
		more      = false
		keys      = make([][]byte, 0, rb.limit)
		vals      = make([][]byte, 0, rb.limit)
	)

	// Get an iterator into the storage or the main account snapshot.
	if rb.request.Account == (common.Hash{}) {
		snapIt = &accountIt{AccountIterator: rb.snap.DiskAccountIterator(startHash)}
	} else {
		snapIt = &storageIt{StorageIterator: rb.snap.DiskStorageIterator(rb.request.Account, startHash)}
	}
	defer snapIt.Release()
	for snapIt.Next() {
		// if we're at the end, break this loop
		if len(rb.request.End) > 0 && bytes.Compare(snapIt.Key(), rb.request.End) > 0 {
			more = true
			break
		}
		// If we've returned enough data or run out of time, set the more flag and exit
		// this flag will determine if the proof is generated or not
		if len(keys) >= int(rb.limit) || ctx.Err() != nil {
			more = true
			break
		}

		keys = append(keys, snapIt.Key())
		vals = append(vals, snapIt.Value())
	}
	return keys, vals, more, snapIt.Error()
}
