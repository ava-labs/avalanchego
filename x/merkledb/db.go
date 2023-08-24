// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	DefaultEvictionBatchSize = 100
	RootPath                 = EmptyPath
	// TODO: name better
	rebuildViewSizeFractionOfCacheSize = 50
	minRebuildViewSizePerCommit        = 1000
)

var (
	_ TrieView = (*merkleDB)(nil)
	_ MerkleDB = (*merkleDB)(nil)

	codec = newCodec()

	rootKey                 []byte
	metadataPrefix          = []byte("metadata")
	cleanShutdownKey        = []byte(string(metadataPrefix) + "cleanShutdown")
	hadCleanShutdown        = []byte{1}
	didNotHaveCleanShutdown = []byte{0}

	errSameRoot = errors.New("start and end root are the same")
)

type ChangeProofer interface {
	// GetChangeProof returns a proof for a subset of the key/value changes in key range
	// [start, end] that occurred between [startRootID] and [endRootID].
	// Returns at most [maxLength] key/value pairs.
	// Returns [ErrInsufficientHistory] if this node has insufficient history
	// to generate the proof.
	GetChangeProof(
		ctx context.Context,
		startRootID ids.ID,
		endRootID ids.ID,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		maxLength int,
	) (*ChangeProof, error)

	// Returns nil iff all of the following hold:
	//   - [start] <= [end].
	//   - [proof] is non-empty.
	//   - All keys in [proof.KeyValues] and [proof.DeletedKeys] are in [start, end].
	//     If [start] is nothing, all keys are considered > [start].
	//     If [end] is nothing, all keys are considered < [end].
	//   - [proof.KeyValues] and [proof.DeletedKeys] are sorted in order of increasing key.
	//   - [proof.StartProof] and [proof.EndProof] are well-formed.
	//   - When the keys in [proof.KeyValues] are added to [db] and the keys in [proof.DeletedKeys]
	//     are removed from [db], the root ID of [db] is [expectedEndRootID].
	//
	// This is defined on Database instead of ChangeProof because it accesses
	// database internals.
	VerifyChangeProof(
		ctx context.Context,
		proof *ChangeProof,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		expectedEndRootID ids.ID,
	) error

	// CommitChangeProof commits the key/value pairs within the [proof] to the db.
	CommitChangeProof(ctx context.Context, proof *ChangeProof) error
}

type RangeProofer interface {
	// GetRangeProofAtRoot returns a proof for the key/value pairs in this trie within the range
	// [start, end] when the root of the trie was [rootID].
	// If [start] is Nothing, there's no lower bound on the range.
	// If [end] is Nothing, there's no upper bound on the range.
	GetRangeProofAtRoot(
		ctx context.Context,
		rootID ids.ID,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		maxLength int,
	) (*RangeProof, error)

	// CommitRangeProof commits the key/value pairs within the [proof] to the db.
	// [start] is the smallest key in the range this [proof] covers.
	CommitRangeProof(ctx context.Context, start maybe.Maybe[[]byte], proof *RangeProof) error
}

type MerkleDB interface {
	database.Database
	Trie
	MerkleRootGetter
	ProofGetter
	ChangeProofer
	RangeProofer
}

type Config struct {
	// The number of nodes that are evicted from the cache and written to
	// disk at a time.
	EvictionBatchSize int
	// The number of changes to the database that we store in memory in order to
	// serve change proofs.
	HistoryLength             int
	ValueNodeCacheSize        int
	IntermediateNodeCacheSize int
	// If [Reg] is nil, metrics are collected locally but not exported through
	// Prometheus.
	// This may be useful for testing.
	Reg    prometheus.Registerer
	Tracer trace.Tracer
}

// merkleDB can only be edited by committing changes from a trieView.
type merkleDB struct {
	// Must be held when reading/writing fields.
	lock sync.RWMutex

	// Must be held when preparing work to be committed to the DB.
	// Used to prevent editing of the trie without restricting read access
	// until the full set of changes is ready to be written.
	// Should be held before taking [db.lock]
	commitLock sync.RWMutex

	underlyingDB database.Database

	valueNodesDB        *valueNodeDB
	intermediateNodesDB *intermediateNodeDB

	// Stores change lists. Used to serve change proofs and construct
	// historical views of the trie.
	history *trieHistory

	// True iff the db has been closed.
	closed bool

	metrics merkleMetrics

	tracer trace.Tracer

	// The root of this trie.
	root *node

	// Valid children of this trie.
	childViews []*trieView
}

