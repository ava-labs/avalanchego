// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"slices"

	"github.com/ava-labs/libevm/common"
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

var _ handlers.Responder[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse] = (*responder)(nil)

type responder struct {
	log      logging.Logger
	trieDB   *triedb.Database
	snapshot Snapshot // optional
	minKey   []byte   // read-only stand-in for an absent start key
	maxKey   []byte   // read-only stand-in for an absent end key
}

func newResponder(log logging.Logger, trieDB *triedb.Database, trieKeyLength int, opts ...HandlerOption) *responder {
	return &responder{
		log:      log,
		trieDB:   trieDB,
		snapshot: options.As(opts...).snapshot,
		minKey:   make([]byte, trieKeyLength),
		maxKey:   bytes.Repeat([]byte{0xff}, trieKeyLength),
	}
}

type handlerConfig struct {
	snapshot Snapshot
}

// HandlerOption configures [RegisterHandler].
type HandlerOption = options.Option[handlerConfig]

// WithSnapshot serves leaves from the snapshot where it agrees with the trie,
// falling back to trie iteration everywhere else. A nil snapshot is equivalent
// to not providing this option.
func WithSnapshot[V any, P SnapshotPointer[V]](s P) HandlerOption {
	return options.Func[handlerConfig](func(c *handlerConfig) {
		if s != nil {
			c.snapshot = s
		}
	})
}

func (r *responder) Respond(_ context.Context, nodeID ids.NodeID, req *syncpb.GetLeafRequest) (*syncpb.GetLeafResponse, *avacommon.AppError) {
	q, appErr := newQuery(r, req)
	if appErr != nil {
		r.log.Debug("rejecting request",
			zap.Stringer("nodeID", nodeID),
			zap.Error(appErr),
		)
		return nil, appErr
	}
	resp, err := q.collect()
	if err != nil {
		return nil, handlers.Fault(r.log, nodeID, err)
	}
	return resp, nil
}

// query holds the read-only inputs of a request.
type query struct {
	startKey []byte
	endKey   []byte
	limit    int

	snapshot trieSnapshot
	trie     *trie.Trie
	minKey   []byte
}

// MaxLeavesLimit caps leaves per response.
const MaxLeavesLimit = 1024

var (
	errWrongStartKeyLength = &avacommon.AppError{
		Code:    3000,
		Message: "start key length mismatch",
	}
	errWrongEndKeyLength = &avacommon.AppError{
		Code:    3001,
		Message: "end key length mismatch",
	}
	errStartAfterEnd = &avacommon.AppError{
		Code:    3002,
		Message: "start key after end key",
	}
	errZeroKeyLimit = &avacommon.AppError{
		Code:    3003,
		Message: "zero key limit",
	}
	errWrongAccountHashLength = &avacommon.AppError{
		Code:    3004,
		Message: "account length mismatch",
	}
	errWrongRootLength = &avacommon.AppError{
		Code:    3005,
		Message: "root length mismatch",
	}
	errMissingRoot = &avacommon.AppError{
		Code:    3006,
		Message: "missing trie root",
	}
	errEmptyRoot = &avacommon.AppError{
		Code:    3007,
		Message: "empty trie root",
	}
	errRootNotFound = &avacommon.AppError{
		Code:    3008,
		Message: "requested trie root not found",
	}
)

// newQuery opens the requested trie and returns the per-request query, or the
// rejection for a malformed or unservable req.
func newQuery(r *responder, req *syncpb.GetLeafRequest) (*query, *avacommon.AppError) {
	start := req.GetStartKey()
	if len(start) == 0 {
		start = r.minKey
	}
	end := req.GetEndKey()
	if len(end) == 0 {
		end = r.maxKey
	}
	trieKeyLength := len(r.minKey)
	switch {
	case len(start) != trieKeyLength:
		return nil, errWrongStartKeyLength
	case len(end) != trieKeyLength:
		return nil, errWrongEndKeyLength
	case bytes.Compare(start, end) > 0:
		return nil, errStartAfterEnd
	}

	limit := req.GetKeyLimit()
	if limit == 0 {
		return nil, errZeroKeyLimit
	}

	account := req.GetAccountHash()
	if len(account) != 0 && len(account) != common.HashLength {
		return nil, errWrongAccountHashLength
	}

	var snap trieSnapshot
	if r.snapshot != nil {
		if len(account) != 0 {
			snap = storageSnapshot{
				s:       r.snapshot,
				account: common.BytesToHash(account),
			}
		} else {
			snap = accountSnapshot{
				s: r.snapshot,
			}
		}
	}

	rootBytes := req.GetRootHash()
	if len(rootBytes) != common.HashLength {
		return nil, errWrongRootLength
	}
	root := common.BytesToHash(rootBytes)
	switch root {
	case common.Hash{}:
		return nil, errMissingRoot
	case types.EmptyRootHash:
		return nil, errEmptyRoot
	}

	t, err := trie.New(trie.TrieID(root), r.trieDB)
	if err != nil {
		return nil, errRootNotFound
	}
	return &query{
		startKey: start,
		endKey:   end,
		limit:    int(min(limit, MaxLeavesLimit)),

		snapshot: snap,
		trie:     t,
		minKey:   r.minKey,
	}, nil
}

