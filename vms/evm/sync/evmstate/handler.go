// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"slices"

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

// SnapshotReader opens flat iterators over the snapshot leaves. The
// implementation does not need to guarantee anything about the state it
// serves. A state that happens to match a request speeds up the response but
// never changes it.
type SnapshotReader interface {
	AccountIterator(start common.Hash) (snapshot.AccountIterator, error)
	StorageIterator(account, start common.Hash) (snapshot.StorageIterator, error)
}

var _ handlers.Responder[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse] = (*responder)(nil)

type responder struct {
	log      logging.Logger
	trieDB   *triedb.Database
	snapshot SnapshotReader // optional
	minKey   []byte         // read-only stand-in for an absent start key
	maxKey   []byte         // read-only stand-in for an absent end key
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
	snapshot SnapshotReader
}

// HandlerOption configures [RegisterHandler].
type HandlerOption = options.Option[handlerConfig]

// SnapshotReaderPointer is a type constraint for a pointer that implements
// [SnapshotReader].
//
// It can be used to avoid typed-nil interface panics.
type SnapshotReaderPointer[V any] interface {
	SnapshotReader
	*V
}

// WithSnapshot serves leaves from the snapshot where it agrees with the trie,
// falling back to trie iteration everywhere else. A nil snapshot is equivalent
// to not providing this option.
func WithSnapshot[V any, P SnapshotReaderPointer[V]](s P) HandlerOption {
	return options.Func[handlerConfig](func(c *handlerConfig) {
		if s != nil {
			c.snapshot = s
		}
	})
}

var (
	errZeroKeyLimit = &avacommon.AppError{
		Code:    3000,
		Message: "zero key limit",
	}
	errMissingRoot = &avacommon.AppError{
		Code:    3001,
		Message: "missing trie root",
	}
	errEmptyRoot = &avacommon.AppError{
		Code:    3002,
		Message: "empty trie root",
	}
	errWrongAccountHashLength = &avacommon.AppError{
		Code:    3003,
		Message: "account hash length mismatch",
	}
	errWrongStartKeyLength = &avacommon.AppError{
		Code:    3004,
		Message: "start key length mismatch",
	}
	errWrongEndKeyLength = &avacommon.AppError{
		Code:    3005,
		Message: "end key length mismatch",
	}
	errStartAfterEnd = &avacommon.AppError{
		Code:    3006,
		Message: "start key after end key",
	}
	errRootNotFound = &avacommon.AppError{
		Code:    3007,
		Message: "requested trie root not found",
	}
)

func (r *responder) Respond(_ context.Context, nodeID ids.NodeID, req *syncpb.GetLeafRequest) (*syncpb.GetLeafResponse, *avacommon.AppError) {
	if appErr := validateRequest(req, len(r.minKey)); appErr != nil {
		r.log.Debug("rejecting request",
			zap.Stringer("nodeID", nodeID),
			zap.Error(appErr),
		)
		return nil, appErr
	}
	q, appErr := newQuery(r, nodeID, req)
	if appErr != nil {
		return nil, appErr
	}
	if err := q.collect(); err != nil {
		return nil, handlers.Fault(q.log, nodeID, err)
	}
	return q.resp, nil
}

// validateRequest returns the rejection for a malformed req, nil when it is
// valid.
func validateRequest(req *syncpb.GetLeafRequest, trieKeyLength int) *avacommon.AppError {
	if req.GetKeyLimit() == 0 {
		return errZeroKeyLimit
	}

	switch root := common.BytesToHash(req.GetRootHash()); root {
	case common.Hash{}:
		return errMissingRoot
	case types.EmptyRootHash:
		return errEmptyRoot
	}

	switch account, start, end := req.GetAccountHash(), req.GetStartKey(), req.GetEndKey(); {
	case len(account) != 0 && len(account) != common.HashLength:
		return errWrongAccountHashLength
	case len(start) != 0 && len(start) != trieKeyLength:
		return errWrongStartKeyLength
	case len(end) != 0 && len(end) != trieKeyLength:
		return errWrongEndKeyLength
	case len(end) != 0 && bytes.Compare(start, end) > 0:
		return errStartAfterEnd
	}
	return nil
}

