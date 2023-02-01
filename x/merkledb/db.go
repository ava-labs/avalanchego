// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	RootPath = EmptyPath

	// TODO: name better
	rebuildViewSizeFractionOfCacheSize = 50
	minRebuildViewSizePerCommit        = 1000
)

var (
	_ Trie              = &Database{}
	_ database.Database = &Database{}

	Codec, Version = newCodec()

	rootKey                 = []byte{}
	nodePrefix              = []byte("node")
	metadataPrefix          = []byte("metadata")
	cleanShutdownKey        = []byte("cleanShutdown")
	hadCleanShutdown        = []byte{1}
	didNotHaveCleanShutdown = []byte{0}

	errSameRoot = errors.New("start and end root are the same")
)

type Config struct {
	// The number of changes to the database that we store in memory in order to
	// serve change proofs.
	HistoryLength  int
	ValueCacheSize int
	NodeCacheSize  int
	// If [Reg] is nil, metrics are collected locally but not exported through
	// Prometheus.
	// This may be useful for testing.
	Reg    prometheus.Registerer
	Tracer trace.Tracer
}

// Can only be edited by committing changes from a trieView.
type Database struct {
	// Must be held when reading/writing fields.
	lock sync.RWMutex

	// versiondb that the other dbs are built on.
	// Allows the changes made to the snapshot and [nodeDB] to be atomic.
	nodeDB *versiondb.Database

	// Stores data about the database's current state.
	metadataDB database.Database

	// If a value is nil, the corresponding key isn't in the trie.
	nodeCache cache.LRU[path, *node]

	valueCache cache.LRU[string, Maybe[[]byte]]

	// Stores change lists. Used to serve change proofs and construct
	// historical views of the trie.
	history *trieHistory

	// True iff the db has been closed.
	closed bool

	metrics merkleMetrics

	tracer trace.Tracer

	// The root of this trie.
	root *node
}

