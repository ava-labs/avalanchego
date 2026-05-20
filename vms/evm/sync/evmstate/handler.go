// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

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
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

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

// Handler serves [syncpb.GetLeafRequest] over [p2p.EVMLeafRequestHandlerID].
type Handler = handlers.Handler[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]

// Responder serves leaf-range requests.
type Responder = handlers.Responder[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]

// NewHandler wires resp into a [Handler].
func NewHandler(log logging.Logger, resp Responder) *Handler {
	return handlers.NewHandler(log, func() *syncpb.GetLeafRequest { return &syncpb.GetLeafRequest{} }, resp)
}

// SnapshotProvider exposes snapshot iterators for the fast path.
type SnapshotProvider interface {
	AccountIterator(start common.Hash) ethdb.Iterator
	StorageIterator(account, start common.Hash) ethdb.Iterator
}

var _ Responder = (*responder)(nil)

// responder is bound to one (trieDB, key-length, snapshot) tuple.
type responder struct {
	trieDB        *triedb.Database
	snapshot      SnapshotProvider // optional
	trieKeyLength int
}

func NewResponder(
	trieDB *triedb.Database,
	trieKeyLength int,
	snapshot SnapshotProvider,
) Responder {
	return &responder{
		trieDB:        trieDB,
		snapshot:      snapshot,
		trieKeyLength: trieKeyLength,
	}
}

func (r *responder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetLeafRequest) (*syncpb.GetLeafResponse, error) {
	if !validateRequest(req, r.trieKeyLength) {
		log.Debug("invalid leaf request, dropping", "nodeID", nodeID, "request", req)
		return nil, nil
	}
	b := newBuild(r, nodeID, req)
	if b == nil {
		return nil, nil
	}
	return b.run(ctx, nodeID)
}

// validateRequest reports whether req has a valid shape.
func validateRequest(req *syncpb.GetLeafRequest, trieKeyLength int) bool {
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

// build holds one in-flight leaf request.
type build struct {
	startKey []byte
	endKey   []byte
	rootHash common.Hash
	account  common.Hash // empty for account trie, non-empty for storage trie
	limit    uint16
	zeroKey  []byte // cached for empty-start proofs

	trie     *trie.Trie
	snapshot SnapshotProvider

	resp *syncpb.GetLeafResponse
}

// newBuild opens the trie and returns a per-request build, or nil
// if the trie root is missing.
func newBuild(r *responder, nodeID ids.NodeID, req *syncpb.GetLeafRequest) *build {
	// TODO(powerslider): We should know the state root that accounts correspond to,
	// as this information will be necessary to access storage tries
	// when the trie is path based.
	root := common.BytesToHash(req.GetRootHash())
	t, err := trie.New(trie.TrieID(root), r.trieDB)
	if err != nil {
		log.Debug("error opening trie when processing request, dropping", "nodeID", nodeID, "root", root, "err", err)
		return nil
	}

	limit := min(uint16(req.GetKeyLimit()), MaxLeavesLimit)
	return &build{
		startKey: req.GetStartKey(),
		endKey:   req.GetEndKey(),
		rootHash: root,
		account:  common.BytesToHash(req.GetAccountHash()),
		limit:    limit,
		zeroKey:  bytes.Repeat([]byte{0x00}, r.trieKeyLength),
		trie:     t,
		snapshot: r.snapshot,
		resp: &syncpb.GetLeafResponse{
			Keys:   make([][]byte, 0, limit),
			Values: make([][]byte, 0, limit),
		},
	}
}

// build runs the response pipeline (snapshot fast path, trie fill-in,
// range proof) and mutates [build.resp].
func (b *build) build(ctx context.Context) error {
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
			return err
		}
		if len(b.startKey) == 0 && !more {
			// Whole trie. Root suffices, no proof.
			return nil
		}
	}

	proof, err := b.generateRangeProof(b.startKey, b.resp.Keys)
	if err != nil {
		return err
	}
	defer proof.Close()

	b.resp.ProofVals, err = iteratorValues(proof)
	if err != nil {
		return err
	}
	return nil
}

// run executes the build pipeline. Returns nil to signal a late drop
// (pipeline error or ctx cancelled before any leaves were read).
func (b *build) run(ctx context.Context, nodeID ids.NodeID) (*syncpb.GetLeafResponse, error) {
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
func (b *build) fillFromSnapshot(ctx context.Context) (bool, error) {
	// Reserve time for the trie fallback.
	snapCtx := ctx
	if deadline, ok := ctx.Deadline(); ok {
		bufferedDeadline := time.Now().Add(time.Until(deadline) * snapshotReadDeadlinePercent / 100)
		var cancel context.CancelFunc
		snapCtx, cancel = context.WithDeadline(ctx, bufferedDeadline)
		defer cancel()
	}

	snapKeys, snapVals, err := b.readFromSnapshot(snapCtx)
	if err != nil {
		return false, err
	}

	// Fast path: validate the entire range against the trie in one shot.
	proof, ok, more, err := b.isRangeValid(snapKeys, snapVals, false)
	if err != nil {
		return false, err
	}
	defer proof.Close()
	if ok {
		b.resp.Keys, b.resp.Values = snapKeys, snapVals
		if len(b.startKey) == 0 && !more {
			return true, nil
		}
		b.resp.ProofVals, err = iteratorValues(proof)
		if err != nil {
			return false, err
		}
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
			return false, err
		}
		_ = segProof.Close() // verdict only, proof not needed
		if !segOK {
			hasGap = true
			continue
		}

		if hasGap {
			// Fill the gap from the trie up to and including
			// snapKeys[i], then drop the trailing entry. snapKeys[i]
			// is also the first entry we are about to append from the
			// snapshot, and it must not be duplicated.
			if _, err := b.fillFromTrie(ctx, snapKeys[i]); err != nil {
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
// [build.limit]. Storage reads scope to [build.account].
func (b *build) readFromSnapshot(ctx context.Context) ([][]byte, [][]byte, error) {
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

// fillFromTrie iterates the trie from [build.nextKey] up to end
// (exclusive). Returns true if the trie has more keys past the response.
func (b *build) fillFromTrie(ctx context.Context, end []byte) (bool, error) {
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
func (b *build) nextKey() []byte {
	if len(b.resp.Keys) == 0 {
		return b.startKey
	}
	next := common.CopyBytes(b.resp.Keys[len(b.resp.Keys)-1])
	incrementBytes(next)
	return next
}

// generateRangeProof returns a Merkle range proof for [start, last].
// Empty start substitutes the cached zero-key.
func (b *build) generateRangeProof(start []byte, keys [][]byte) (*memorydb.Database, error) {
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
func (b *build) verifyRangeProof(keys, vals [][]byte, start []byte, proof *memorydb.Database) (bool, error) {
	if len(start) == 0 {
		start = b.zeroKey
	}
	return trie.VerifyRangeProof(b.rootHash, start, keys, vals, proof)
}

// isRangeValid generates and verifies a range proof for the supplied
// keys/vals. With hasGap=true the proof validates standalone starting
// at keys[0]. With hasGap=false the proof starts at nextKey(), so the
// keys can be appended to the response directly.
func (b *build) isRangeValid(keys, vals [][]byte, hasGap bool) (*memorydb.Database, bool, bool, error) {
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
