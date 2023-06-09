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

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	RootPath = EmptyPath
	// TODO: name better
	rebuildViewSizeFractionOfCacheSize = 50
	minRebuildViewSizePerCommit        = 1000
)

var (
	_ TrieView = (*merkleDB)(nil)
	_ MerkleDB = (*merkleDB)(nil)

	codec, version = newCodec()

	rootKey                 []byte
	nodePrefix              = []byte("node")
	metadataPrefix          = []byte("metadata")
	cleanShutdownKey        = []byte("cleanShutdown")
	hadCleanShutdown        = []byte{1}
	didNotHaveCleanShutdown = []byte{0}

	errSameRoot = errors.New("start and end root are the same")
)

type ChangeProofer interface {
	// GetChangeProof returns a proof for a subset of the key/value changes in key range
	// [start, end] that occurred between [startRootID] and [endRootID].
	// Returns at most [maxLength] key/value pairs.
	GetChangeProof(
		ctx context.Context,
		startRootID ids.ID,
		endRootID ids.ID,
		start []byte,
		end []byte,
		maxLength int,
	) (*ChangeProof, error)

	// Returns nil iff all of the following hold:
	//   - [start] <= [end].
	//   - [proof] is non-empty iff [proof.HadRootsInHistory].
	//   - All keys in [proof.KeyValues] and [proof.DeletedKeys] are in [start, end].
	//     If [start] is empty, all keys are considered > [start].
	//     If [end] is empty, all keys are considered < [end].
	//   - [proof.KeyValues] and [proof.DeletedKeys] are sorted in order of increasing key.
	//   - [proof.StartProof] and [proof.EndProof] are well-formed.
	//   - When the keys in [proof.KeyValues] are added to [db] and the keys in [proof.DeletedKeys]
	//     are removed from [db], the root ID of [db] is [expectedEndRootID].
	//
	// Assumes [db.lock] isn't held.
	// This is defined on Database instead of ChangeProof because it accesses
	// database internals.
	VerifyChangeProof(
		ctx context.Context,
		proof *ChangeProof,
		start []byte,
		end []byte,
		expectedEndRootID ids.ID,
	) error

	// CommitChangeProof commits the key/value pairs within the [proof] to the db.
	CommitChangeProof(ctx context.Context, proof *ChangeProof) error
}