func newDatabase(
	ctx context.Context,
	db database.Database,
	config Config,
	metrics merkleMetrics,
) (*Database, error) {
	trieDB := &Database{
		metrics:    metrics,
		nodeDB:     versiondb.New(prefixdb.New(nodePrefix, db)),
		metadataDB: prefixdb.New(metadataPrefix, db),
		history:    newTrieHistory(config.HistoryLength),
		tracer:     config.Tracer,
		valueCache: cache.LRU[string, Maybe[[]byte]]{Size: config.ValueCacheSize},
	}

	// Note: trieDB.OnEviction is responsible for writing intermediary nodes to
	// disk as they are evicted from the cache.
	trieDB.nodeCache = cache.LRU[path, *node]{
		Size:       config.NodeCacheSize,
		OnEviction: trieDB.OnEviction,
	}

	root, err := trieDB.initializeRootIfNeeded(ctx)
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

// Only grabs a read lock because views built atop this database can't mutate it.
// The database is only mutated by a view when the view is committed, and
// trieView grabs a write lock when committing.
func (db *Database) lockStack() {
	db.lock.RLock()
}

func (db *Database) unlockStack() {
	db.lock.RUnlock()
}

// Deletes every intermediate node and rebuilds them by re-adding every key/value.
// TODO: make this more efficient by only clearing out the stale portions of the trie.
func (db *Database) rebuild(ctx context.Context) error {
	db.root = newNode(nil, RootPath)
	if err := db.nodeDB.Delete(rootKey); err != nil {
		return err
	}
	it := db.nodeDB.NewIterator()
	defer it.Release()

	currentView, err := db.newView(ctx)
	if err != nil {
		return err
	}
	currentViewSize := 0
	viewSizeLimit := math.Max(
		db.nodeCache.Size/rebuildViewSizeFractionOfCacheSize,
		minRebuildViewSizePerCommit,
	)
	for it.Next() {
		if currentViewSize >= viewSizeLimit {
			if err := currentView.Commit(ctx); err != nil {
				return err
			}
			currentView, err = db.newView(ctx)
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
		}
		if err := db.nodeDB.Delete(key); err != nil {
			return err
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	if err := currentView.Commit(ctx); err != nil {
		return err
	}
	return db.nodeDB.Compact(nil, nil)
}

// New returns a new merkle database.
func New(ctx context.Context, db database.Database, config Config) (*Database, error) {
	metrics, err := newMetrics("merkleDB", config.Reg)
	if err != nil {
		return nil, err
	}
	return newDatabase(ctx, db, config, metrics)
}

// Commits the key/value pairs within the [proof] to the db.
func (db *Database) CommitChangeProof(ctx context.Context, proof *ChangeProof) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	view, err := db.prepareChangeProofView(ctx, proof)
	if err != nil {
		return err
	}
	return view.commit(ctx)
}

// Commits the key/value pairs within the [proof] to the db.
// [start] is the smallest key in the range this [proof] covers.
func (db *Database) CommitRangeProof(ctx context.Context, start []byte, proof *RangeProof) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	view, err := db.prepareRangeProofView(ctx, start, proof)
	if err != nil {
		return err
	}
	return view.commit(ctx)
}

func (db *Database) Compact(start []byte, limit []byte) error {
	return db.nodeDB.Compact(start, limit)
}

func (db *Database) Close() error {
	db.lock.Lock()
	defer func() {
		_ = db.metadataDB.Close() // TODO add logger and log error
		_ = db.nodeDB.Close()     // TODO add logger and log error
		db.lock.Unlock()
	}()

	db.closed = true

	// flush [nodeCache] to persist intermediary nodes to disk.
	db.nodeCache.Flush()
	if err := db.nodeDB.Commit(); err != nil {
		return err
	}

	// after intermediary nodes are persisted, we can mark a clean shutdown.
	return db.metadataDB.Put(cleanShutdownKey, hadCleanShutdown)
}

func (db *Database) Delete(key []byte) error {
	// this is a duplicate because the database interface doesn't support
	// contexts, which are used for tracing
	return db.Remove(context.Background(), key)
}

func (db *Database) Get(key []byte) ([]byte, error) {
	// this is a duplicate because the database interface doesn't support
	// contexts, which are used for tracing
	return db.GetValue(context.Background(), key)
}

func (db *Database) GetValues(ctx context.Context, keys [][]byte) ([][]byte, []error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	values := make([][]byte, len(keys))
	errors := make([]error, len(keys))
	for i, key := range keys {
		path := newPath(key)
		values[i], errors[i] = db.getValue(ctx, path)
	}
	return values, errors
}

// Get the value associated with [key].
// Returns database.ErrNotFound if it doesn't exist.
func (db *Database) GetValue(ctx context.Context, key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getValue(ctx, newPath(key))
}

// Assumes [db.lock] is read locked.
func (db *Database) getValue(ctx context.Context, key path) ([]byte, error) {
	if db.closed {
		return nil, database.ErrClosed
	}
	keyStr := string(key.Serialize().Value)
	if val, ok := db.getValueInCache(keyStr); ok {
		if val.IsNothing() {
			return nil, database.ErrNotFound
		}
		return val.value, nil
	}
	n, err := db.getNode(ctx, key)
	if err == database.ErrNotFound {
		db.putValueInCache(keyStr, Nothing[[]byte]())
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	db.putValueInCache(keyStr, n.value)
	if n.value.IsNothing() {
		return nil, database.ErrNotFound
	}
	return n.value.value, nil
}

// Returns a view of the trie as it was when the merkle root was [rootID].
func (db *Database) GetHistoricalView(ctx context.Context, rootID ids.ID) (ReadOnlyTrie, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getHistoricalViewForRangeProof(ctx, rootID, nil, nil)
}

// Returns the ID of the root node of the merkle trie.
func (db *Database) GetMerkleRoot(_ context.Context) (ids.ID, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getMerkleRoot(), nil
}

// Returns the ID of the root node of the merkle trie.
// Assumes [db.lock] is read locked.
func (db *Database) getMerkleRoot() ids.ID {
	return db.root.id
}

// Returns a proof of the existence/non-existence of [key] in this trie.
func (db *Database) GetProof(ctx context.Context, key []byte) (*Proof, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getProof(ctx, key)
}

// Returns a proof of the existence/non-existence of [key] in this trie.
// Assumes [db.lock] is read locked.
func (db *Database) getProof(ctx context.Context, key []byte) (*Proof, error) {
	view, err := db.newView(ctx)
	if err != nil {
		return nil, err
	}
	// Don't need to lock [view] because nobody else has a reference to it.
	return view.getProof(ctx, key)
}

// Returns a proof for the key/value pairs in this trie within the range
// [start, end].
func (db *Database) GetRangeProof(
	ctx context.Context,
	start,
	end []byte,
	maxLength int,
) (*RangeProof, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getRangeProofAtRoot(ctx, db.getMerkleRoot(), start, end, maxLength)
}

// Returns a proof for the key/value pairs in this trie within the range
// [start, end] when the root of the trie was [rootID].
func (db *Database) GetRangeProofAtRoot(
	ctx context.Context,
	rootID ids.ID,
	start,
	end []byte,
	maxLength int,
) (*RangeProof, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getRangeProofAtRoot(ctx, rootID, start, end, maxLength)
}

// Assumes [db.lock] is read locked.
func (db *Database) getRangeProofAtRoot(
	ctx context.Context,
	rootID ids.ID,
	start,
	end []byte,
	maxLength int,
) (*RangeProof, error) {
	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	historicalView, err := db.getHistoricalViewForRangeProof(ctx, rootID, start, end)
	if err != nil {
		return nil, err
	}
	return historicalView.getRangeProof(ctx, start, end, maxLength)
}

// Returns a proof for a subset of the key/value changes in key range
// [start, end] that occurred between [startRootID] and [endRootID].
// Returns at most [maxLength] key/value pairs.
func (db *Database) GetChangeProof(
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

	db.lock.RLock()
	defer db.lock.RUnlock()

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
	result.KeyValues = make([]KeyValue, 0, len(changedKeys))
	result.DeletedKeys = make([][]byte, 0, len(changedKeys))

	for _, key := range changedKeys {
		change := changes.values[key]
		serializedKey := key.Serialize().Value

		if change.after.IsNothing() {
			result.DeletedKeys = append(result.DeletedKeys, serializedKey)
		} else {
			result.KeyValues = append(result.KeyValues, KeyValue{
				Key:   serializedKey,
				Value: change.after.value,
			})
		}
		end = serializedKey
	}

	// Since we hold [db.lock] we must still have sufficient
	// history to recreate the trie at [endRootID].
	historicalView, err := db.getHistoricalViewForRangeProof(ctx, endRootID, start, end)
	if err != nil {
		return nil, err
	}

	if len(end) > 0 {
		endProof, err := historicalView.getProof(ctx, end)
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

// Returns a new view on top of this trie.
// Changes made to the view will only be reflected in the original trie if Commit is called.
// Assumes [db.lock] isn't held.
func (db *Database) NewView(ctx context.Context) (TrieView, error) {
	return db.NewPreallocatedView(ctx, defaultPreallocationSize)
}

// Assumes [db.lock] is read locked.
func (db *Database) newView(ctx context.Context) (*trieView, error) {
	return db.newPreallocatedView(ctx, defaultPreallocationSize)
}

// Same as NewView except that the view will be preallocated to hold at least [estimatedSize]
// value changes at a time. If more changes are made, additional memory will be allocated.
// Assumes [db.lock] isn't held.
func (db *Database) NewPreallocatedView(ctx context.Context, estimatedSize int) (TrieView, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return newTrieView(ctx, db, nil, nil, estimatedSize)
}

// Assumes [db.lock] is read locked.
func (db *Database) newPreallocatedView(ctx context.Context, estimatedSize int) (*trieView, error) {
	return newTrieView(ctx, db, nil, nil, estimatedSize)
}

func (db *Database) Has(k []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return false, database.ErrClosed
	}

	_, err := db.getValue(context.Background(), newPath(k))
	if err == database.ErrNotFound {
		return false, nil
	}
	return err == nil, err
}

func (db *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	return db.nodeDB.HealthCheck(ctx)
}

func (db *Database) Insert(ctx context.Context, k, v []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	view, err := db.newView(ctx)
	if err != nil {
		return err
	}
	// Don't need to lock [view] because nobody else has a reference to it.
	if err := view.insert(ctx, k, v); err != nil {
		return err
	}
	return view.commit(ctx)
}

func (db *Database) NewBatch() database.Batch {
	return &batch{
		db:   db,
		data: linkedhashmap.New[string, batchOp](),
	}
}

func (db *Database) NewIterator() database.Iterator {
	return &iterator{
		nodeIter: db.nodeDB.NewIterator(),
		db:       db,
	}
}

func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return &iterator{
		nodeIter: db.nodeDB.NewIteratorWithStart(newPath(start).Bytes()),
		db:       db,
	}
}

func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return &iterator{
		nodeIter: db.nodeDB.NewIteratorWithPrefix(newPath(prefix).Bytes()),
		db:       db,
	}
}

func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	startBytes := newPath(start).Bytes()
	prefixBytes := newPath(prefix).Bytes()
	return &iterator{
		nodeIter: db.nodeDB.NewIteratorWithStartAndPrefix(startBytes, prefixBytes),
		db:       db,
	}
}

