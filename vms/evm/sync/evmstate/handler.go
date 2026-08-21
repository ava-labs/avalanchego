// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// RegisterHandler serves leaf-range requests at handlerID on net. The ID names
// which trie is served.
func RegisterHandler(log logging.Logger, net *p2p.Network, handlerID uint64, trieDB *triedb.Database, trieKeyLength int, opts ...HandlerOption) error {
	h := handlers.NewHandler(log, newResponder(log, trieDB, trieKeyLength, opts...))
	return net.AddHandler(handlerID, h)
}

// SnapshotReader serves its reads as taking DiskRoot rather than the
// requested root, which the tree retires before sync stops asking.
type SnapshotReader interface {
	DiskRoot() common.Hash
	AccountIterator(root, seek common.Hash) (snapshot.AccountIterator, error)
	StorageIterator(root, account, seek common.Hash) (snapshot.StorageIterator, error)
}

var (
	_ handlers.Responder[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse] = (*responder)(nil)
	_ SnapshotReader                                                      = (*snapshot.Tree)(nil)
)

type responder struct {
	log      logging.Logger
	trieDB   *triedb.Database
	snapshot SnapshotReader // optional
	zeroKey  []byte         // read-only stand-in for an absent start key
}

func newResponder(log logging.Logger, trieDB *triedb.Database, trieKeyLength int, opts ...HandlerOption) *responder {
	var cfg handlerConfig
	options.ApplyTo(&cfg, opts...)

	return &responder{
		log:      log,
		trieDB:   trieDB,
		snapshot: cfg.snapshot,
		zeroKey:  make([]byte, trieKeyLength),
	}
}

type handlerConfig struct {
	snapshot SnapshotReader
}

// HandlerOption configures [RegisterHandler].
type HandlerOption = options.Option[handlerConfig]

// WithSnapshot serves leaves from the snapshot where it agrees with the trie,
// falling back to trie iteration everywhere else.
func WithSnapshot(s SnapshotReader) HandlerOption {
	return options.Func[handlerConfig](func(c *handlerConfig) {
		c.snapshot = s
	})
}

var (
	errInvalidRequest = &avacommon.AppError{
		Code:    3000,
		Message: "invalid leaf request",
	}
	errRootNotFound = &avacommon.AppError{
		Code:    3001,
		Message: "requested trie root not found",
	}
	errServingCancelled = &avacommon.AppError{
		Code:    3002,
		Message: "serving cancelled",
	}
)

func (r *responder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetLeafRequest) (*syncpb.GetLeafResponse, *avacommon.AppError) {
	if reason := validateRequest(req, len(r.zeroKey)); reason != "" {
		r.log.Debug("rejecting request",
			zap.Stringer("nodeID", nodeID),
			zap.String("reason", reason),
		)
		return nil, errInvalidRequest
	}
	q, appErr := newQuery(r, nodeID, req)
	if appErr != nil {
		return nil, appErr
	}
	return q.run(ctx, nodeID)
}

// validateRequest returns why req is malformed, empty when it is valid. The
// reason is logged in place of the request, which may carry megabytes.
func validateRequest(req *syncpb.GetLeafRequest, trieKeyLength int) string {
	if req.GetKeyLimit() == 0 {
		return "zero key limit"
	}

	root := common.BytesToHash(req.GetRootHash())
	if root == (common.Hash{}) || root == types.EmptyRootHash {
		return "empty trie root"
	}

	startKey, endKey := req.GetStartKey(), req.GetEndKey()
	switch {
	case len(endKey) > 0 && bytes.Compare(startKey, endKey) > 0:
		return "start key after end key"
	case len(startKey) != 0 && len(startKey) != trieKeyLength:
		return "start key length mismatch"
	case len(endKey) != 0 && len(endKey) != trieKeyLength:
		return "end key length mismatch"
	}
	return ""
}