type RangeProofer interface {
	// GetRangeProofAtRoot returns a proof for the key/value pairs in this trie within the range
	// [start, end] when the root of the trie was [rootID].
	GetRangeProofAtRoot(
		ctx context.Context,
		rootID ids.ID,
		start,
		end []byte,
		maxLength int,
	) (*RangeProof, error)

	// CommitRangeProof commits the key/value pairs within the [proof] to the db.
	// [start] is the smallest key in the range this [proof] covers.
	CommitRangeProof(ctx context.Context, start []byte, proof *RangeProof) error
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
	HistoryLength int
	NodeCacheSize int
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

	nodeDB database.Database

	// Stores data about the database's current state.
	metadataDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	nodeCache         onEvictCache[path, *node]
	onEvictionErr     utils.Atomic[error]
	evictionBatchSize int

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

func newDatabase(
	ctx context.Context,
	db database.Database,
	config Config,
	metrics merkleMetrics,
) (*merkleDB, error) {
	trieDB := &merkleDB{
		metrics:           metrics,
		nodeDB:            prefixdb.New(nodePrefix, db),
		metadataDB:        prefixdb.New(metadataPrefix, db),
		history:           newTrieHistory(config.HistoryLength),
		tracer:            config.Tracer,
		childViews:        make([]*trieView, 0, defaultPreallocationSize),
		evictionBatchSize: config.EvictionBatchSize,
	}

	// Note: trieDB.OnEviction is responsible for writing intermediary nodes to
	// disk as they are evicted from the cache.
	trieDB.nodeCache = newOnEvictCache[path](config.NodeCacheSize, trieDB.onEviction)

	root, err := trieDB.initializeRootIfNeeded()
	if err != nil {
		return nil, err
	}

	// add current root to history (has no changes)
	trieDB.history.record(&changeSummary{
		rootID: root,
		values: map[path]*change[Maybe[[]byte]]{},
		nodes:  map[path]*change[*node]{},
	})

	shutdownType, err := trieDB.metadataDB.Get(cleanShutdownKey)
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
	err = trieDB.metadataDB.Put(cleanShutdownKey, didNotHaveCleanShutdown)
	return trieDB, err
}

// Deletes every intermediate node and rebuilds them by re-adding every key/value.
// TODO: make this more efficient by only clearing out the stale portions of the trie.
func (db *merkleDB) rebuild(ctx context.Context) error {
	db.root = newNode(nil, RootPath)
	if err := db.nodeDB.Delete(rootKey); err != nil {
		return err
	}
	it := db.nodeDB.NewIterator()
	defer it.Release()

	currentViewSize := 0
	viewSizeLimit := math.Max(
		db.nodeCache.maxSize/rebuildViewSizeFractionOfCacheSize,
		minRebuildViewSizePerCommit,
	)

	currentView, err := db.newUntrackedView(viewSizeLimit)
	if err != nil {
		return err
	}

	for it.Next() {
		if currentViewSize >= viewSizeLimit {
			if err := currentView.commitToDB(ctx); err != nil {
				return err
			}
			currentView, err = db.newUntrackedView(viewSizeLimit)
			if err != nil {
				return err
			}
			currentViewSize = 0
		}

		key := it.Key()
		path := path(key)
		value := it.Value()
		n, err := parseNode(path, value)
		if err != nil {
			return err
		}
		if n.hasValue() {
			serializedPath := path.Serialize()
			if err := currentView.Insert(ctx, serializedPath.Value, n.value.value); err != nil {
				return err
			}
			currentViewSize++
		} else if err := db.nodeDB.Delete(key); err != nil {
			return err
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	if err := currentView.commitToDB(ctx); err != nil {
		return err
	}
	return db.nodeDB.Compact(nil, nil)
}

// New returns a new merkle database.
func New(ctx context.Context, db database.Database, config Config) (*merkleDB, error) {
	metrics, err := newMetrics("merkleDB", config.Reg)
	if err != nil {
		return nil, err
	}
	return newDatabase(ctx, db, config, metrics)
}

func (db *merkleDB) CommitChangeProof(ctx context.Context, proof *ChangeProof) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	view, err := db.prepareChangeProofView(proof)
	if err != nil {
		return err
	}
	return view.commitToDB(ctx)
}

func (db *merkleDB) CommitRangeProof(ctx context.Context, start []byte, proof *RangeProof) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	view, err := db.prepareRangeProofView(start, proof)
	if err != nil {
		return err
	}
	return view.commitToDB(ctx)
}

func (db *merkleDB) Compact(start []byte, limit []byte) error {
	return db.nodeDB.Compact(start, limit)
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

	defer func() {
		_ = db.metadataDB.Close()
		_ = db.nodeDB.Close()
	}()

	if err := db.onEvictionErr.Get(); err != nil {
		// If there was an error during cache eviction,
		// [db.nodeCache] and [db.nodeDB] are in an inconsistent state.
		// Do not write cached nodes to disk or mark clean shutdown.
		return nil
	}

	// Flush [nodeCache] to persist intermediary nodes to disk.
	if err := db.nodeCache.Flush(); err != nil {
		// There was an error during cache eviction.
		// Don't commit to disk.
		return err
	}

	// Successfully wrote intermediate nodes.
	return db.metadataDB.Put(cleanShutdownKey, hadCleanShutdown)
}

func (db *merkleDB) Delete(key []byte) error {
	// this is a duplicate because the database interface doesn't support
	// contexts, which are used for tracing
	return db.Remove(context.Background(), key)
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
		values[i], errors[i] = db.getValueCopy(newPath(key), false /*lock*/)
	}
	return values, errors
}