// OnEviction persists intermediary nodes to [nodeDB] as they are evicted from
// [nodeCache].
// Note that this is called by [db.nodeCache] with that cache's lock held, so
// the movement of the node from [db.nodeCache] to [db.nodeDB] is atomic.
// That is, as soon as [db.nodeCache] reports that it no longer has an evicted
// node, the node is guaranteed to be in [db.nodeDB].
func (db *Database) OnEviction(node *node) {
	if node == nil || node.hasValue() {
		// only persist intermediary nodes
		return
	}
	nodeBytes, err := node.marshal()
	if err != nil {
		// TODO: Handle this error correctly
		panic(err)
	}
	if err = db.nodeDB.Put(node.key.Bytes(), nodeBytes); err != nil {
		// TODO: Handle this error correctly
		panic(err)
	}
}

// Inserts the key/value pair into the db.
func (db *Database) Put(k, v []byte) error {
	return db.Insert(context.Background(), k, v)
}

func (db *Database) Remove(ctx context.Context, key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	view, err := db.newView(ctx)
	if err != nil {
		return err
	}
	// Don't need to lock [view] because nobody else has a reference to it.
	if err = view.remove(ctx, key); err != nil {
		return err
	}
	return view.commit(ctx)
}

// Assumes [db.lock] is held.
func (db *Database) commitBatch(ops linkedhashmap.LinkedHashmap[string, batchOp]) error {
	view, err := db.prepareBatchView(context.Background(), ops)
	if err != nil {
		return err
	}
	return view.commit(context.Background())
}