// New returns a new merkle database.
func New(ctx context.Context, db database.Database, config Config) (MerkleDB, error) {
	metrics, err := newMetrics("merkleDB", config.Reg)
	if err != nil {
		return nil, err
	}
	return newDatabase(ctx, db, config, metrics)
}

func newDatabase(
	ctx context.Context,
	db database.Database,
	config Config,
	metrics merkleMetrics,
) (*merkleDB, error) {
	trieDB := &merkleDB{
		metrics:             metrics,
		underlyingDB:        db,
		valueNodesDB:        newValueNodeDB(db, metrics, config.ValueNodeCacheSize),
		intermediateNodesDB: newIntermediateNodeDB(db, metrics, config.IntermediateNodeCacheSize, config.EvictionBatchSize),
		history:             newTrieHistory(config.HistoryLength),
		tracer:              config.Tracer,
		childViews:          make([]*trieView, 0, defaultPreallocationSize),
	}

	root, err := trieDB.initializeRootIfNeeded()
	if err != nil {
		return nil, err
	}

	// add current root to history (has no changes)
	trieDB.history.record(&changeSummary{
		rootID: root,
		values: map[path]*change[maybe.Maybe[[]byte]]{},
		nodes:  map[path]*change[*node]{},
	})

	shutdownType, err := trieDB.underlyingDB.Get(cleanShutdownKey)
	switch err {
	case nil:
		if bytes.Equal(shutdownType, didNotHaveCleanShutdown) {
			if err := trieDB.rebuild(ctx); err != nil {
				return nil, err
			}
		}
	case database.ErrNotFound:
		// If the marker wasn't found then the DB is being created for the first
		// time and there is nothing to do.
	default:
		return nil, err
	}

	// mark that the db has not yet been cleanly closed
	err = trieDB.underlyingDB.Put(cleanShutdownKey, didNotHaveCleanShutdown)
	return trieDB, err
}

// Deletes every intermediate node and rebuilds them by re-adding every key/value.
// TODO: make this more efficient by only clearing out the stale portions of the trie.
func (db *merkleDB) rebuild(ctx context.Context) error {
	db.root = newNode(nil, RootPath)

	opsSizeLimit := math.Max(
		db.valueNodesDB.nodeCache.maxSize/rebuildViewSizeFractionOfCacheSize,
		minRebuildViewSizePerCommit,
	)
	intermediateNodeIt := db.underlyingDB.NewIteratorWithPrefix(intermediateNodePrefixBytes)
	defer intermediateNodeIt.Release()
	intermediateBatch := db.underlyingDB.NewBatch()
	count := 0
	// delete all intermediate nodes
	for intermediateNodeIt.Next() {
		if count >= opsSizeLimit {
			if err := intermediateBatch.Write(); err != nil {
				return err
			}
			intermediateBatch = db.underlyingDB.NewBatch()
		}
		if err := intermediateBatch.Delete(intermediateNodeIt.Key()); err != nil {
			return err
		}
	}
	if err := intermediateNodeIt.Error(); err != nil {
		return err
	}
	if err := intermediateBatch.Write(); err != nil {
		return err
	}

	// add all key/values back into the database
	currentOps := make([]database.BatchOp, 0, opsSizeLimit)
	valueIt := db.NewIterator()
	defer valueIt.Release()
	for valueIt.Next() {
		if len(currentOps) >= opsSizeLimit {
			view, err := db.newUntrackedView(currentOps)
			if err != nil {
				return err
			}
			if err := view.commitToDB(ctx); err != nil {
				return err
			}
			currentOps = make([]database.BatchOp, 0, opsSizeLimit)
		}

		currentOps = append(currentOps, database.BatchOp{
			Key:   valueIt.Key(),
			Value: valueIt.Value(),
		})
	}
	if err := valueIt.Error(); err != nil {
		return err
	}
	view, err := db.newUntrackedView(currentOps)
	if err != nil {
		return err
	}
	if err := view.commitToDB(ctx); err != nil {
		return err
	}
	return db.Compact(nil, nil)
}