// collect returns the response holding the leaf range and its proof.
func (q *query) collect() (*syncpb.GetLeafResponse, error) {
	r := newLeafRange(q.startKey, q.limit)
	more, err := fillFromSnapshot(q.snapshot, q.trie, r, q.endKey)
	if err != nil {
		return nil, err
	}
	if more && !r.full() {
		more, err = fillFromTrie(q.trie, r, q.endKey)
		if err != nil {
			return nil, err
		}
	}

	resp := &syncpb.GetLeafResponse{
		Keys:   r.keys,
		Values: r.vals,
	}
	// [trie.VerifyRangeProof] allows an empty proof when proving a full trie.
	// This uses less bandwidth and is faster to verify.
	if bytes.Equal(q.startKey, q.minKey) && !more {
		return resp, nil
	}

	proofDB, err := newRangeProof(q.trie, q.startKey, r.keys)
	if err != nil {
		return nil, err
	}
	resp.ProofVals = dbValues(proofDB)
	return resp, nil
}

// leafRange accumulates the leaves of one response.
type leafRange struct {
	start []byte // start of the requested range
	keys  [][]byte
	vals  [][]byte
}

// newLeafRange returns an empty range starting at start, holding at most
// capacity leaves.
func newLeafRange(start []byte, capacity int) *leafRange {
	return &leafRange{
		start: start,
		keys:  make([][]byte, 0, capacity),
		vals:  make([][]byte, 0, capacity),
	}
}

// space returns the remaining capacity for the range.
func (l *leafRange) space() int {
	return cap(l.keys) - len(l.keys)
}

// full reports whether the range's capacity has been reached.
func (l *leafRange) full() bool {
	return l.space() == 0
}

// append appends what the capacity allows. kept below len(keys) means the
// leaves were trimmed.
func (l *leafRange) append(keys, vals [][]byte) (kept int) {
	space := l.space()
	kept = min(len(keys), space)
	l.keys = append(l.keys, keys[:kept]...)
	l.vals = append(l.vals, vals[:kept]...)
	return kept
}

// add appends a leaf, ignoring the capacity.
func (l *leafRange) add(key, val []byte) {
	l.keys = append(l.keys, key)
	l.vals = append(l.vals, val)
}

// next returns where trie iteration resumes after the appended leaves.
func (l *leafRange) next() []byte {
	if len(l.keys) == 0 {
		return l.start
	}

	last := l.keys[len(l.keys)-1]
	next := slices.Clone(last)
	incrementBytes(next)
	return next
}

// fillFromSnapshot appends leaves from [leafRange.next] through end to r, up
// to the capacity, serving from the snapshot where it agrees with the trie.
// A nil snapshot appends nothing. It returns whether the trie may hold leaves
// past the response.
func fillFromSnapshot(s trieSnapshot, t *trie.Trie, r *leafRange, end []byte) (bool, error) {
	if s == nil {
		return true, nil
	}

	next := r.next()
	keys, vals, err := readSnapshot(
		s,
		common.BytesToHash(next),
		common.BytesToHash(end),
		r.space(),
	)
	if err != nil || len(keys) == 0 {
		// Since the snapshot is volatile, an error or an empty read falls
		// back to the trie.
		return true, nil
	}

	// The whole read often proves in one shot, avoiding per-segment proofs.
	valid, more, err := isRangeValid(
		t,
		&leafRange{
			start: next,
			keys:  keys,
			vals:  vals,
		},
	)
	if err != nil {
		return false, err
	}
	if valid {
		// TODO(StephenButtolph): In the case where the snapshot proved
		// correctly and it isn't the full trie, we could avoid re-proving the
		// range and just return the proof here.
		r.append(keys, vals)
		return more, nil
	}

	return fillFromSegments(t, r, keys, vals)
}

const segmentLen = 64