// query holds one in-flight leaf request.
type query struct {
	log       logging.Logger
	startKey  []byte
	endKey    []byte
	rootHash  common.Hash
	account   common.Hash // populated when isStorage
	isStorage bool
	limit     int

	trie     *trie.Trie
	snapshot SnapshotReader
	minKey   []byte

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

	start := req.GetStartKey()
	if len(start) == 0 {
		start = r.minKey
	}
	end := req.GetEndKey()
	if len(end) == 0 {
		end = r.maxKey
	}
	account := req.GetAccountHash()
	limit := int(min(req.GetKeyLimit(), MaxLeavesLimit))
	return &query{
		log:       r.log,
		startKey:  start,
		endKey:    end,
		rootHash:  root,
		account:   common.BytesToHash(account),
		isStorage: len(account) != 0,
		limit:     limit,

		trie:     t,
		snapshot: r.snapshot,
		minKey:   r.minKey,

		resp: &syncpb.GetLeafResponse{
			Keys:   make([][]byte, 0, limit),
			Values: make([][]byte, 0, limit),
		},
	}, nil
}

func (q *query) atLimit() bool {
	return len(q.resp.Keys) >= q.limit
}

// appendLeaves appends what the limit allows. kept below len(keys) means the
// segment was trimmed.
func (q *query) appendLeaves(keys, vals [][]byte) (kept int) {
	kept = min(len(keys), q.limit-len(q.resp.Keys))
	q.resp.Keys = append(q.resp.Keys, keys[:kept]...)
	q.resp.Values = append(q.resp.Values, vals[:kept]...)
	return kept
}

// collect fills [query.resp] with the leaf range and its proof.
func (q *query) collect() error {
	var (
		// Only a proof establishes what lies past the response, so more starts
		// pessimistic.
		more = true
		err  error
	)
	if q.snapshot != nil {
		more, err = q.fillFromSnapshot()
		if err != nil {
			return err
		}
	}
	if more && !q.atLimit() {
		more, err = q.fillFromTrie(q.endKey)
		if err != nil {
			return err
		}
	}
	return q.attachProof(more)
}

// attachProof proves the response range.
func (q *query) attachProof(more bool) error {
	// [trie.VerifyRangeProof] allows an empty proof when proving a full trie.
	// This uses less bandwidth and is faster to verify.
	if bytes.Equal(q.startKey, q.minKey) && !more {
		return nil
	}

	proofDB, err := newRangeProof(q.trie, q.startKey, q.resp.Keys)
	if err != nil {
		return err
	}
	q.resp.ProofVals, err = dbValues(proofDB)
	return err
}

// fillFromSnapshot appends the snapshot leaves that were able to be proven
// as correct to [query.resp]. It returns whether the trie may hold leaves past
// the last key appended to the response.
func (q *query) fillFromSnapshot() (bool, error) {
	snapKeys, snapVals := q.readFromSnapshot()
	if len(snapKeys) == 0 {
		return true, nil // Unavailable or empty here, use the trie.
	}

	// Fast path: validate the entire range against the trie in one shot.
	valid, more, err := isRangeValid(q.trie, q.startKey, snapKeys, snapVals)
	if err != nil {
		return false, err
	}
	if valid {
		q.appendLeaves(snapKeys, snapVals)
		return more, nil
	}

	return q.fillFromSegments(snapKeys, snapVals)
}

const snapshotSegmentLen = 64

