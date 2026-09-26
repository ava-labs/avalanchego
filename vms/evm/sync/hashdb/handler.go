// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// RegisterHandler serves leaf-range requests at handlerID on net, counting
// the requests on reg. Requests name the trie to read by root, opened from
// trieDB. Every key in a served trie is trieKeyLength bytes.
//
// Each registration counts on its own reg, so a node serving several tries
// (e.g. the EVM state trie and the atomic trie) MUST give each handler a
// distinctly prefixed registerer to keep their metrics apart.
func RegisterHandler(
	log logging.Logger,
	net *p2p.Network,
	handlerID uint64,
	trieDB *triedb.Database,
	trieKeyLength int,
	reg prometheus.Registerer,
	opts ...HandlerOption,
) error {
	m, err := newHandlerMetrics(reg)
	if err != nil {
		return fmt.Errorf("registering leafs handler metrics: %w", err)
	}
	h := handlers.NewHandler(log, newResponder(log, trieDB, trieKeyLength, m, opts...))
	return net.AddHandler(handlerID, h)
}

var _ handlers.Responder[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse] = (*responder)(nil)

type responder struct {
	log      logging.Logger
	trieDB   *triedb.Database
	snapshot Snapshot // optional
	minKey   []byte   // read-only stand-in for an absent start key
	metrics  *handlerMetrics
}