// fillFromSegments appends segments of keys and vals that prove against the
// trie to r, filling the gaps left by failed segments with leaves from the
// trie. It returns whether the trie may hold leaves past the range.
//
// keys=[A B C D E], segmentLen=2, [C D] diverged:
//
//	[A B] proves    append           -> r=[A B]
//	[C D] fails     mark the gap     -> r=[A B]
//	[E]   proves    fill gap below E -> r=[A B C D]
//	                append [E]       -> r=[A B C D E]
func fillFromSegments(t *trie.Trie, r *leafRange, keys, vals [][]byte) (bool, error) {
	// Only a proved segment establishes what lies past the response, so more
	// starts pessimistic.
	more := true
	hasGap := false
	for i := 0; i < len(keys) && !r.full(); i += segmentLen {
		// Starting at r.next proves the trie holds nothing between the
		// response and the segment. After a gap the trie itself supplies
		// that span, so the proof starts at the segment.
		start := r.next()
		if hasGap {
			start = keys[i]
		}
		end := min(i+segmentLen, len(keys))
		segment := &leafRange{
			start: start,
			keys:  keys[i:end],
			vals:  vals[i:end],
		}

		valid, moreAfterSeg, err := isRangeValid(t, segment)
		if err != nil {
			return false, err
		}
		if !valid {
			hasGap = true
			continue
		}
		if hasGap {
			// The trie supplies the gap, the segment supplies the rest.
			if _, err := fillFromTrie(t, r, segment.keys[0], withExclusiveEnd()); err != nil {
				return false, err
			}
			hasGap = false
		}

		// moreAfterSeg speaks for the segment's end, which the response only
		// reaches when nothing is trimmed.
		kept := r.append(segment.keys, segment.vals)
		more = moreAfterSeg || kept < len(segment.keys)
	}
	return more, nil
}

type fillConfig struct {
	// maxEndCmp is the largest [bytes.Compare] result allowed between an
	// appended key and the fill's end. 0 includes end, -1 excludes it.
	maxEndCmp int
}

// fillOption configures [fillFromTrie].
type fillOption = options.Option[fillConfig]

// withExclusiveEnd stops the fill before end rather than on it.
func withExclusiveEnd() fillOption {
	return options.Func[fillConfig](func(c *fillConfig) {
		c.maxEndCmp = -1
	})
}

// fillFromTrie appends trie leaves from [leafRange.next] through end to r, up
// to the capacity. [withExclusiveEnd] stops the fill before end instead. It
// returns whether the trie holds leaves past the response.
func fillFromTrie(t *trie.Trie, r *leafRange, end []byte, opts ...fillOption) (bool, error) {
	// While [trie.Trie.NodeIterator] documents that it starts iterating after
	// the given key, it actually starts at the key if it exists.
	nodeIt, err := t.NodeIterator(r.next())
	if err != nil {
		return false, err
	}
	it := trie.NewIterator(nodeIt)

	maxEndCmp := options.As(opts...).maxEndCmp
	for it.Next() {
		if bytes.Compare(it.Key, end) > maxEndCmp || r.full() {
			return true, it.Err
		}
		r.add(it.Key, it.Value)
	}
	return false, it.Err
}

// isRangeValid range-proves r against the trie. valid reports whether the proof
// succeeded, and more reports whether the trie holds leaves past r.
func isRangeValid(t *trie.Trie, r *leafRange) (valid, more bool, _ error) {
	proofDB, err := newRangeProof(t, r.start, r.keys)
	if err != nil {
		return false, false, err
	}
	more, proofErr := trie.VerifyRangeProof(t.Hash(), r.start, r.keys, r.vals, proofDB)
	return proofErr == nil, more, nil
}

// newRangeProof returns a range proof for [start, last(keys)].
func newRangeProof(t *trie.Trie, start []byte, keys [][]byte) (*memorydb.Database, error) {
	// [trie.VerifyRangeProof] requires the proof to resolve a full path for
	// each edge key, even when the leaves would otherwise prove the range.
	proofDB := memorydb.New()
	if err := t.Prove(start, proofDB); err != nil {
		return nil, err
	}
	if len(keys) > 0 {
		last := keys[len(keys)-1]
		if err := t.Prove(last, proofDB); err != nil {
			return nil, err
		}
	}
	return proofDB, nil
}

// dbValues returns all values stored in the given memorydb.Database. It never
// returns an error, because the database is in-memory and cannot fail.
func dbValues(db *memorydb.Database) [][]byte {
	it := db.NewIterator(nil, nil)
	defer it.Release()

	out := make([][]byte, 0, db.Len())
	for it.Next() {
		out = append(out, it.Value())
	}
	return out
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