// Applies unwritten changes into the db.
// Assumes [db.lock] is held.
func (db *Database) commitChanges(ctx context.Context, changes *changeSummary) error {
	_, span := db.tracer.Start(ctx, "MerkleDB.commitChanges", oteltrace.WithAttributes(
		attribute.Int("nodesChanged", len(changes.nodes)),
		attribute.Int("valuesChanged", len(changes.values)),
	))
	defer span.End()

	if db.closed {
		return database.ErrClosed
	}

	if len(changes.nodes) == 0 {
		return nil
	}

	rootChange, ok := changes.nodes[RootPath]
	if !ok {
		return errNoNewRoot
	}
	changes.rootID = rootChange.after.id

	// commit any outstanding cache evicted nodes.
	// Note that we do this here because below we may Abort
	// [db.nodeDB], which would cause us to lose these changes.
	if err := db.nodeDB.Commit(); err != nil {
		return err
	}

	_, nodesSpan := db.tracer.Start(ctx, "MerkleDB.commitChanges.writeNodes")
	for key, nodeChange := range changes.nodes {
		if nodeChange.after == nil {
			db.metrics.IOKeyWrite()
			if err := db.nodeDB.Delete(key.Bytes()); err != nil {
				db.nodeDB.Abort()
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
			nodeBytes, err := nodeChange.after.marshal()
			if err != nil {
				db.nodeDB.Abort()
				nodesSpan.End()
				return err
			}

			if err := db.nodeDB.Put(key.Bytes(), nodeBytes); err != nil {
				db.nodeDB.Abort()
				nodesSpan.End()
				return err
			}
		}
	}
	nodesSpan.End()

	_, commitSpan := db.tracer.Start(ctx, "MerkleDB.commitChanges.dbCommit")
	err := db.nodeDB.Commit()
	commitSpan.End()
	if err != nil {
		db.nodeDB.Abort()
		return err
	}

	// Only modify in-memory state after the commit succeeds
	// so that we don't need to cleanup on error.
	db.root = rootChange.after

	for key, nodeChange := range changes.nodes {
		db.putNodeInCache(key, nodeChange.after)
	}

	_, valuesSpan := db.tracer.Start(ctx, "MerkleDB.commitChanges.writeValues")
	for key, valueChange := range changes.values {
		serializedKey := key.Serialize()
		db.putValueInCache(string(serializedKey.Value), valueChange.after)
	}
	valuesSpan.End()

	db.history.record(changes)
	return nil
}

func (db *Database) initializeRootIfNeeded(_ context.Context) (ids.ID, error) {
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
	if err := db.nodeDB.Put(rootKey, rootBytes); err != nil {
		return ids.Empty, err
	}

	return db.root.id, db.nodeDB.Commit()
}

// Returns a view of the trie as it was when it had root [rootID] for keys within range [start, end].
// Assumes [db.lock] is read locked.
func (db *Database) getHistoricalViewForRangeProof(
	ctx context.Context,
	rootID ids.ID,
	start []byte,
	end []byte,
) (*trieView, error) {
	currentRootID := db.getMerkleRoot()

	// looking for the trie's current root id, so return the trie unmodified
	if currentRootID == rootID {
		return newTrieView(ctx, db, nil, nil, 100)
	}

	changeHistory, err := db.history.getChangesToGetToRoot(rootID, start, end)
	if err != nil {
		return nil, err
	}
	return newTrieView(ctx, db, nil, changeHistory, len(changeHistory.nodes))
}

// Returns all of the keys in range [start, end] that aren't in [keySet].
// If [start] is nil, then the range has no lower bound.
// If [end] is nil, then the range has no upper bound.
// Assumes [db.lock] is read locked.
func (db *Database) getKeysNotInSet(start, end []byte, keySet set.Set[string]) ([][]byte, error) {
	it := db.NewIteratorWithStart(start)
	defer it.Release()

	keysNotInSet := make([][]byte, 0, len(keySet))
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

// Returns the node with the given [key].
// Returns database.ErrNotFound if the node doesn't exist.
// Assumes [db.lock] is read locked.
func (db *Database) getNode(_ context.Context, key path) (*node, error) {
	if key == RootPath {
		return db.root.clone(), nil
	}

	if n, isCached := db.getNodeInCache(key); isCached {
		db.metrics.DBNodeCacheHit()
		if n == nil {
			return nil, database.ErrNotFound
		}
		return n.clone(), nil
	}

	db.metrics.DBNodeCacheMiss()
	db.metrics.IOKeyRead()
	rawBytes, err := db.nodeDB.Get(key.Bytes())
	if err != nil {
		if err == database.ErrNotFound {
			// Cache the miss.
			db.putNodeInCache(key, nil)
		}
		return nil, err
	}

	node, err := parseNode(key, rawBytes)
	if err != nil {
		return nil, err
	}

	db.putNodeInCache(key, node)
	return node.clone(), nil
}

// Assumes [db.lock] is read locked.
func (db *Database) getKeyValues(
	_ context.Context,
	start []byte,
	end []byte,
	maxLength int,
	keysToIgnore set.Set[string],
) ([]KeyValue, error) {
	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	it := db.NewIteratorWithStart(start)
	defer it.Release()

	remainingLength := maxLength
	result := make([]KeyValue, 0, maxLength)
	// Keep adding key/value pairs until one of the following:
	// * We hit a key that is lexicographically larger than the end key.
	// * [maxLength] elements are in [result].
	// * There are no more values to add.
	for remainingLength > 0 && it.Next() {
		key := it.Key()
		if len(end) != 0 && bytes.Compare(it.Key(), end) > 0 {
			break
		}
		if keysToIgnore.Contains(string(key)) {
			continue
		}
		result = append(result, KeyValue{
			Key:   key,
			Value: it.Value(),
		})
		remainingLength--
	}

	return result, it.Error()
}

// Returns a new view atop [db] with the changes in [ops] applied to it.
// Assumes [db.lock] is read locked.
func (db *Database) prepareBatchView(
	ctx context.Context,
	ops linkedhashmap.LinkedHashmap[string, batchOp],
) (*trieView, error) {
	view, err := db.newPreallocatedView(ctx, ops.Len())
	if err != nil {
		return nil, err
	}
	// Don't need to lock [view] because nobody else has a reference to it.

	// write into the trie
	it := ops.NewIterator()
	for it.Next() {
		if it.Value().delete {
			if err := view.remove(ctx, []byte(it.Key())); err != nil {
				return nil, err
			}
		} else if err := view.insert(ctx, []byte(it.Key()), it.Value().value); err != nil {
			return nil, err
		}
	}

	return view, nil
}

// Returns a new view atop [db] with the key/value pairs in [proof.KeyValues]
// inserted and the key/value pairs in [proof.DeletedKeys] removed.
// Assumes [db.lock] is read locked.
func (db *Database) prepareChangeProofView(ctx context.Context, proof *ChangeProof) (*trieView, error) {
	view, err := db.newPreallocatedView(ctx, len(proof.KeyValues))
	if err != nil {
		return nil, err
	}
	// Don't need to lock [view] because nobody else has a reference to it.

	for _, kv := range proof.KeyValues {
		if err := view.insert(ctx, kv.Key, kv.Value); err != nil {
			return nil, err
		}
	}

	for _, keyToDelete := range proof.DeletedKeys {
		if err := view.remove(ctx, keyToDelete); err != nil {
			return nil, err
		}
	}
	return view, nil
}

// Returns a new view atop [db] with the key/value pairs in [proof.KeyValues] added and
// any existing key-value pairs in the proof's range but not in the proof removed.
// Assumes [db.lock] is read locked.
func (db *Database) prepareRangeProofView(ctx context.Context, start []byte, proof *RangeProof) (*trieView, error) {
	view, err := db.newPreallocatedView(ctx, len(proof.KeyValues))
	if err != nil {
		return nil, err
	}
	// Don't need to lock [view] because nobody else has a reference to it.

	keys := set.NewSet[string](len(proof.KeyValues))
	for _, kv := range proof.KeyValues {
		keys.Add(string(kv.Key))
		if err := view.insert(ctx, kv.Key, kv.Value); err != nil {
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
		if err := view.remove(ctx, keyToDelete); err != nil {
			return nil, err
		}
	}
	return view, nil
}

// Assumes [db.lock] is read locked.
// This is required because putting a node in [db.nodeCache] can cause an eviction,
// which puts a node in [db.nodeDB], and we don't want to put anything in [db.nodeDB]
// after [db] is closed.
func (db *Database) putNodeInCache(key path, n *node) {
	// TODO Cache metrics
	// Note that this may cause a node to be evicted from the cache,
	// which will call [OnEviction].
	db.nodeCache.Put(key, n)
}

func (db *Database) getNodeInCache(key path) (*node, bool) {
	// TODO Cache metrics
	if node, ok := db.nodeCache.Get(key); ok {
		if node == nil {
			return nil, true
		}
		return node, true
	}
	return nil, false
}

func (db *Database) putValueInCache(key string, value Maybe[[]byte]) {
	// TODO Cache metrics
	db.valueCache.Put(key, value)
}

func (db *Database) getValueInCache(key string) (Maybe[[]byte], bool) {
	if value, ok := db.valueCache.Get(key); ok {
		return value, true
	}
	return Nothing[[]byte](), false
}