func (db *merkleDB) CommitChangeProof(ctx context.Context, proof *ChangeProof) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.Closed() {
		return database.ErrClosed
	}
	ops := make([]database.BatchOp, len(proof.KeyChanges))
	for i, kv := range proof.KeyChanges {
		ops[i] = database.BatchOp{
			Key:    kv.Key,
			Value:  kv.Value.Value(),
			Delete: kv.Value.IsNothing(),
		}
	}

	view, err := db.newUntrackedView(ops)
	if err != nil {
		return err
	}
	return view.commitToDB(ctx)
}

func (db *merkleDB) CommitRangeProof(ctx context.Context, start maybe.Maybe[[]byte], proof *RangeProof) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.Closed() {
		return database.ErrClosed
	}

	ops := make([]database.BatchOp, len(proof.KeyValues))
	keys := set.NewSet[string](len(proof.KeyValues))
	for i, kv := range proof.KeyValues {
		keys.Add(string(kv.Key))
		ops[i] = database.BatchOp{
			Key:   kv.Key,
			Value: kv.Value,
		}
	}

	var largestKey []byte
	if len(proof.KeyValues) > 0 {
		largestKey = proof.KeyValues[len(proof.KeyValues)-1].Key
	}
	keysToDelete, err := db.getKeysNotInSet(start.Value(), largestKey, keys)
	if err != nil {
		return err
	}
	for _, keyToDelete := range keysToDelete {
		ops = append(ops, database.BatchOp{
			Key:    keyToDelete,
			Delete: true,
		})
	}

	// Don't need to lock [view] because nobody else has a reference to it.
	view, err := db.newUntrackedView(ops)
	if err != nil {
		return err
	}

	return view.commitToDB(ctx)
}

func (db *merkleDB) Compact(start []byte, limit []byte) error {
	if db.Closed() {
		return database.ErrClosed
	}
	return db.underlyingDB.Compact(start, limit)
}

func (db *merkleDB) Closed() bool {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.closed
}

func (db *merkleDB) Close() error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	db.closed = true
	db.valueNodesDB.Close()
	// Flush [nodeCache] to persist intermediary nodes to disk.
	if err := db.intermediateNodesDB.Flush(); err != nil {
		// There was an error during cache eviction.
		// Don't commit to disk.
		return err
	}

	// Successfully wrote intermediate nodes.
	return db.underlyingDB.Put(cleanShutdownKey, hadCleanShutdown)
}

func (db *merkleDB) Get(key []byte) ([]byte, error) {
	// this is a duplicate because the database interface doesn't support
	// contexts, which are used for tracing
	return db.GetValue(context.Background(), key)
}

func (db *merkleDB) GetValues(ctx context.Context, keys [][]byte) ([][]byte, []error) {
	_, span := db.tracer.Start(ctx, "MerkleDB.GetValues", oteltrace.WithAttributes(
		attribute.Int("keyCount", len(keys)),
	))
	defer span.End()

	// Lock to ensure no commit happens during the reads.
	db.lock.RLock()
	defer db.lock.RUnlock()

	values := make([][]byte, len(keys))
	errors := make([]error, len(keys))
	for i, key := range keys {
		values[i], errors[i] = db.getValueCopy(newPath(key))
	}
	return values, errors
}

// GetValue returns the value associated with [key].
// Returns database.ErrNotFound if it doesn't exist.
func (db *merkleDB) GetValue(ctx context.Context, key []byte) ([]byte, error) {
	_, span := db.tracer.Start(ctx, "MerkleDB.GetValue")
	defer span.End()

	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getValueCopy(newPath(key))
}

// getValueCopy returns a copy of the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
// Assumes [db.lock] is read locked.
func (db *merkleDB) getValueCopy(key path) ([]byte, error) {
	val, err := db.getValueWithoutLock(key)
	if err != nil {
		return nil, err
	}
	return slices.Clone(val), nil
}

