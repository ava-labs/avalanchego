// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"math"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/ids"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	// MaxLeavesLimit caps leaves per response.
	MaxLeavesLimit = uint16(1024)

	// snapshotReadDeadlinePercent reserves the rest of the request
	// deadline for trie iteration if the snapshot pass returns invalid
	// data.
	snapshotReadDeadlinePercent = 75

	// snapshotSegmentLen is the per-segment validation granularity for
	// the snapshot slow path.
	snapshotSegmentLen = 64
)

// LeafHandler serves [syncpb.GetLeafRequest] over [p2p.EVMLeafsRequestHandlerID].
type LeafHandler = Handler[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]

// LeafResponder is the inner contract for leaf-range requests.
type LeafResponder = Responder[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]

// NewLeafHandler wires resp into a [LeafHandler].
func NewLeafHandler(resp LeafResponder) *LeafHandler {
	return NewHandler(func() *syncpb.GetLeafRequest { return &syncpb.GetLeafRequest{} }, resp)
}

// SnapshotProvider exposes snapshot iterators for the leaf responder's
// fast path.
type SnapshotProvider interface {
	AccountIterator(start common.Hash) ethdb.Iterator
	StorageIterator(account, start common.Hash) ethdb.Iterator
}

var _ LeafResponder = (*leafResponder)(nil)

// leafResponder is bound to one (trieDB, key-length, snapshot) tuple.
// Use one per trie kind.
type leafResponder struct {
	trieDB        *triedb.Database
	snapshot      SnapshotProvider // optional
	stats         LeafStats
	trieKeyLength int
}

func NewLeafResponder(
	trieDB *triedb.Database,
	trieKeyLength int,
	snapshot SnapshotProvider,
	stats LeafStats,
) LeafResponder {
	return &leafResponder{
		trieDB:        trieDB,
		snapshot:      snapshot,
		stats:         stats,
		trieKeyLength: trieKeyLength,
	}
}

func (r *leafResponder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetLeafRequest) (*syncpb.GetLeafResponse, error) {
	if !validateLeafRequest(req, r.trieKeyLength) {
		log.Debug("invalid leaf request, dropping", "nodeID", nodeID, "request", req)
		r.stats.IncInvalidLeafRequest()
		return nil, nil
	}
	b := newLeafBuild(r, nodeID, req)
	if b == nil {
		return nil, nil
	}
	return b.run(ctx, nodeID)
}

// validateLeafRequest reports whether req has a valid shape.
func validateLeafRequest(req *syncpb.GetLeafRequest, trieKeyLength int) bool {
	if req.GetNodeType() != syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE {
		return false
	}
	if req.GetKeyLimit() == 0 || req.GetKeyLimit() > math.MaxUint16 {
		return false
	}
	root := common.BytesToHash(req.GetRootHash())
	if root == (common.Hash{}) || root == types.EmptyRootHash {
		return false
	}
	startKey, endKey := req.GetStartKey(), req.GetEndKey()
	if len(endKey) > 0 && bytes.Compare(startKey, endKey) > 0 {
		return false
	}
	if (len(startKey) != 0 && len(startKey) != trieKeyLength) ||
		(len(endKey) != 0 && len(endKey) != trieKeyLength) {
		return false
	}
	return true
}

// leafBuild owns one in-flight leaf request: pulls leaves from the
// snapshot fast path (when available), falls back to trie iteration,
// and emits the range proof.
type leafBuild struct {
	startTime time.Time

	startKey []byte
	endKey   []byte
	rootHash common.Hash
	account  common.Hash // empty for account trie, non-empty for storage trie
	limit    uint16
	zeroKey  []byte // cached for empty-start proofs

	trie     *trie.Trie
	snapshot SnapshotProvider

	resp *syncpb.GetLeafResponse

	stats        LeafStats
	trieReadTime time.Duration
	proofTime    time.Duration
}