func newResponder(log logging.Logger, trieDB *triedb.Database, trieKeyLength int, m *handlerMetrics, opts ...HandlerOption) *responder {
	return &responder{
		log:      log,
		trieDB:   trieDB,
		snapshot: options.As(opts...).snapshot,
		minKey:   make([]byte, trieKeyLength),
		metrics:  m,
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
//
// The snapshot's keys are hashes, so providing a snapshop SHOULD only be done
// when the trie's keys are [common.HashLength] bytes.
func WithSnapshot[V any, P SnapshotPointer[V]](s P) HandlerOption {
	return options.Func[handlerConfig](func(c *handlerConfig) {
		if s != nil {
			c.snapshot = s
		}
	})
}

func (r *responder) Respond(_ context.Context, nodeID ids.NodeID, reqPB *syncpb.GetLeafRequest) (*syncpb.GetLeafResponse, *avacommon.AppError) {
	r.metrics.count.Inc()
	start := time.Now()
	defer func() {
		r.metrics.processingTime.Observe(time.Since(start).Seconds())
	}()

	req, appErr := r.newRequest(reqPB)
	if appErr != nil {
		// A root this node has never held is a serving gap, not peer
		// misbehavior, so it is counted apart from the malformed requests.
		if appErr == errRootNotFound {
			r.metrics.missingRoot.Inc()
		} else {
			r.metrics.invalid.Inc()
		}
		r.log.Debug("rejecting request",
			zap.Stringer("nodeID", nodeID),
			zap.Error(appErr),
		)
		return nil, appErr
	}
	resp, err := getLeaves(r.metrics, req)
	switch {
	case errors.Is(err, errInvalidLeafKey):
		return nil, errInvalidRoot
	case err != nil:
		return nil, handlers.Fault(r.log, nodeID, err)
	}
	r.metrics.totalLeafs.Observe(float64(len(resp.GetKeys())))
	r.metrics.proofValsReturned.Observe(float64(len(resp.GetProofVals())))
	return resp, nil
}

// maxLimit caps leaves per response.
const maxLimit = 1024

var (
	errWrongStartKeyLength = &avacommon.AppError{
		Code:    3000,
		Message: "start key length mismatch",
	}
	errZeroKeyLimit = &avacommon.AppError{
		Code:    3001,
		Message: "zero key limit",
	}
	errWrongAccountHashLength = &avacommon.AppError{
		Code:    3002,
		Message: "account hash length mismatch",
	}
	errWrongRootLength = &avacommon.AppError{
		Code:    3003,
		Message: "root length mismatch",
	}
	errMissingRoot = &avacommon.AppError{
		Code:    3004,
		Message: "missing trie root",
	}
	errEmptyRoot = &avacommon.AppError{
		Code:    3005,
		Message: "empty trie root",
	}
	errRootNotFound = &avacommon.AppError{
		Code:    3006,
		Message: "requested trie root not found",
	}
	errInvalidRoot = &avacommon.AppError{
		Code:    3007,
		Message: "invalid trie root",
	}
)

// request holds the parsed read-only inputs of a [syncpb.GetLeafRequest] and
// the state opened to serve them.
type request struct {
	start []byte
	limit int
	// startsAtMin reports whether start is the lowest representable key.
	startsAtMin bool

	snapshot trieSnapshot
	trie     *trie.Trie
}

// newRequest opens the requested trie and returns the parsed request, or the
// rejection for a malformed or unservable reqPB.
func (r *responder) newRequest(reqPB *syncpb.GetLeafRequest) (*request, *avacommon.AppError) {
	start := reqPB.GetStartKey()
	switch {
	case len(start) == 0:
		start = r.minKey
	case len(start) != len(r.minKey):
		return nil, errWrongStartKeyLength
	}

	limit := reqPB.GetKeyLimit()
	if limit == 0 {
		return nil, errZeroKeyLimit
	}

	rootBytes := reqPB.GetRootHash()
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

	account := reqPB.GetAccountHash()
	if len(account) != 0 && len(account) != common.HashLength {
		return nil, errWrongAccountHashLength
	}

	t, err := trie.New(trie.TrieID(root), r.trieDB)
	if err != nil {
		return nil, errRootNotFound
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
	return &request{
		start:       start,
		limit:       int(min(limit, maxLimit)),
		startsAtMin: bytes.Equal(start, r.minKey),

		snapshot: snap,
		trie:     t,
	}, nil
}

// getLeaves returns the response holding the leaf range and its proof.
func getLeaves(m *handlerMetrics, req *request) (*syncpb.GetLeafResponse, error) {
	r := newLeafRange(req.start, req.limit)
	readStart := time.Now()
	more, err := fillFromSnapshot(m, req.snapshot, req.trie, r)
	if err != nil {
		return nil, err
	}
	if more && !r.full() {
		more, err = fillFromTrie(m, req.trie, r)
		if err != nil {
			return nil, err
		}
	}
	m.readTime.Observe(time.Since(readStart).Seconds())

	resp := &syncpb.GetLeafResponse{
		Keys:   r.keys,
		Values: r.vals,
	}
	// [trie.VerifyRangeProof] allows an empty proof when proving a full trie.
	// This uses less bandwidth and is faster to verify.
	if req.startsAtMin && !more {
		return resp, nil
	}

	proofDB, err := newRangeProof(m, req.trie, req.start, r.keys)
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

	return NextKey(l.keys[len(l.keys)-1])
}

// fillFromSnapshot appends leaves from [leafRange.next] to r, up to the
// capacity, serving from the snapshot where it agrees with the trie. A nil
// snapshot appends nothing. It returns whether the trie may hold leaves past
// the response.
func fillFromSnapshot(m *handlerMetrics, s trieSnapshot, t *trie.Trie, r *leafRange) (bool, error) {
	if s == nil {
		return true, nil
	}
	m.snapshotReadAttempt.Inc()

	next := r.next()
	readStart := time.Now()
	keys, vals, err := readSnapshot(
		s,
		common.BytesToHash(next),
		r.space(),
	)
	m.snapshotReadTime.Observe(time.Since(readStart).Seconds())
	if err != nil || len(keys) == 0 {
		if err != nil {
			m.snapshotReadError.Inc()
		}
		// Since the snapshot is volatile, an error or an empty read falls
		// back to the trie.
		return true, nil
	}

	// The whole read often proves in one shot, avoiding per-segment proofs.
	valid, more, err := isRangeValid(
		m,
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
		m.snapshotReadSuccess.Inc()
		// TODO(StephenButtolph): In the case where the snapshot proved
		// correctly and it isn't the full trie, we could avoid re-proving the
		// range and just return the proof here.
		r.append(keys, vals)
		return more, nil
	}

	return fillFromSegments(m, t, r, keys, vals)
}

// segmentLen balances proof overhead against waste. Larger segments amortize
// per-segment proofs, smaller segments discard fewer leaves when one diverges.
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
func fillFromSegments(m *handlerMetrics, t *trie.Trie, r *leafRange, keys, vals [][]byte) (bool, error) {
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

		valid, moreAfterSeg, err := isRangeValid(m, t, segment)
		if err != nil {
			return false, err
		}
		if !valid {
			m.snapshotSegmentInvalid.Inc()
			hasGap = true
			continue
		}
		m.snapshotSegmentValid.Inc()
		if hasGap {
			// The trie supplies the gap, the segment supplies the rest.
			if _, err := fillFromTrie(m, t, r, withBefore(segment.keys[0])); err != nil {
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
	hasEnd bool
	end    []byte
}

// fillOption configures [fillFromTrie].
type fillOption = options.Option[fillConfig]

// withBefore stops the fill before end.
func withBefore(end []byte) fillOption {
	return options.Func[fillConfig](func(c *fillConfig) {
		c.hasEnd = true
		c.end = end
	})
}

// fillFromTrie appends trie leaves from [leafRange.next] to r, up to the
// capacity. [withBefore] stops the fill before end. It returns whether the trie
// holds leaves past the response.
func fillFromTrie(m *handlerMetrics, t *trie.Trie, r *leafRange, opts ...fillOption) (_ bool, retErr error) {
	defer func() {
		if retErr != nil {
			m.trieError.Inc()
		}
	}()

	c := options.As(opts...)
	for pair, err := range LeafIterator(t, r.next()) {
		if err != nil {
			return false, err
		}

		hitEnd := c.hasEnd && bytes.Compare(pair.Key, c.end) >= 0
		if hitEnd || r.full() {
			return true, nil
		}
		r.add(pair.Key, pair.Value)
	}
	return false, nil
}

// isRangeValid range-proves r against the trie. valid reports whether the proof
// succeeded, and more reports whether the trie holds leaves past r.
func isRangeValid(m *handlerMetrics, t *trie.Trie, r *leafRange) (valid, more bool, _ error) {
	proofDB, err := newRangeProof(m, t, r.start, r.keys)
	if err != nil {
		return false, false, err
	}
	more, proofErr := trie.VerifyRangeProof(t.Hash(), r.start, r.keys, r.vals, proofDB)
	return proofErr == nil, more, nil
}

// newRangeProof returns a range proof for [start, last(keys)], observing the
// generation time and counting failures on m.
func newRangeProof(m *handlerMetrics, t *trie.Trie, start []byte, keys [][]byte) (_ *memorydb.Database, retErr error) {
	proofStart := time.Now()
	defer func() {
		m.generateRangeProofTime.Observe(time.Since(proofStart).Seconds())
		if retErr != nil {
			m.proofError.Inc()
		}
	}()

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

// NextKey adds 1 to a copy of k, with carry. All-0xff wraps to all-zeros.
func NextKey(k []byte) []byte {
	k = slices.Clone(k)
	for i := len(k) - 1; i >= 0; i-- {
		if k[i] < 0xff {
			k[i]++
			return k
		}
		k[i] = 0
	}
	return k
}