// getValue returns the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
// Assumes [db.lock] isn't held.
func (db *merkleDB) getValue(key path) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getValueWithoutLock(key)
}

// getValueWithoutLock returns the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
// Assumes [db.lock] is read locked.
func (db *merkleDB) getValueWithoutLock(key path) ([]byte, error) {
	if db.closed {
		return nil, database.ErrClosed
	}

	n, err := db.getNode(key, true)
	if err != nil {
		return nil, err
	}
	if n.value.IsNothing() {
		return nil, database.ErrNotFound
	}
	return n.value.Value(), nil
}

func (db *merkleDB) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	_, span := db.tracer.Start(ctx, "MerkleDB.GetMerkleRoot")
	defer span.End()

	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return ids.Empty, database.ErrClosed
	}

	return db.getMerkleRoot(), nil
}

// Assumes [db.lock] is read locked.
func (db *merkleDB) getMerkleRoot() ids.ID {
	return db.root.id
}

func (db *merkleDB) GetProof(ctx context.Context, key []byte) (*Proof, error) {
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	return db.getProof(ctx, key)
}

// Assumes [db.commitLock] is read locked.
// Assumes [db.lock] is not held
func (db *merkleDB) getProof(ctx context.Context, key []byte) (*Proof, error) {
	if db.Closed() {
		return nil, database.ErrClosed
	}

	view, err := db.newUntrackedView(nil)
	if err != nil {
		return nil, err
	}
	// Don't need to lock [view] because nobody else has a reference to it.
	return view.getProof(ctx, key)
}

func (db *merkleDB) GetRangeProof(
	ctx context.Context,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) (*RangeProof, error) {
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	return db.getRangeProofAtRoot(ctx, db.getMerkleRoot(), start, end, maxLength)
}

func (db *merkleDB) GetRangeProofAtRoot(
	ctx context.Context,
	rootID ids.ID,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) (*RangeProof, error) {
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	return db.getRangeProofAtRoot(ctx, rootID, start, end, maxLength)
}

// Assumes [db.commitLock] is read locked.
// Assumes [db.lock] is not held
func (db *merkleDB) getRangeProofAtRoot(
	ctx context.Context,
	rootID ids.ID,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) (*RangeProof, error) {
	if db.Closed() {
		return nil, database.ErrClosed
	}
	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	historicalView, err := db.getHistoricalViewForRange(rootID, start, end)
	if err != nil {
		return nil, err
	}
	return historicalView.GetRangeProof(ctx, start, end, maxLength)
}

func (db *merkleDB) GetChangeProof(
	ctx context.Context,
	startRootID ids.ID,
	endRootID ids.ID,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) (*ChangeProof, error) {
	if start.HasValue() && end.HasValue() && bytes.Compare(start.Value(), end.Value()) == 1 {
		return nil, ErrStartAfterEnd
	}
	if startRootID == endRootID {
		return nil, errSameRoot
	}

	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	if db.Closed() {
		return nil, database.ErrClosed
	}

	changes, err := db.history.getValueChanges(startRootID, endRootID, start, end, maxLength)
	if err != nil {
		return nil, err
	}

	// [changedKeys] are a subset of the keys that were added or had their
	// values modified between [startRootID] to [endRootID] sorted in increasing
	// order.
	changedKeys := maps.Keys(changes.values)
	utils.Sort(changedKeys)

	result := &ChangeProof{
		KeyChanges: make([]KeyChange, 0, len(changedKeys)),
	}

	for _, key := range changedKeys {
		change := changes.values[key]
		serializedKey := key.Serialize().Value

		result.KeyChanges = append(result.KeyChanges, KeyChange{
			Key: serializedKey,
			// create a copy so edits of the []byte don't affect the db
			Value: maybe.Bind(change.after, slices.Clone[[]byte]),
		})
	}

	largestKey := end
	if len(result.KeyChanges) > 0 {
		largestKey = maybe.Some(result.KeyChanges[len(result.KeyChanges)-1].Key)
	}

	// Since we hold [db.commitlock] we must still have sufficient
	// history to recreate the trie at [endRootID].
	historicalView, err := db.getHistoricalViewForRange(endRootID, start, largestKey)
	if err != nil {
		return nil, err
	}

	if largestKey.HasValue() {
		endProof, err := historicalView.getProof(ctx, largestKey.Value())
		if err != nil {
			return nil, err
		}
		result.EndProof = endProof.Path
	}

	if start.HasValue() {
		startProof, err := historicalView.getProof(ctx, start.Value())
		if err != nil {
			return nil, err
		}
		result.StartProof = startProof.Path

		// strip out any common nodes to reduce proof size
		commonNodeIndex := 0
		for ; commonNodeIndex < len(result.StartProof) &&
			commonNodeIndex < len(result.EndProof) &&
			result.StartProof[commonNodeIndex].KeyPath.Equal(result.EndProof[commonNodeIndex].KeyPath); commonNodeIndex++ {
		}
		result.StartProof = result.StartProof[commonNodeIndex:]
	}

	// Note that one of the following must be true:
	//  - [result.StartProof] is non-empty.
	//  - [result.EndProof] is non-empty.
	//  - [result.KeyValues] is non-empty.
	//  - [result.DeletedKeys] is non-empty.
	// If all of these were false, it would mean that no
	// [start] and [end] were given, and no diff between
	// the trie at [startRootID] and [endRootID] was found.
	// Since [startRootID] != [endRootID], this is impossible.
	return result, nil
}