// GetValue returns the value associated with [key].
// Returns database.ErrNotFound if it doesn't exist.
func (db *merkleDB) GetValue(ctx context.Context, key []byte) ([]byte, error) {
	_, span := db.tracer.Start(ctx, "MerkleDB.GetValue")
	defer span.End()

	return db.getValueCopy(newPath(key), true /*lock*/)
}

// getValueCopy returns a copy of the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
func (db *merkleDB) getValueCopy(key path, lock bool) ([]byte, error) {
	val, err := db.getValue(key, lock)
	if err != nil {
		return nil, err
	}
	return slices.Clone(val), nil
}

// getValue returns the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
// If [lock], [db.lock]'s read lock is acquired.
// Otherwise, assumes [db.lock] is already held.
func (db *merkleDB) getValue(key path, lock bool) ([]byte, error) {
	if lock {
		db.lock.RLock()
		defer db.lock.RUnlock()
	}

	if db.closed {
		return nil, database.ErrClosed
	}

	n, err := db.getNode(key)
	if err != nil {
		return nil, err
	}
	if n.value.IsNothing() {
		return nil, database.ErrNotFound
	}
	return n.value.value, nil
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
func (db *merkleDB) getProof(ctx context.Context, key []byte) (*Proof, error) {
	if db.closed {
		return nil, database.ErrClosed
	}

	view, err := db.newUntrackedView(defaultPreallocationSize)
	if err != nil {
		return nil, err
	}
	// Don't need to lock [view] because nobody else has a reference to it.
	return view.getProof(ctx, key)
}

// GetRangeProof returns a proof for the key/value pairs in this trie within the range
// [start, end].
func (db *merkleDB) GetRangeProof(
	ctx context.Context,
	start,
	end []byte,
	maxLength int,
) (*RangeProof, error) {
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	return db.getRangeProofAtRoot(ctx, db.getMerkleRoot(), start, end, maxLength)
}

// GetRangeProofAtRoot returns a proof for the key/value pairs in this trie within the range
// [start, end] when the root of the trie was [rootID].
func (db *merkleDB) GetRangeProofAtRoot(
	ctx context.Context,
	rootID ids.ID,
	start,
	end []byte,
	maxLength int,
) (*RangeProof, error) {
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	return db.getRangeProofAtRoot(ctx, rootID, start, end, maxLength)
}

// Assumes [db.commitLock] is read locked.
func (db *merkleDB) getRangeProofAtRoot(
	ctx context.Context,
	rootID ids.ID,
	start,
	end []byte,
	maxLength int,
) (*RangeProof, error) {
	if db.closed {
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
	start []byte,
	end []byte,
	maxLength int,
) (*ChangeProof, error) {
	if len(end) > 0 && bytes.Compare(start, end) == 1 {
		return nil, ErrStartAfterEnd
	}
	if startRootID == endRootID {
		return nil, errSameRoot
	}

	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}

	result := &ChangeProof{
		HadRootsInHistory: true,
	}
	changes, err := db.history.getValueChanges(startRootID, endRootID, start, end, maxLength)
	if err == ErrRootIDNotPresent {
		result.HadRootsInHistory = false
		return result, nil
	}
	if err != nil {
		return nil, err
	}

	// [changedKeys] are a subset of the keys that were added or had their
	// values modified between [startRootID] to [endRootID] sorted in increasing
	// order.
	changedKeys := maps.Keys(changes.values)
	slices.SortFunc(changedKeys, func(i, j path) bool {
		return i.Compare(j) < 0
	})

	// TODO: sync.pool these buffers
	result.KeyChanges = make([]KeyChange, 0, len(changedKeys))

	for _, key := range changedKeys {
		change := changes.values[key]
		serializedKey := key.Serialize().Value

		result.KeyChanges = append(result.KeyChanges, KeyChange{
			Key: serializedKey,
			// create a copy so edits of the []byte don't affect the db
			Value: Clone(change.after),
		})
	}

	largestKey := end
	if len(result.KeyChanges) > 0 {
		largestKey = result.KeyChanges[len(result.KeyChanges)-1].Key
	}

	// Since we hold [db.commitlock] we must still have sufficient
	// history to recreate the trie at [endRootID].
	historicalView, err := db.getHistoricalViewForRange(endRootID, start, largestKey)
	if err != nil {
		return nil, err
	}

	if len(largestKey) > 0 {
		endProof, err := historicalView.getProof(ctx, largestKey)
		if err != nil {
			return nil, err
		}
		result.EndProof = endProof.Path
	}

	if len(start) > 0 {
		startProof, err := historicalView.getProof(ctx, start)
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
// Assumes [db.lock] isn't held.
func (db *merkleDB) NewView() (TrieView, error) {
	return db.NewPreallocatedView(defaultPreallocationSize)
}

// Returns a new view that isn't tracked in [db.childViews].
// For internal use only, namely in methods that create short-lived views.
// Assumes [db.lock] and/or [db.commitLock] is read locked.
func (db *merkleDB) newUntrackedView(estimatedSize int) (*trieView, error) {
	return newTrieView(db, db, db.root.clone(), estimatedSize)
}

// NewPreallocatedView returns a new view with memory allocated to hold at least [estimatedSize] value changes at a time.
// If more changes are made, additional memory will be allocated.
// The returned view is added to [db.childViews].
// Assumes [db.lock] isn't held.
func (db *merkleDB) NewPreallocatedView(estimatedSize int) (TrieView, error) {
	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return nil, database.ErrClosed
	}

	newView, err := newTrieView(db, db, db.root.clone(), estimatedSize)
	if err != nil {
		return nil, err
	}
	db.childViews = append(db.childViews, newView)
	return newView, nil
}

func (db *merkleDB) Has(k []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return false, database.ErrClosed
	}

	_, err := db.getValue(newPath(k), false /*lock*/)
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
	return db.nodeDB.HealthCheck(ctx)
}

func (db *merkleDB) Insert(ctx context.Context, k, v []byte) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	view, err := db.newUntrackedView(defaultPreallocationSize)
	if err != nil {
		return err
	}
	// Don't need to lock [view] because nobody else has a reference to it.
	if err := view.insert(k, v); err != nil {
		return err
	}
	return view.commitToDB(ctx)
}