// newLeafBuild opens the trie and returns a per-request build, or nil
// if the trie root is missing.
func newLeafBuild(r *leafResponder, nodeID ids.NodeID, req *syncpb.GetLeafRequest) *leafBuild {
	startTime := time.Now()
	r.stats.IncLeafRequest()

	// TODO(powerslider): We should know the state root that accounts correspond to,
	// as this information will be necessary to access storage tries
	// when the trie is path based.
	root := common.BytesToHash(req.GetRootHash())
	t, err := trie.New(trie.TrieID(root), r.trieDB)
	if err != nil {
		log.Debug("error opening trie when processing request, dropping", "nodeID", nodeID, "root", root, "err", err)
		r.stats.IncMissingRoot()
		return nil
	}

	limit := min(uint16(req.GetKeyLimit()), MaxLeavesLimit)
	return &leafBuild{
		startTime: startTime,
		startKey:  req.GetStartKey(),
		endKey:    req.GetEndKey(),
		rootHash:  root,
		account:   common.BytesToHash(req.GetAccountHash()),
		limit:     limit,
		zeroKey:   bytes.Repeat([]byte{0x00}, r.trieKeyLength),
		trie:      t,
		snapshot:  r.snapshot,
		resp: &syncpb.GetLeafResponse{
			Keys:   make([][]byte, 0, limit),
			Values: make([][]byte, 0, limit),
		},
		stats: r.stats,
	}
}