// NewView returns a new view on top of this trie.
// Changes made to the view will only be reflected in the original trie if Commit is called.
// Assumes [db.commitLock] and [db.lock] aren't held.
func (db *merkleDB) NewView(_ context.Context, batchOps []database.BatchOp) (TrieView, error) {
	// ensure the db doesn't change while creating the new view
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	newView, err := db.newUntrackedView(batchOps)
	if err != nil {
		return nil, err
	}

	// ensure access to childViews is protected
	db.lock.Lock()
	defer db.lock.Unlock()

	db.childViews = append(db.childViews, newView)
	return newView, nil
}

// Returns a new view that isn't tracked in [db.childViews].
// For internal use only, namely in methods that create short-lived views.
// Assumes [db.lock] isn't held and [db.commitLock] is read locked.
func (db *merkleDB) newUntrackedView(batchOps []database.BatchOp) (*trieView, error) {
	if db.Closed() {
		return nil, database.ErrClosed
	}

	newView, err := newTrieView(db, db, db.root.clone(), batchOps)
	if err != nil {
		return nil, err
	}
	return newView, nil
}

func (db *merkleDB) Has(k []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return false, database.ErrClosed
	}

	_, err := db.getValueWithoutLock(newPath(k))
	if err == database.ErrNotFound {
		return false, nil
	}
	return err == nil, err
}

func (db *merkleDB) HealthCheck(ctx context.Context) (interface{}, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}
	return db.underlyingDB.HealthCheck(ctx)
}

func (db *merkleDB) NewBatch() database.Batch {
	return &batch{
		db: db,
	}
}

func (db *merkleDB) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

func (db *merkleDB) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

func (db *merkleDB) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (db *merkleDB) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	return db.valueNodesDB.newIteratorWithStartAndPrefix(start, prefix)
}

func (db *merkleDB) Put(k, v []byte) error {
	return db.PutContext(context.Background(), k, v)
}

// Same as [Put] but takes in a context used for tracing.
func (db *merkleDB) PutContext(ctx context.Context, k, v []byte) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	view, err := db.newUntrackedView([]database.BatchOp{
		{
			Key:   k,
			Value: v,
		},
	})
	if err != nil {
		return err
	}
	return view.commitToDB(ctx)
}

func (db *merkleDB) Delete(key []byte) error {
	return db.DeleteContext(context.Background(), key)
}

func (db *merkleDB) DeleteContext(ctx context.Context, key []byte) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.Closed() {
		return database.ErrClosed
	}

	view, err := db.newUntrackedView([]database.BatchOp{
		{
			Key:    key,
			Delete: true,
		},
	})
	if err != nil {
		return err
	}
	return view.commitToDB(ctx)
}