// fillFromSegments appends the segments of snapKeys and snapVals that prove
// against the trie to [query.resp], bridging failed segments with leaves from
// the trie. It returns whether the trie may hold leaves past the response.
//
// snapKeys=[A B C D E], snapshotSegmentLen=2, [C D] diverged:
//
//	[A B] proves    append        -> resp=[A B]
//	[C D] fails     mark the gap  -> resp=[A B]
//	[E]   proves    bridge to E   -> resp=[A B C D]
//	                append past E -> resp=[A B C D E]
func (q *query) fillFromSegments(snapKeys, snapVals [][]byte) (bool, error) {
	hasGap := false
	// Only a proved segment answers this, so it starts pessimistic. A stale
	// answer is safe, collect re-derives it below the limit.
	trieHasMore := true

	for i := 0; i < len(snapKeys); i += snapshotSegmentLen {
		// Without a gap the proof starts at nextKey, so the span back to the
		// response is covered too.
		var startKey []byte
		if hasGap {
			startKey = snapKeys[i]
		} else {
			startKey = q.nextKey()
		}
		end := min(i+snapshotSegmentLen, len(snapKeys))
		valid, more, err := isRangeValid(q.trie, startKey, snapKeys[i:end], snapVals[i:end])
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
			if _, err := q.fillFromTrie(snapKeys[i]); err != nil {
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
	return trieHasMore, nil
}

// readFromSnapshot pulls leaves in [startKey, endKey] up to [query.limit]. They
// are unvalidated, the caller range-proves them against the requested root.
func (q *query) readFromSnapshot() ([][]byte, [][]byte) {
	log := q.log.With(
		zap.Bool("isStorage", q.isStorage),
		zap.Stringer("account", q.account),
	)

	it, err := q.newSnapshotIterator()
	if err != nil {
		log.Debug("snapshot read abandoned",
			zap.String("reason", "iterator unavailable"),
			zap.Error(err),
		)
		return nil, nil
	}
	defer it.Release()

	keys := make([][]byte, 0, q.limit)
	vals := make([][]byte, 0, q.limit)
	for it.Next() {
		k := it.Hash().Bytes()
		if bytes.Compare(k, q.endKey) > 0 || len(keys) >= q.limit {
			break
		}
		v, err := it.Value()
		if err != nil {
			log.Debug("snapshot read abandoned",
				zap.String("reason", "leaf encoding failed"),
				zap.Error(err),
			)
			return nil, nil
		}
		keys = append(keys, k)
		vals = append(vals, v)
	}
	if err := it.Error(); err != nil {
		log.Debug("snapshot read abandoned",
			zap.String("reason", "iteration failed"),
			zap.Error(err),
		)
		return nil, nil
	}
	return keys, vals
}

// iterator walks snapshot leaves. Value returns the trie-encoded value at the
// cursor.
type iterator interface {
	snapshot.Iterator
	Value() ([]byte, error)
}

type accountIterator struct{ snapshot.AccountIterator }
type storageIterator struct{ snapshot.StorageIterator }

func (it accountIterator) Value() ([]byte, error) { return types.FullAccountRLP(it.Account()) }
func (it storageIterator) Value() ([]byte, error) { return it.Slot(), nil }

// newSnapshotIterator returns an iterator over accounts, or over the requested
// account's storage. The iterator serves some recent state, not necessarily
// the state rooted at [query.rootHash].
//
// If a nil error is returned, the iterator MUST be released.
func (q *query) newSnapshotIterator() (iterator, error) {
	start := common.BytesToHash(q.startKey)
	if q.isStorage {
		it, err := q.snapshot.StorageIterator(q.account, start)
		return storageIterator{it}, err
	}
	it, err := q.snapshot.AccountIterator(start)
	return accountIterator{it}, err
}

// fillFromTrie appends trie leaves from [query.nextKey] through end to
// [query.resp], up to [query.limit]. It returns whether the trie holds leaves
// past the response.
func (q *query) fillFromTrie(end []byte) (bool, error) {
	// While [trie.Trie.NodeIterator] documents that it starts iterating after
	// the given key, it actually starts at the key if it exists.
	nodeIt, err := q.trie.NodeIterator(q.nextKey())
	if err != nil {
		return false, err
	}
	it := trie.NewIterator(nodeIt)

	for it.Next() {
		if bytes.Compare(it.Key, end) > 0 || q.atLimit() {
			return true, it.Err
		}
		q.resp.Keys = append(q.resp.Keys, it.Key)
		q.resp.Values = append(q.resp.Values, it.Value)
	}
	return false, it.Err
}

// nextKey returns where trie iteration resumes after the response.
func (q *query) nextKey() []byte {
	if len(q.resp.Keys) == 0 {
		return q.startKey
	}

	last := q.resp.Keys[len(q.resp.Keys)-1]
	next := slices.Clone(last)
	incrementBytes(next)
	return next
}

// isRangeValid range-proves keys/vals against the trie from start. valid
// reports whether the proof succeeded, and more reports whether the trie holds
// leaves past keys.
func isRangeValid(
	t *trie.Trie,
	start []byte,
	keys [][]byte,
	vals [][]byte,
) (valid, more bool, _ error) {
	proofDB, err := newRangeProof(t, start, keys)
	if err != nil {
		return false, false, err
	}
	more, proofErr := trie.VerifyRangeProof(t.Hash(), start, keys, vals, proofDB)
	return proofErr == nil, more, nil
}

// newRangeProof returns a range proof for [start, last(keys)].
func newRangeProof(
	t *trie.Trie,
	start []byte,
	keys [][]byte,
) (*memorydb.Database, error) {
	proofDB := memorydb.New()
	if err := t.Prove(start, proofDB); err != nil {
		return nil, err
	}
	if len(keys) > 0 {
		end := keys[len(keys)-1]
		if err := t.Prove(end, proofDB); err != nil {
			return nil, err
		}
	}
	return proofDB, nil
}

func dbValues(db *memorydb.Database) ([][]byte, error) {
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
