// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
	"github.com/ava-labs/avalanchego/graft/evm/sync/syncutils"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/ids"
)

var _ LeafRequestHandler = (*leafsRequestHandler)(nil)

// LeafRequestHandler handles incoming leaf requests from peers.
type LeafRequestHandler interface {
	OnLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error)
}

// SnapshotProvider provides access to the snapshot tree.
type SnapshotProvider interface {
	Snapshots() *snapshot.Tree
}

const (
	// Maximum number of leaves to return in a message.LeafsResponse
	// This parameter overrides any other Limit specified
	// in message.LeafsRequest if it is greater than this value
	maxLeavesLimit = uint16(1024)

	// Maximum percent of the time left to deadline to spend on optimistically
	// reading the snapshot to find the response
	maxSnapshotReadTimePercent = 75

	segmentLen = 64 // divide data from snapshot to segments of this size
)

// leafsRequestHandler is a peer.RequestHandler for types.LeafsRequest
// serving requested trie data
type leafsRequestHandler struct {
	trieDB           *triedb.Database
	snapshotProvider SnapshotProvider
	codec            codec.Manager
	stats            stats.LeafsRequestHandlerStats
	trieKeyLength    int
}

func NewLeafsRequestHandler(trieDB *triedb.Database, trieKeyLength int, snapshotProvider SnapshotProvider, codec codec.Manager, syncerStats stats.LeafsRequestHandlerStats) *leafsRequestHandler {
	return &leafsRequestHandler{
		trieDB:           trieDB,
		snapshotProvider: snapshotProvider,
		codec:            codec,
		stats:            syncerStats,
		trieKeyLength:    trieKeyLength,
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
// Returns nothing if NodeType is invalid or requested trie root is not found
// Assumes ctx is active
func (lrh *leafsRequestHandler) OnLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	startTime := time.Now()
	lrh.stats.IncLeafsRequest()

	startKey := leafsRequest.StartKey()
	endKey := leafsRequest.EndKey()
	root := leafsRequest.RootHash()

	if (len(endKey) > 0 && bytes.Compare(startKey, endKey) > 0) ||
		root == (common.Hash{}) ||
		root == types.EmptyRootHash ||
		leafsRequest.KeyLimit() == 0 {
		log.Debug("invalid leafs request, dropping request", "nodeID", nodeID, "requestID", requestID, "request", leafsRequest)
		lrh.stats.IncInvalidLeafsRequest()
		return nil, nil
	}
	if (len(startKey) != 0 && len(startKey) != lrh.trieKeyLength) ||
		(len(endKey) != 0 && len(endKey) != lrh.trieKeyLength) {
		log.Debug("invalid length for leafs request range, dropping request", "startLen", len(startKey), "endLen", len(endKey), "expected", lrh.trieKeyLength)
		lrh.stats.IncInvalidLeafsRequest()
		return nil, nil
	}

	// TODO: We should know the state root that accounts correspond to,
	// as this information will be necessary to access storage tries when
	// the trie is path based.
	// stateRoot := common.Hash{}
	t, err := trie.New(trie.TrieID(root), lrh.trieDB)
	if err != nil {
		log.Debug("error opening trie when processing request, dropping request", "nodeID", nodeID, "requestID", requestID, "root", root, "err", err)
		lrh.stats.IncMissingRoot()
		return nil, nil
	}
	// override limit if it is greater than the configured maxLeavesLimit
	limit := leafsRequest.KeyLimit()
	if limit > maxLeavesLimit {
		limit = maxLeavesLimit
	}

	var leafsResponse message.LeafsResponse
	leafsResponse.Keys = make([][]byte, 0, limit)
	leafsResponse.Vals = make([][]byte, 0, limit)

	responseBuilder := &responseBuilder{
		request:   leafsRequest,
		response:  &leafsResponse,
		t:         t,
		keyLength: lrh.trieKeyLength,
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
		lrh.stats.UpdateRangeProofValsReturned(int64(len(leafsResponse.ProofVals)))
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

	log.Debug("handled leafsRequest", "time", time.Since(startTime), "leafs", len(leafsResponse.Keys), "proofLen", len(leafsResponse.ProofVals))
	return responseBytes, nil
}

type responseBuilder struct {
	request   message.LeafsRequest
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
		// reset the proof if we will iterate the trie further
		rb.response.ProofVals = nil
	}

	if len(rb.response.Keys) < int(rb.limit) {
		// more indicates whether there are more leaves in the trie
		more, err := rb.fillFromTrie(ctx, rb.request.EndKey())
		if err != nil {
			rb.stats.IncTrieError()
			return err
		}
		if len(rb.request.StartKey()) == 0 && !more {
			// omit proof via early return
			return nil
		}
	}

	// Generate the proof and add it to the response.
	proof, err := rb.generateRangeProof(rb.request.StartKey(), rb.response.Keys)
	if err != nil {
		rb.stats.IncProofError()
		return err
	}
	defer proof.Close() // closing memdb does not error

	rb.response.ProofVals, err = iterateVals(proof)
	if err != nil {
		rb.stats.IncProofError()
		return err
	}
	return nil
}

// fillFromSnapshot reads data from snapshot and returns true if the response is complete.
// Otherwise, the caller should attempt to iterate the trie and determine if a range proof
// should be added to the response.
func (rb *responseBuilder) fillFromSnapshot(ctx context.Context) (bool, error) {
	snapshotReadStart := time.Now()
	rb.stats.IncSnapshotReadAttempt()

	// Optimistically read leafs from the snapshot, assuming they have not been
	// modified since the requested root. If this assumption can be verified with
	// range proofs and data from the trie, we can skip iterating the trie as
	// an optimization.
	// Since we are performing this read optimistically, we use a separate context
	// with reduced timeout so there is enough time to read the trie if the snapshot
	// read does not contain up-to-date data.
	snapCtx := ctx
	if deadline, ok := ctx.Deadline(); ok {
		timeTillDeadline := time.Until(deadline)
		bufferedDeadline := time.Now().Add(timeTillDeadline * maxSnapshotReadTimePercent / 100)

		var cancel context.CancelFunc
		snapCtx, cancel = context.WithDeadline(ctx, bufferedDeadline)
		defer cancel()
	}
	snapKeys, snapVals, err := rb.readLeafsFromSnapshot(snapCtx)
	// Update read snapshot time here, so that we include the case that an error occurred.
	rb.stats.UpdateSnapshotReadTime(time.Since(snapshotReadStart))
	if err != nil {
		rb.stats.IncSnapshotReadError()
		return false, err
	}

	// Check if the entire range read from the snapshot is valid according to the trie.
	proof, ok, more, err := rb.isRangeValid(snapKeys, snapVals, false)
	if err != nil {
		rb.stats.IncProofError()
		return false, err
	}
	defer proof.Close() // closing memdb does not error
	if ok {
		rb.response.Keys, rb.response.Vals = snapKeys, snapVals
		if len(rb.request.StartKey()) == 0 && !more {
			// omit proof via early return
			rb.stats.IncSnapshotReadSuccess()
			return true, nil
		}
		rb.response.ProofVals, err = iterateVals(proof)
		if err != nil {
			rb.stats.IncProofError()
			return false, err
		}
		rb.stats.IncSnapshotReadSuccess()
		return !more, nil
	}
	// The data from the snapshot could not be validated as a whole. It is still likely
	// most of the data from the snapshot is useable, so we try to validate smaller
	// segments of the data and use them in the response.
	hasGap := false
	for i := 0; i < len(snapKeys); i += segmentLen {
		segmentEnd := min(i+segmentLen, len(snapKeys))
		proof, ok, _, err := rb.isRangeValid(snapKeys[i:segmentEnd], snapVals[i:segmentEnd], hasGap)
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
		segmentEnd = min(segmentEnd, i+int(rb.limit)-len(rb.response.Keys))
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

	if err := rb.t.Prove(start, proof); err != nil {
		_ = proof.Close() // closing memdb does not error
		return nil, err
	}
	if len(keys) > 0 {
		// If there is a non-zero number of keys, set [end] for the range proof to the last key.
		end := keys[len(keys)-1]
		if err := rb.t.Prove(end, proof); err != nil {
			_ = proof.Close() // closing memdb does not error
			return nil, err
		}
	}
	return proof, nil
}

// verifyRangeProof verifies the provided range proof with [keys/vals], starting at [start].
// Returns a boolean indicating if there are more leaves to the right of the last key in the trie and a nil error if the range proof is successfully verified.
func (rb *responseBuilder) verifyRangeProof(keys, vals [][]byte, start []byte, proof *memorydb.Database) (bool, error) {
	startTime := time.Now()
	defer func() { rb.proofTime += time.Since(startTime) }()

	// If [start] is empty, populate it with the appropriate length key starting at 0.
	if len(start) == 0 {
		start = bytes.Repeat([]byte{0x00}, rb.keyLength)
	}
	return trie.VerifyRangeProof(rb.request.RootHash(), start, keys, vals, proof)
}

// iterateVals returns the values contained in [db]
func iterateVals(db *memorydb.Database) ([][]byte, error) {
	if db == nil {
		return nil, nil
	}
	// iterate db into [][]byte and return
	it := db.NewIterator(nil, nil)
	defer it.Release()

	vals := make([][]byte, 0, db.Len())
	for it.Next() {
		vals = append(vals, it.Value())
	}

	return vals, it.Error()
}

// isRangeValid generates and verifies a range proof, returning true if keys/vals are
// part of the trie. If [hasGap] is true, the range is validated independent of the
// existing response. If [hasGap] is false, the range proof begins at a key which
// guarantees the range can be appended to the response.
// Additionally returns a boolean indicating if there are more leaves in the trie.
func (rb *responseBuilder) isRangeValid(keys, vals [][]byte, hasGap bool) (*memorydb.Database, bool, bool, error) {
	var startKey []byte
	if hasGap {
		startKey = keys[0]
	} else {
		startKey = rb.nextKey()
	}

	proof, err := rb.generateRangeProof(startKey, keys)
	if err != nil {
		return nil, false, false, err
	}
	more, proofErr := rb.verifyRangeProof(keys, vals, startKey, proof)
	return proof, proofErr == nil, more, nil
}

// nextKey returns the nextKey that could potentially be part of the response.
func (rb *responseBuilder) nextKey() []byte {
	if len(rb.response.Keys) == 0 {
		return rb.request.StartKey()
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
	nodeIt, err := rb.t.NodeIterator(rb.nextKey())
	if err != nil {
		return false, err
	}
	it := trie.NewIterator(nodeIt)
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

// readLeafsFromSnapshot iterates the storage snapshot of the requested account
// (or the main account trie if account is empty). Returns up to [rb.limit] key/value
// pairs for keys that are in the request's range (inclusive).
func (rb *responseBuilder) readLeafsFromSnapshot(ctx context.Context) ([][]byte, [][]byte, error) {
	var (
		snapIt    ethdb.Iterator
		startHash = common.BytesToHash(rb.request.StartKey())
		keys      = make([][]byte, 0, rb.limit)
		vals      = make([][]byte, 0, rb.limit)
	)

	// Get an iterator into the storage or the main account snapshot.
	if rb.request.AccountHash() == (common.Hash{}) {
		snapIt = &syncutils.AccountIterator{AccountIterator: rb.snap.DiskAccountIterator(startHash)}
	} else {
		snapIt = &syncutils.StorageIterator{StorageIterator: rb.snap.DiskStorageIterator(rb.request.AccountHash(), startHash)}
	}
	defer snapIt.Release()
	for snapIt.Next() {
		// if we're at the end, break this loop
		if len(rb.request.EndKey()) > 0 && bytes.Compare(snapIt.Key(), rb.request.EndKey()) > 0 {
			break
		}
		// If we've returned enough data or run out of time, set the more flag and exit
		// this flag will determine if the proof is generated or not
		if len(keys) >= int(rb.limit) || ctx.Err() != nil {
			break
		}

		keys = append(keys, snapIt.Key())
		vals = append(vals, snapIt.Value())
	}
	return keys, vals, snapIt.Error()
}