// query holds one in-flight leaf request.
type query struct {
	log      logging.Logger
	startKey []byte
	endKey   []byte
	rootHash common.Hash
	account  common.Hash // empty for account trie, non-empty for storage trie
	limit    uint16
	zeroKey  []byte

	trie     *trie.Trie
	snapshot SnapshotReader

	resp *syncpb.GetLeafResponse
}

// MaxLeavesLimit caps leaves per response.
const MaxLeavesLimit = 1024

// newQuery opens the trie and returns a per-request query.
func newQuery(r *responder, nodeID ids.NodeID, req *syncpb.GetLeafRequest) (*query, *avacommon.AppError) {
	root := common.BytesToHash(req.GetRootHash())
	t, err := trie.New(trie.TrieID(root), r.trieDB)
	if err != nil {
		r.log.Debug("rejecting request",
			zap.Stringer("nodeID", nodeID),
			zap.String("reason", "trie root not found"),
			zap.Stringer("root", root),
			zap.Error(err),
		)
		return nil, errRootNotFound
	}

	limit := uint16(min(req.GetKeyLimit(), MaxLeavesLimit))
	return &query{
		log:      r.log,
		startKey: req.GetStartKey(),
		endKey:   req.GetEndKey(),
		rootHash: root,
		account:  common.BytesToHash(req.GetAccountHash()),
		limit:    limit,
		zeroKey:  r.zeroKey,
		trie:     t,
		snapshot: r.snapshot,
		resp: &syncpb.GetLeafResponse{
			Keys:   make([][]byte, 0, limit),
			Values: make([][]byte, 0, limit),
		},
	}, nil
}

// wholeTrie reports that the response spans the trie end to end, which the root
// alone attests, so no range proof is needed.
func (q *query) wholeTrie(more bool) bool {
	return len(q.startKey) == 0 && !more
}

func (q *query) atLimit() bool {
	return len(q.resp.Keys) >= int(q.limit)
}

// appendLeaves appends what the limit allows. kept below len(keys) means the
// segment was trimmed.
func (q *query) appendLeaves(keys, vals [][]byte) (kept int) {
	kept = min(len(keys), int(q.limit)-len(q.resp.Keys))
	q.resp.Keys = append(q.resp.Keys, keys[:kept]...)
	q.resp.Values = append(q.resp.Values, vals[:kept]...)
	return kept
}