func (db *merkleDB) NewBatch() database.Batch {
	return &batch{
		db: db,
	}
}

func (db *merkleDB) NewIterator() database.Iterator {
	return &iterator{
		nodeIter: db.nodeDB.NewIterator(),
		db:       db,
	}
}

func (db *merkleDB) NewIteratorWithStart(start []byte) database.Iterator {
	return &iterator{
		nodeIter: db.nodeDB.NewIteratorWithStart(newPath(start).Bytes()),
		db:       db,
	}
}

func (db *merkleDB) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return &iterator{
		nodeIter: db.nodeDB.NewIteratorWithPrefix(newPath(prefix).Bytes()),
		db:       db,
	}
}

func (db *merkleDB) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	startBytes := newPath(start).Bytes()
	prefixBytes := newPath(prefix).Bytes()
	return &iterator{
		nodeIter: db.nodeDB.NewIteratorWithStartAndPrefix(startBytes, prefixBytes),
		db:       db,
	}
}

// If [node] is an intermediary node, puts it in [nodeDB].
// Note this is called by [db.nodeCache] with its lock held, so
// the movement of [node] from [db.nodeCache] to [db.nodeDB] is atomic.
// As soon as [db.nodeCache] no longer has [node], [db.nodeDB] does.
// Non-nil error is fatal -- causes [db] to close.
func (db *merkleDB) onEviction(n *node) error {
	// the evicted node isn't an intermediary node, so skip writing.
	if n == nil || n.hasValue() {
		return nil
	}

	batch := db.nodeDB.NewBatch()
	if err := writeNodeToBatch(batch, n); err != nil {
		return err
	}

	// Evict the oldest [evictionBatchSize] nodes from the cache
	// and write them to disk. We write a batch of them, rather than
	// just [n], so that we don't immediately evict and write another
	// node, because each time this method is called we do a disk write.
	var err error
	for removedCount := 0; removedCount < db.evictionBatchSize; removedCount++ {
		_, n, exists := db.nodeCache.removeOldest()
		if !exists {
			// The cache is empty.
			break
		}
		if n == nil || n.hasValue() {
			// only persist intermediary nodes
			continue
		}
		// Note this must be = not := since we check
		// [err] outside the loop.
		if err = writeNodeToBatch(batch, n); err != nil {
			break
		}
	}
	if err == nil {
		err = batch.Write()
	}
	if err != nil {
		db.onEvictionErr.Set(err)
		_ = db.nodeDB.Close()
		go db.Close()
		return err
	}
	return nil
}