// Assumes [db.lock] is not held
func (db *merkleDB) commitBatch(ops []database.BatchOp) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.Closed() {
		return database.ErrClosed
	}

	view, err := db.newUntrackedView(ops)
	if err != nil {
		return err
	}
	return view.commitToDB(context.Background())
}

// commitChanges commits the changes in [trieToCommit] to [db].
// Assumes [trieToCommit]'s node IDs have been calculated.
func (db *merkleDB) commitChanges(ctx context.Context, trieToCommit *trieView) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	switch {
	case db.closed:
		return database.ErrClosed
	case trieToCommit == nil:
		return nil
	case trieToCommit.isInvalid():
		return ErrInvalid
	case trieToCommit.committed:
		return ErrCommitted
	case trieToCommit.db != trieToCommit.getParentTrie():
		return ErrParentNotDatabase
	}

	changes := trieToCommit.changes
	_, span := db.tracer.Start(ctx, "MerkleDB.commitChanges", oteltrace.WithAttributes(
		attribute.Int("nodesChanged", len(changes.nodes)),
		attribute.Int("valuesChanged", len(changes.values)),
	))
	defer span.End()

	// invalidate all child views except for the view being committed
	db.invalidateChildrenExcept(trieToCommit)

	// move any child views of the committed trie onto the db
	db.moveChildViewsToDB(trieToCommit)

	if len(changes.nodes) == 0 {
		return nil
	}

	rootChange, ok := changes.nodes[RootPath]
	if !ok {
		return errNoNewRoot
	}

	currentValueNodeBatch := db.valueNodesDB.NewBatch()

	_, nodesSpan := db.tracer.Start(ctx, "MerkleDB.commitChanges.writeNodes")
	for key, nodeChange := range changes.nodes {
		if nodeChange.after == nil {
			switch {
			case nodeChange.before == nil:
				// before and after are both nil, this is a noop
			case nodeChange.before.hasValue():
				// the node had a value before, delete from the value node db
				currentValueNodeBatch.Delete(key)
			default:
				// the node didn't have a value before, delete from the intermediate node db
				if err := db.intermediateNodesDB.Delete(key); err != nil {
					nodesSpan.End()
					return err
				}
			}
			continue
		}

		// the node is not nil and has a value, add to the value node db
		if nodeChange.after.hasValue() {
			currentValueNodeBatch.Put(key, nodeChange.after)

			// the node didn't have a value before, delete from the intermediate node db
			if nodeChange.before != nil && !nodeChange.before.hasValue() {
				if err := db.intermediateNodesDB.Delete(key); err != nil {
					nodesSpan.End()
					return err
				}
			}
			continue
		}

		// the node is not nil and has no value, add to the intermediate node db
		if err := db.intermediateNodesDB.Put(key, nodeChange.after); err != nil {
			nodesSpan.End()
			return err
		}

		// the node had a value before, delete from the value node db
		if nodeChange.before != nil && nodeChange.before.hasValue() {
			currentValueNodeBatch.Delete(key)
		}
	}
	nodesSpan.End()

	_, commitSpan := db.tracer.Start(ctx, "MerkleDB.commitChanges.dbCommit")
	err := currentValueNodeBatch.Write()
	commitSpan.End()
	if err != nil {
		return err
	}

	// Only modify in-memory state after the commit succeeds
	// so that we don't need to clean up on error.
	db.root = rootChange.after
	db.history.record(changes)
	return nil
}

// moveChildViewsToDB removes any child views from the trieToCommit and moves them to the db
// assumes [db.lock] is held
func (db *merkleDB) moveChildViewsToDB(trieToCommit *trieView) {
	trieToCommit.validityTrackingLock.Lock()
	defer trieToCommit.validityTrackingLock.Unlock()

	for _, childView := range trieToCommit.childViews {
		childView.updateParent(db)
		db.childViews = append(db.childViews, childView)
	}
	trieToCommit.childViews = make([]*trieView, 0, defaultPreallocationSize)
}

// CommitToDB is a no-op for db since it is already in sync with itself.
// This exists to satisfy the TrieView interface.
func (*merkleDB) CommitToDB(context.Context) error {
	return nil
}