// collect fills [query.resp] with the leaf range and its proof.
func (q *query) collect(ctx context.Context) error {
	if q.snapshot != nil {
		done, err := q.fillFromSnapshot(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}

	// At the limit nothing established what lies past the response, so the
	// range has to be proved.
	more := true
	if !q.atLimit() {
		var err error
		if more, err = q.fillFromTrie(ctx, q.endKey); err != nil {
			return err
		}
	}
	return q.attachProof(more)
}

// attachProof proves the response range, unless it spans the whole trie, which
// the root alone attests.
func (q *query) attachProof(more bool) error {
	if q.wholeTrie(more) {
		return nil
	}

	proofDB, err := q.generateRangeProof(q.startKey, q.resp.Keys)
	if err != nil {
		return err
	}
	q.resp.ProofVals, err = iteratorValues(proofDB)
	return err
}

// run executes the collect pipeline. A pipeline failure is a server fault,
// a cancellation before any leaves were read tells the peer we gave up.
func (q *query) run(ctx context.Context, nodeID ids.NodeID) (*syncpb.GetLeafResponse, *avacommon.AppError) {
	if err := q.collect(ctx); err != nil {
		return nil, handlers.Fault(q.log, nodeID, err)
	}
	if len(q.resp.Keys) == 0 && ctx.Err() != nil {
		q.log.Debug("rejecting request",
			zap.Stringer("nodeID", nodeID),
			zap.String("reason", "cancelled before any leaves were iterated"),
			zap.Error(ctx.Err()),
		)
		return nil, errServingCancelled
	}
	return q.resp, nil
}

// fillFromSnapshot reads from the snapshot. done reports that the response
// needs nothing further, the inverse of the more returned by [query.fillFromTrie].
func (q *query) fillFromSnapshot(ctx context.Context) (done bool, _ error) {
	snapKeys, snapVals := q.readFromSnapshot(ctx)
	if len(snapKeys) == 0 {
		// Unavailable or empty here, use the trie.
		return false, nil
	}

	// Fast path: validate the entire range against the trie in one shot.
	valid, more, err := q.isRangeValid(snapKeys, snapVals, false)
	if err != nil {
		return false, err
	}
	if valid {
		q.appendLeaves(snapKeys, snapVals)
		return q.wholeTrie(more), nil
	}

	return q.fillFromSegments(ctx, snapKeys, snapVals)
}

const snapshotSegmentLen = 64

// fillFromSegments serves a diverged snapshot one segment at a time. A segment
// that fails leaves a gap the next good segment bridges from the trie.
//
// snapKeys=[A B C D E], snapshotSegmentLen=2, [C D] diverged:
//
//	[A B] proves    append        -> resp=[A B]
//	[C D] fails     mark the gap  -> resp=[A B]
//	[E]   proves    bridge to E   -> resp=[A B C D]
//	                append past E -> resp=[A B C D E]
func (q *query) fillFromSegments(ctx context.Context, snapKeys, snapVals [][]byte) (done bool, _ error) {
	hasGap := false
	// Only a proved segment answers this, so it starts pessimistic. A stale
	// answer is safe, collect re-derives it below the limit.
	trieHasMore := true

	for i := 0; i < len(snapKeys) && ctx.Err() == nil; i += snapshotSegmentLen {
		end := min(i+snapshotSegmentLen, len(snapKeys))
		valid, more, err := q.isRangeValid(snapKeys[i:end], snapVals[i:end], hasGap)
		if err != nil {
			return false, err
		}
		if !valid {
			hasGap = true
			continue
		}

		start := i
		if hasGap {
			// The bridge stops on snapKeys[i] inclusive, so skip it here.
			if _, err := q.fillFromTrie(ctx, snapKeys[i]); err != nil {
				return false, err
			}
			if q.atLimit() {
				break
			}
			start = i + 1
		}
		hasGap = false

		// The response now ends where this segment was proved, so the segment's
		// verdict carries. A trimmed segment leaves the rest of the trie to come.
		kept := q.appendLeaves(snapKeys[start:end], snapVals[start:end])
		trieHasMore = more || kept < end-start

		if q.atLimit() {
			break
		}
	}
	return q.wholeTrie(trieHasMore), nil
}

// leafEncoder returns the trie-encoded value at an iterator's cursor. The
// snapshot holds slim accounts where the trie holds full ones.
type leafEncoder func() ([]byte, error)

// snapshotLeaves opens a disk-layer iterator over the account trie, or over
// the request's storage trie, with the function that trie-encodes its values.
//
// DiskRoot and the iterator are separate calls, so a concurrent flatten can
// retire the root in between and fail the open.
func (q *query) snapshotLeaves() (snapshot.Iterator, leafEncoder, error) {
	diskRoot := q.snapshot.DiskRoot()
	seek := common.BytesToHash(q.startKey)

	if q.account == (common.Hash{}) {
		it, err := q.snapshot.AccountIterator(diskRoot, seek)
		if err != nil {
			return nil, nil, err
		}
		return it, func() ([]byte, error) {
			return types.FullAccountRLP(it.Account())
		}, nil
	}

	it, err := q.snapshot.StorageIterator(diskRoot, q.account, seek)
	if err != nil {
		return nil, nil, err
	}
	return it, func() ([]byte, error) {
		return it.Slot(), nil
	}, nil
}

// abandonSnapshot gives up the fast path. A snapshot that never serves looks
// exactly like no snapshot at all, so every way of getting here is reported.
func (q *query) abandonSnapshot(reason string, err error) {
	q.log.Debug("snapshot read abandoned, falling back to the trie",
		zap.String("reason", reason),
		zap.Stringer("account", q.account),
		zap.Error(err),
	)
}

// readFromSnapshot pulls leaves in [startKey, endKey] up to [query.limit]. They
// are unvalidated, the caller range-proves them against the requested root.
func (q *query) readFromSnapshot(ctx context.Context) ([][]byte, [][]byte) {
	it, leaf, err := q.snapshotLeaves()
	if err != nil {
		q.abandonSnapshot("iterator unavailable", err)
		return nil, nil
	}
	defer it.Release()

	keys := make([][]byte, 0, q.limit)
	vals := make([][]byte, 0, q.limit)
	for it.Next() {
		k := it.Hash().Bytes()
		if len(q.endKey) > 0 && bytes.Compare(k, q.endKey) > 0 {
			break
		}
		if len(keys) >= int(q.limit) || ctx.Err() != nil {
			break
		}
		v, err := leaf()
		if err != nil {
			q.abandonSnapshot("leaf encoding failed", err)
			return nil, nil
		}
		keys = append(keys, k)
		vals = append(vals, v)
	}
	if err := it.Error(); err != nil {
		q.abandonSnapshot("iteration failed", err)
		return nil, nil
	}
	return keys, vals
}

// fillFromTrie iterates the trie from [query.nextKey] up to end (exclusive).
// more reports keys past the response, the inverse of fillFromSnapshot's done.
func (q *query) fillFromTrie(ctx context.Context, end []byte) (more bool, _ error) {
	nodeIt, err := q.trie.NodeIterator(q.nextKey())
	if err != nil {
		return false, err
	}
	it := trie.NewIterator(nodeIt)

	for it.Next() {
		if len(end) > 0 && bytes.Compare(it.Key, end) > 0 {
			more = true
			break
		}
		if q.atLimit() || ctx.Err() != nil {
			more = true
			break
		}
		q.resp.Keys = append(q.resp.Keys, it.Key)
		q.resp.Values = append(q.resp.Values, it.Value)
	}
	return more, it.Err
}

// nextKey returns where trie iteration resumes after the response.
func (q *query) nextKey() []byte {
	if len(q.resp.Keys) == 0 {
		return q.startKey
	}
	next := common.CopyBytes(q.resp.Keys[len(q.resp.Keys)-1])
	incrementBytes(next)
	return next
}

// generateRangeProof returns a Merkle range proof for [start, last]. An absent
// start means the trie's beginning, which Prove needs as a concrete key.
func (q *query) generateRangeProof(start []byte, keys [][]byte) (*memorydb.Database, error) {
	proofDB := memorydb.New()
	if len(start) == 0 {
		start = q.zeroKey
	}
	if err := q.trie.Prove(start, proofDB); err != nil {
		return nil, err
	}
	if len(keys) > 0 {
		end := keys[len(keys)-1]
		if err := q.trie.Prove(end, proofDB); err != nil {
			return nil, err
		}
	}
	return proofDB, nil
}

// verifyRangeProof reports whether the trie has more keys past the last
// verified key. more carries the same meaning as in [query.fillFromTrie].
func (q *query) verifyRangeProof(keys, vals [][]byte, start []byte, proofDB *memorydb.Database) (more bool, _ error) {
	if len(start) == 0 {
		start = q.zeroKey
	}
	return trie.VerifyRangeProof(q.rootHash, start, keys, vals, proofDB)
}

// isRangeValid range-proves keys/vals against the trie. Without a gap the proof
// starts at nextKey, so the span back to the response is covered too.
func (q *query) isRangeValid(keys, vals [][]byte, hasGap bool) (valid, more bool, _ error) {
	var startKey []byte
	if hasGap {
		startKey = keys[0]
	} else {
		startKey = q.nextKey()
	}

	proofDB, err := q.generateRangeProof(startKey, keys)
	if err != nil {
		return false, false, err
	}
	more, proofErr := q.verifyRangeProof(keys, vals, startKey, proofDB)
	return proofErr == nil, more, nil
}

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

// incrementBytes adds 1 to b in place, with carry. All-0xff wraps to
// all-zeros.
func incrementBytes(b []byte) {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return
		}
		b[i] = 0
	}
}