// Writes [n] to [batch]. Assumes [n] is non-nil.
func writeNodeToBatch(batch database.Batch, n *node) error {
	nodeBytes, err := n.marshal()
	if err != nil {
		return err
	}

	return batch.Put(n.key.Bytes(), nodeBytes)
}

// Put upserts the key/value pair into the db.
func (db *merkleDB) Put(k, v []byte) error {
	return db.Insert(context.Background(), k, v)
}

func (db *merkleDB) Remove(ctx context.Context, key []byte) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	view, err := db.newUntrackedView(defaultPreallocationSize)
	if err != nil {
		return err
	}
	// Don't need to lock [view] because nobody else has a reference to it.
	if err = view.remove(key); err != nil {
		return err
	}
	return view.commitToDB(ctx)
}

func (db *merkleDB) commitBatch(ops []database.BatchOp) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	view, err := db.prepareBatchView(ops)
	if err != nil {
		return err
	}
	return view.commitToDB(context.Background())
}

// CommitToParent is a no-op for the db because it has no parent
func (*merkleDB) CommitToParent(context.Context) error {
	return nil
}

// commitToDB is a no-op for the db because it is the db
func (*merkleDB) commitToDB(context.Context) error {
	return nil
}

// commitChanges commits the changes in trieToCommit to the db
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

	batch := db.nodeDB.NewBatch()

	_, nodesSpan := db.tracer.Start(ctx, "MerkleDB.commitChanges.writeNodes")
	for key, nodeChange := range changes.nodes {
		if nodeChange.after == nil {
			db.metrics.IOKeyWrite()
			if err := batch.Delete(key.Bytes()); err != nil {
				nodesSpan.End()
				return err
			}
		} else if nodeChange.after.hasValue() || (nodeChange.before != nil && nodeChange.before.hasValue()) {
			// Note: If [nodeChange.after] is an intermediary node we only
			// persist [nodeChange] if [nodeChange.before] was a leaf.
			// This guarantees that the key/value pairs are correctly persisted
			// on disk, without being polluted by the previous value.
			// Otherwise, intermediary nodes are persisted on cache eviction or
			// shutdown.
			db.metrics.IOKeyWrite()
			if err := writeNodeToBatch(batch, nodeChange.after); err != nil {
				nodesSpan.End()
				return err
			}
		}
	}
	nodesSpan.End()

	_, commitSpan := db.tracer.Start(ctx, "MerkleDB.commitChanges.dbCommit")
	err := batch.Write()
	commitSpan.End()
	if err != nil {
		return err
	}

	// Only modify in-memory state after the commit succeeds
	// so that we don't need to clean up on error.
	db.root = rootChange.after

	for key, nodeChange := range changes.nodes {
		if err := db.putNodeInCache(key, nodeChange.after); err != nil {
			return err
		}
	}

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