// Assumes [db.lock] isn't held.
func (db *merkleDB) VerifyChangeProof(
	ctx context.Context,
	proof *ChangeProof,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	expectedEndRootID ids.ID,
) error {
	switch {
	case start.HasValue() && end.HasValue() && bytes.Compare(start.Value(), end.Value()) > 0:
		return ErrStartAfterEnd
	case proof.Empty():
		return ErrNoMerkleProof
	case end.HasValue() && len(proof.KeyChanges) == 0 && len(proof.EndProof) == 0:
		// We requested an end proof but didn't get one.
		return ErrNoEndProof
	case start.HasValue() && len(proof.StartProof) == 0 && len(proof.EndProof) == 0:
		// We requested a start proof but didn't get one.
		// Note that we also have to check that [proof.EndProof] is empty
		// to handle the case that the start proof is empty because all
		// its nodes are also in the end proof, and those nodes are omitted.
		return ErrNoStartProof
	}

	// Make sure the key-value pairs are sorted and in [start, end].
	if err := verifyKeyChanges(proof.KeyChanges, start, end); err != nil {
		return err
	}

	// Note that if [start] is Nothing, smallestPath is the empty path.
	smallestPath := newPath(start.Value())

	// Make sure the start proof, if given, is well-formed.
	if err := verifyProofPath(proof.StartProof, smallestPath); err != nil {
		return err
	}

	// Find the greatest key in [proof.KeyChanges]
	// Note that [proof.EndProof] is a proof for this key.
	// [largestPath] is also used when we add children of proof nodes to [trie] below.
	largestPath := maybe.Bind(end, newPath)
	if len(proof.KeyChanges) > 0 {
		// If [proof] has key-value pairs, we should insert children
		// greater than [end] to ancestors of the node containing [end]
		// so that we get the expected root ID.
		largestPath = maybe.Some(newPath(proof.KeyChanges[len(proof.KeyChanges)-1].Key))
	}

	// Make sure the end proof, if given, is well-formed.
	if err := verifyProofPath(proof.EndProof, largestPath.Value()); err != nil {
		return err
	}

	keyValues := make(map[path]maybe.Maybe[[]byte], len(proof.KeyChanges))
	for _, keyValue := range proof.KeyChanges {
		keyValues[newPath(keyValue.Key)] = keyValue.Value
	}

	// want to prevent commit writes to DB, but not prevent DB reads
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	if err := verifyAllChangeProofKeyValuesPresent(
		ctx,
		db,
		proof.StartProof,
		smallestPath,
		largestPath,
		keyValues,
	); err != nil {
		return err
	}

	if err := verifyAllChangeProofKeyValuesPresent(
		ctx,
		db,
		proof.EndProof,
		smallestPath,
		largestPath,
		keyValues,
	); err != nil {
		return err
	}

	// Insert the key-value pairs into the trie.
	ops := make([]database.BatchOp, len(proof.KeyChanges))
	for i, kv := range proof.KeyChanges {
		ops[i] = database.BatchOp{
			Key:    kv.Key,
			Value:  kv.Value.Value(),
			Delete: kv.Value.IsNothing(),
		}
	}

	// Don't need to lock [view] because nobody else has a reference to it.
	view, err := db.newUntrackedView(ops)
	if err != nil {
		return err
	}

	// For all the nodes along the edges of the proof, insert the children whose
	// keys are less than [insertChildrenLessThan] or whose keys are greater
	// than [insertChildrenGreaterThan] into the trie so that we get the
	// expected root ID (if this proof is valid).
	insertChildrenLessThan := maybe.Nothing[path]()
	if len(smallestPath) > 0 {
		insertChildrenLessThan = maybe.Some(smallestPath)
	}
	if err := addPathInfo(
		view,
		proof.StartProof,
		insertChildrenLessThan,
		largestPath,
	); err != nil {
		return err
	}
	if err := addPathInfo(
		view,
		proof.EndProof,
		insertChildrenLessThan,
		largestPath,
	); err != nil {
		return err
	}

	// Make sure we get the expected root.
	calculatedRoot, err := view.GetMerkleRoot(ctx)
	if err != nil {
		return err
	}
	if expectedEndRootID != calculatedRoot {
		return fmt.Errorf("%w:[%s], expected:[%s]", ErrInvalidProof, calculatedRoot, expectedEndRootID)
	}

	return nil
}