// build runs the response pipeline: snapshot fast path (if available),
// then trie fill-in, then range proof. Mutates [leafBuild.resp].
func (b *leafBuild) build(ctx context.Context) error {
	if b.snapshot != nil {
		done, err := b.fillFromSnapshot(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		// Snapshot didn't satisfy the response on its own. Trie
		// iteration will regenerate the proof at the end.
		b.resp.ProofVals = nil
	}

	if len(b.resp.Keys) < int(b.limit) {
		more, err := b.fillFromTrie(ctx, b.endKey)
		if err != nil {
			b.stats.IncTrieError()
			return err
		}
		if len(b.startKey) == 0 && !more {
			// Whole trie. Root suffices, no proof.
			return nil
		}
	}

	proof, err := b.generateRangeProof(b.startKey, b.resp.Keys)
	if err != nil {
		b.stats.IncProofError()
		return err
	}
	defer proof.Close()

	b.resp.ProofVals, err = iteratorValues(proof)
	if err != nil {
		b.stats.IncProofError()
		return err
	}
	return nil
}

// run executes the build pipeline. Returns nil to signal a late drop
// (pipeline error or ctx cancelled before any leaves were read).
func (b *leafBuild) run(ctx context.Context, nodeID ids.NodeID) (*syncpb.GetLeafResponse, error) {
	defer func() {
		b.stats.UpdateLeafRequestProcessingTime(time.Since(b.startTime))
		b.stats.UpdateLeafReturned(uint16(len(b.resp.Keys)))
		b.stats.UpdateRangeProofValsReturned(int64(len(b.resp.ProofVals)))
		b.stats.UpdateGenerateRangeProofTime(b.proofTime)
		b.stats.UpdateReadLeafTime(b.trieReadTime)
	}()

	if err := b.build(ctx); err != nil {
		log.Debug("failed to serve leaf request", "nodeID", nodeID, "err", err)
		return nil, nil
	}
	if len(b.resp.Keys) == 0 && ctx.Err() != nil {
		log.Debug("context err set before any leaves were iterated", "nodeID", nodeID, "ctxErr", ctx.Err())
		return nil, nil
	}
	return b.resp, nil
}

// fillFromSnapshot reads from the snapshot. Returns true if the
// response is complete.
func (b *leafBuild) fillFromSnapshot(ctx context.Context) (bool, error) {
	snapshotReadStart := time.Now()
	b.stats.IncSnapshotReadAttempt()

	// Reserve time for the trie fallback.
	snapCtx := ctx
	if deadline, ok := ctx.Deadline(); ok {
		bufferedDeadline := time.Now().Add(time.Until(deadline) * snapshotReadDeadlinePercent / 100)
		var cancel context.CancelFunc
		snapCtx, cancel = context.WithDeadline(ctx, bufferedDeadline)
		defer cancel()
	}

	snapKeys, snapVals, err := b.readFromSnapshot(snapCtx)
	b.stats.UpdateSnapshotReadTime(time.Since(snapshotReadStart))
	if err != nil {
		b.stats.IncSnapshotReadError()
		return false, err
	}

	// Fast path: validate the entire range against the trie in one shot.
	proof, ok, more, err := b.isRangeValid(snapKeys, snapVals, false)
	if err != nil {
		b.stats.IncProofError()
		return false, err
	}
	defer proof.Close()
	if ok {
		b.resp.Keys, b.resp.Values = snapKeys, snapVals
		if len(b.startKey) == 0 && !more {
			b.stats.IncSnapshotReadSuccess()
			return true, nil
		}
		b.resp.ProofVals, err = iteratorValues(proof)
		if err != nil {
			b.stats.IncProofError()
			return false, err
		}
		b.stats.IncSnapshotReadSuccess()
		return !more, nil
	}

	// Slow path: walk snapshot keys in fixed-size segments and verify
	// each against the trie. Valid segments are appended directly.
	// Invalid segments are bridged from the trie when the next valid
	// segment arrives: fill the trie up to and including that
	// segment's first key, drop the trailing key (the segment is
	// about to append it), then append the segment.
	//
	// Example with snapKeys=[A B C D E], snapshotSegmentLen=2, [C D] invalid:
	//   i=0  [A B] valid    append           -> resp=[A B]
	//   i=2  [C D] invalid  mark gap         -> resp unchanged
	//   i=4  [E]   valid    trie-fill to E   -> resp=[A B C D E]
	//                       drop trailing E  -> resp=[A B C D]
	//                       append [E]       -> resp=[A B C D E]
	hasGap := false
	for i := 0; i < len(snapKeys); i += snapshotSegmentLen {
		segmentEnd := min(i+snapshotSegmentLen, len(snapKeys))
		segProof, segOK, _, err := b.isRangeValid(snapKeys[i:segmentEnd], snapVals[i:segmentEnd], hasGap)
		if err != nil {
			b.stats.IncProofError()
			return false, err
		}
		_ = segProof.Close() // verdict only, proof not needed
		if !segOK {
			b.stats.IncSnapshotSegmentInvalid()
			hasGap = true
			continue
		}

		b.stats.IncSnapshotSegmentValid()
		if hasGap {
			// Fill the gap from the trie up to and including
			// snapKeys[i], then drop the trailing entry. snapKeys[i]
			// is also the first entry we are about to append from the
			// snapshot, and it must not be duplicated.
			if _, err := b.fillFromTrie(ctx, snapKeys[i]); err != nil {
				b.stats.IncTrieError()
				return false, err
			}
			if len(b.resp.Keys) >= int(b.limit) || ctx.Err() != nil {
				break
			}
			b.resp.Keys = b.resp.Keys[:len(b.resp.Keys)-1]
			b.resp.Values = b.resp.Values[:len(b.resp.Values)-1]
		}
		hasGap = false

		// Trim the segment to fit the remaining limit.
		segmentEnd = min(segmentEnd, i+int(b.limit)-len(b.resp.Keys))
		b.resp.Keys = append(b.resp.Keys, snapKeys[i:segmentEnd]...)
		b.resp.Values = append(b.resp.Values, snapVals[i:segmentEnd]...)

		if len(b.resp.Keys) >= int(b.limit) {
			break
		}
	}
	return false, nil
}

// readFromSnapshot pulls leaves in [startKey, endKey], capped at
// [leafBuild.limit]. Storage reads scope to [leafBuild.account].
func (b *leafBuild) readFromSnapshot(ctx context.Context) ([][]byte, [][]byte, error) {
	startHash := common.BytesToHash(b.startKey)
	var snapIt ethdb.Iterator
	if b.account == (common.Hash{}) {
		snapIt = b.snapshot.AccountIterator(startHash)
	} else {
		snapIt = b.snapshot.StorageIterator(b.account, startHash)
	}
	defer snapIt.Release()

	keys := make([][]byte, 0, b.limit)
	vals := make([][]byte, 0, b.limit)
	for snapIt.Next() {
		if len(b.endKey) > 0 && bytes.Compare(snapIt.Key(), b.endKey) > 0 {
			break
		}
		if len(keys) >= int(b.limit) || ctx.Err() != nil {
			break
		}
		keys = append(keys, snapIt.Key())
		vals = append(vals, snapIt.Value())
	}
	return keys, vals, snapIt.Error()
}

// fillFromTrie iterates the trie from [leafBuild.nextKey] up to end
// (exclusive). Returns true if the trie has more keys past the response.
func (b *leafBuild) fillFromTrie(ctx context.Context, end []byte) (bool, error) {
	startTime := time.Now()
	defer func() { b.trieReadTime += time.Since(startTime) }()

	nodeIt, err := b.trie.NodeIterator(b.nextKey())
	if err != nil {
		return false, err
	}
	it := trie.NewIterator(nodeIt)

	more := false
	for it.Next() {
		if len(end) > 0 && bytes.Compare(it.Key, end) > 0 {
			more = true
			break
		}
		if len(b.resp.Keys) >= int(b.limit) || ctx.Err() != nil {
			more = true
			break
		}
		b.resp.Keys = append(b.resp.Keys, it.Key)
		b.resp.Values = append(b.resp.Values, it.Value)
	}
	return more, it.Err
}

// nextKey returns the trie iteration start: a byte-incremented copy
// of the last response key, or the request start when empty.
func (b *leafBuild) nextKey() []byte {
	if len(b.resp.Keys) == 0 {
		return b.startKey
	}
	next := common.CopyBytes(b.resp.Keys[len(b.resp.Keys)-1])
	incrementBytes(next)
	return next
}

// generateRangeProof returns a Merkle range proof for [start, last].
// Empty start substitutes the cached zero-key.
func (b *leafBuild) generateRangeProof(start []byte, keys [][]byte) (*memorydb.Database, error) {
	startTime := time.Now()
	defer func() { b.proofTime += time.Since(startTime) }()

	proof := memorydb.New()
	if len(start) == 0 {
		start = b.zeroKey
	}
	if err := b.trie.Prove(start, proof); err != nil {
		_ = proof.Close()
		return nil, err
	}
	if len(keys) > 0 {
		end := keys[len(keys)-1]
		if err := b.trie.Prove(end, proof); err != nil {
			_ = proof.Close()
			return nil, err
		}
	}
	return proof, nil
}

// verifyRangeProof returns whether the trie has more keys past the
// last verified key.
func (b *leafBuild) verifyRangeProof(keys, vals [][]byte, start []byte, proof *memorydb.Database) (bool, error) {
	startTime := time.Now()
	defer func() { b.proofTime += time.Since(startTime) }()

	if len(start) == 0 {
		start = b.zeroKey
	}
	return trie.VerifyRangeProof(b.rootHash, start, keys, vals, proof)
}

// isRangeValid generates and verifies a range proof for the supplied
// keys/vals. With hasGap=true the proof validates standalone starting
// at keys[0]. With hasGap=false the proof starts at nextKey(), so the
// keys can be appended to the response directly.
func (b *leafBuild) isRangeValid(keys, vals [][]byte, hasGap bool) (*memorydb.Database, bool, bool, error) {
	var startKey []byte
	if hasGap {
		startKey = keys[0]
	} else {
		startKey = b.nextKey()
	}

	proof, err := b.generateRangeProof(startKey, keys)
	if err != nil {
		return nil, false, false, err
	}
	more, proofErr := b.verifyRangeProof(keys, vals, startKey, proof)
	return proof, proofErr == nil, more, nil
}

// iteratorValues drains a memorydb iterator into a slice of values.
func iteratorValues(db *memorydb.Database) ([][]byte, error) {
	if db == nil {
		return nil, nil
	}
	it := db.NewIterator(nil, nil)
	defer it.Release()

	out := make([][]byte, 0, db.Len())
	for it.Next() {
		out = append(out, it.Value())
	}
	return out, it.Error()
}

// incrementBytes adds 1 to b in place, with carry.
// Example: [0x01, 0xff] becomes [0x02, 0x00].
// All-0xff wraps to all-zeros.
func incrementBytes(b []byte) {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return
		}
		b[i] = 0
	}
}