func (db *merkleDB) VerifyChangeProof(
	ctx context.Context,
	proof *ChangeProof,
	start []byte,
	end []byte,
	expectedEndRootID ids.ID,
) error {
	if len(end) > 0 && bytes.Compare(start, end) > 0 {
		return ErrStartAfterEnd
	}

	if !proof.HadRootsInHistory {
		// The node we requested the proof from didn't have sufficient
		// history to fulfill this request.
		if !proof.Empty() {
			// cannot have any changes if the root was missing
			return ErrDataInMissingRootProof
		}
		return nil
	}

	switch {
	case proof.Empty():
		return ErrNoMerkleProof
	case len(end) > 0 && len(proof.EndProof) == 0:
		// We requested an end proof but didn't get one.
		return ErrNoEndProof
	case len(start) > 0 && len(proof.StartProof) == 0 && len(proof.EndProof) == 0:
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

	smallestPath := newPath(start)

	// Make sure the start proof, if given, is well-formed.
	if err := verifyProofPath(proof.StartProof, smallestPath); err != nil {
		return err
	}

	// Find the greatest key in [proof.KeyChanges]
	// Note that [proof.EndProof] is a proof for this key.
	// [largestPath] is also used when we add children of proof nodes to [trie] below.
	largestKey := end
	if len(proof.KeyChanges) > 0 {
		// If [proof] has key-value pairs, we should insert children
		// greater than [end] to ancestors of the node containing [end]
		// so that we get the expected root ID.
		largestKey = proof.KeyChanges[len(proof.KeyChanges)-1].Key
	}
	largestPath := newPath(largestKey)

	// Make sure the end proof, if given, is well-formed.
	if err := verifyProofPath(proof.EndProof, largestPath); err != nil {
		return err
	}

	keyValues := make(map[path]Maybe[[]byte], len(proof.KeyChanges))
	for _, keyValue := range proof.KeyChanges {
		keyValues[newPath(keyValue.Key)] = keyValue.Value
	}

	// want to prevent commit writes to DB, but not prevent db reads
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

	// Don't need to lock [view] because nobody else has a reference to it.
	view, err := db.newUntrackedView(len(proof.KeyChanges))
	if err != nil {
		return err
	}

	// Insert the key-value pairs into the trie.
	for _, kv := range proof.KeyChanges {
		if kv.Value.IsNothing() {
			if err := view.removeFromTrie(newPath(kv.Key)); err != nil {
				return err
			}
		} else if _, err := view.insertIntoTrie(newPath(kv.Key), kv.Value); err != nil {
			return err
		}
	}

	// For all the nodes along the edges of the proof, insert children < [start] and > [largestKey]
	// into the trie so that we get the expected root ID (if this proof is valid).
	if err := addPathInfo(view, proof.StartProof, smallestPath, largestPath); err != nil {
		return err
	}
	if err := addPathInfo(view, proof.EndProof, smallestPath, largestPath); err != nil {
		return err
	}

	// Make sure we get the expected root.
	calculatedRoot, err := view.getMerkleRoot(ctx)
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
	// ensure that root exists
	nodeBytes, err := db.nodeDB.Get(rootKey)
	if err == nil {
		// Root already exists, so parse it and set the in-mem copy
		db.root, err = parseNode(RootPath, nodeBytes)
		if err != nil {
			return ids.Empty, err
		}
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

	// write the newly constructed root to the DB
	rootBytes, err := db.root.marshal()
	if err != nil {
		return ids.Empty, err
	}

	batch := db.nodeDB.NewBatch()
	if err := batch.Put(rootKey, rootBytes); err != nil {
		return ids.Empty, err
	}

	return db.root.id, batch.Write()
}

// Returns a view of the trie as it was when it had root [rootID] for keys within range [start, end].
// Assumes [db.commitLock] is read locked.
func (db *merkleDB) getHistoricalViewForRange(
	rootID ids.ID,
	start []byte,
	end []byte,
) (*trieView, error) {
	currentRootID := db.getMerkleRoot()

	// looking for the trie's current root id, so return the trie unmodified
	if currentRootID == rootID {
		return newTrieView(db, db, db.root.clone(), 100)
	}

	changeHistory, err := db.history.getChangesToGetToRoot(rootID, start, end)
	if err != nil {
		return nil, err
	}
	return newTrieViewWithChanges(db, db, changeHistory, len(changeHistory.nodes))
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
func (db *merkleDB) getEditableNode(key path) (*node, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	n, err := db.getNode(key)
	if err != nil {
		return nil, err
	}
	return n.clone(), nil
}

// Returns the node with the given [key].
// Editing the returned node affects the database state.
// Returns database.ErrNotFound if the node doesn't exist.
// Assumes [db.lock] is read locked.
func (db *merkleDB) getNode(key path) (*node, error) {
	if db.closed {
		return nil, database.ErrClosed
	}
	if key == RootPath {
		return db.root, nil
	}

	if n, isCached := db.getNodeInCache(key); isCached {
		db.metrics.DBNodeCacheHit()
		if n == nil {
			return nil, database.ErrNotFound
		}
		return n, nil
	}

	db.metrics.DBNodeCacheMiss()
	db.metrics.IOKeyRead()
	rawBytes, err := db.nodeDB.Get(key.Bytes())
	if err != nil {
		if err == database.ErrNotFound {
			// Cache the miss.
			if err := db.putNodeInCache(key, nil); err != nil {
				return nil, err
			}
		}
		return nil, err
	}

	node, err := parseNode(key, rawBytes)
	if err != nil {
		return nil, err
	}

	err = db.putNodeInCache(key, node)
	return node, err
}

// Returns a new view atop [db] with the changes in [ops] applied to it.
// Assumes [db.commitLock] is read locked.
func (db *merkleDB) prepareBatchView(ops []database.BatchOp) (*trieView, error) {
	view, err := db.newUntrackedView(len(ops))
	if err != nil {
		return nil, err
	}
	// Don't need to lock [view] because nobody else has a reference to it.

	// write into the trie
	for _, op := range ops {
		if op.Delete {
			if err := view.remove(op.Key); err != nil {
				return nil, err
			}
		} else if err := view.insert(op.Key, op.Value); err != nil {
			return nil, err
		}
	}

	return view, nil
}

// Returns a new view atop [db] with the key/value pairs in [proof.KeyValues]
// inserted and the key/value pairs in [proof.DeletedKeys] removed.
// Assumes [db.commitLock] is locked.
func (db *merkleDB) prepareChangeProofView(proof *ChangeProof) (*trieView, error) {
	view, err := db.newUntrackedView(len(proof.KeyChanges))
	if err != nil {
		return nil, err
	}
	// Don't need to lock [view] because nobody else has a reference to it.

	for _, kv := range proof.KeyChanges {
		if kv.Value.IsNothing() {
			if err := view.remove(kv.Key); err != nil {
				return nil, err
			}
		} else if err := view.insert(kv.Key, kv.Value.value); err != nil {
			return nil, err
		}
	}
	return view, nil
}

// Returns a new view atop [db] with the key/value pairs in [proof.KeyValues] added and
// any existing key-value pairs in the proof's range but not in the proof removed.
// Assumes [db.commitLock] is locked.
func (db *merkleDB) prepareRangeProofView(start []byte, proof *RangeProof) (*trieView, error) {
	// Don't need to lock [view] because nobody else has a reference to it.
	view, err := db.newUntrackedView(len(proof.KeyValues))
	if err != nil {
		return nil, err
	}
	keys := set.NewSet[string](len(proof.KeyValues))
	for _, kv := range proof.KeyValues {
		keys.Add(string(kv.Key))
		if err := view.insert(kv.Key, kv.Value); err != nil {
			return nil, err
		}
	}

	var largestKey []byte
	if len(proof.KeyValues) > 0 {
		largestKey = proof.KeyValues[len(proof.KeyValues)-1].Key
	}
	keysToDelete, err := db.getKeysNotInSet(start, largestKey, keys)
	if err != nil {
		return nil, err
	}
	for _, keyToDelete := range keysToDelete {
		if err := view.remove(keyToDelete); err != nil {
			return nil, err
		}
	}
	return view, nil
}

// Non-nil error is fatal -- [db] will close.
func (db *merkleDB) putNodeInCache(key path, n *node) error {
	// TODO Cache metrics
	// Note that this may cause a node to be evicted from the cache,
	// which will call [OnEviction].
	return db.nodeCache.Put(key, n)
}

func (db *merkleDB) getNodeInCache(key path) (*node, bool) {
	// TODO Cache metrics
	if node, ok := db.nodeCache.Get(key); ok {
		return node, true
	}
	return nil, false
}