// Invalidates and removes any child views that aren't [exception].
// Assumes [db.lock] is held.
func (db *merkleDB) invalidateChildrenExcept(exception *trieView) {
	isTrackedView := false

	for _, childView := range db.childViews {
		if childView != exception {
			childView.invalidate()
		} else {
			isTrackedView = true
		}
	}
	db.childViews = make([]*trieView, 0, defaultPreallocationSize)
	if isTrackedView {
		db.childViews = append(db.childViews, exception)
	}
}

func (db *merkleDB) initializeRootIfNeeded() (ids.ID, error) {
	// not sure if the root exists or had a value or not
	// check under both prefixes
	var err error
	db.root, err = db.intermediateNodesDB.Get(RootPath)
	if err == database.ErrNotFound {
		db.root, err = db.valueNodesDB.Get(RootPath)
	}
	if err == nil {
		// Root already exists, so calculate its id
		if err := db.root.calculateID(db.metrics); err != nil {
			return ids.Empty, err
		}
		return db.root.id, nil
	}
	if err != database.ErrNotFound {
		return ids.Empty, err
	}

	// Root doesn't exist; make a new one.
	db.root = newNode(nil, RootPath)

	// update its ID
	if err := db.root.calculateID(db.metrics); err != nil {
		return ids.Empty, err
	}

	if err := db.intermediateNodesDB.Put(RootPath, db.root); err != nil {
		return ids.Empty, err
	}

	return db.root.id, nil
}

// Returns a view of the trie as it was when it had root [rootID] for keys within range [start, end].
// If [start] is Nothing, there's no lower bound on the range.
// If [end] is Nothing, there's no upper bound on the range.
// Assumes [db.commitLock] is read locked.
func (db *merkleDB) getHistoricalViewForRange(
	rootID ids.ID,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
) (*trieView, error) {
	currentRootID := db.getMerkleRoot()

	// looking for the trie's current root id, so return the trie unmodified
	if currentRootID == rootID {
		return newTrieView(db, db, db.root.clone(), nil)
	}

	changeHistory, err := db.history.getChangesToGetToRoot(rootID, start, end)
	if err != nil {
		return nil, err
	}
	return newHistoricalTrieView(db, changeHistory)
}

// Returns all keys in range [start, end] that aren't in [keySet].
// If [start] is nil, then the range has no lower bound.
// If [end] is nil, then the range has no upper bound.
func (db *merkleDB) getKeysNotInSet(start, end []byte, keySet set.Set[string]) ([][]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	it := db.NewIteratorWithStart(start)
	defer it.Release()

	keysNotInSet := make([][]byte, 0, keySet.Len())
	for it.Next() {
		key := it.Key()
		if len(end) != 0 && bytes.Compare(key, end) > 0 {
			break
		}
		if !keySet.Contains(string(key)) {
			keysNotInSet = append(keysNotInSet, key)
		}
	}
	return keysNotInSet, it.Error()
}

// Returns a copy of the node with the given [key].
// This copy may be edited by the caller without affecting the database state.
// Returns database.ErrNotFound if the node doesn't exist.
// Assumes [db.lock] isn't held.
func (db *merkleDB) getEditableNode(key path, hasValue bool) (*node, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	n, err := db.getNode(key, hasValue)
	if err != nil {
		return nil, err
	}
	return n.clone(), nil
}

// Returns the node with the given [key].
// Editing the returned node affects the database state.
// Returns database.ErrNotFound if the node doesn't exist.
// Assumes [db.lock] is read locked.
func (db *merkleDB) getNode(key path, hasValue bool) (*node, error) {
	if db.closed {
		return nil, database.ErrClosed
	}
	if key == RootPath {
		return db.root, nil
	}
	if hasValue {
		return db.valueNodesDB.Get(key)
	}
	return db.intermediateNodesDB.Get(key)
}
