// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"

	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	// TODO: name better
	rebuildViewSizeFractionOfCacheSize   = 50
	minRebuildViewSizePerCommit          = 1000
	rebuildIntermediateDeletionWriteSize = units.MiB
	valueNodePrefixLen                   = 1
	cacheEntryOverHead                   = 8
)

var (
	_ MerkleDB = (*merkleDB)(nil)

	metadataPrefix         = []byte{0}
	valueNodePrefix        = []byte{1}
	intermediateNodePrefix = []byte{2}

	// cleanShutdownKey is used to flag that the database did (or did not)
	// previously shutdown correctly.
	//
	// If this key has value [hadCleanShutdown] it must be true that all
	// intermediate nodes of the trie are correctly populated on disk and that
	// the [rootDBKey] has the correct key for the root node.
	//
	// If this key has value [didNotHaveCleanShutdown] the intermediate nodes of
	// the trie may not be correct and the [rootDBKey] may not exist or point to
	// a node that node longer exists.
	//
	// Regardless of the value of [cleanShutdownKey], the value nodes must
	// always be persisted correctly.
	cleanShutdownKey        = []byte(string(metadataPrefix) + "cleanShutdown")
	rootDBKey               = []byte(string(metadataPrefix) + "root")
	hadCleanShutdown        = []byte{1}
	didNotHaveCleanShutdown = []byte{0}

	errSameRoot    = errors.New("start and end root are the same")
	errTooManyKeys = errors.New("response contains more than requested keys")
)

type ChangeProofer interface {
	// GetChangeProof returns a proof for a subset of the key/value changes in key range
	// [start, end] that occurred between [startRootID] and [endRootID].
	// Returns at most [maxLength] key/value pairs.
	// Returns [ErrInsufficientHistory] if this node has insufficient history
	// to generate the proof.
	// Returns ErrEmptyProof if [endRootID] is ids.Empty.
	// Note that [endRootID] == ids.Empty means the trie is empty
	// (i.e. we don't need a change proof.)
	// Returns [ErrNoEndRoot], which wraps [ErrInsufficientHistory], if the
	// history doesn't contain the [endRootID].
	GetChangeProof(
		ctx context.Context,
		startRootID ids.ID,
		endRootID ids.ID,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		maxLength int,
	) (*ChangeProof, error)

	// Returns nil iff all the following hold:
	//   - [start] <= [end].
	//   - [proof] is non-empty.
	//   - [proof.KeyValues] is not longer than [maxLength].
	//   - All keys in [proof.KeyValues] and [proof.DeletedKeys] are in [start, end].
	//     If [start] is nothing, all keys are considered > [start].
	//     If [end] is nothing, all keys are considered < [end].
	//   - [proof.KeyValues] and [proof.DeletedKeys] are sorted in order of increasing key.
	//   - [proof.StartProof] and [proof.EndProof] are well-formed.
	//   - When the changes in [proof.KeyChanges] are applied,
	//     the root ID of the database is [expectedEndRootID].
	VerifyChangeProof(
		ctx context.Context,
		proof *ChangeProof,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		expectedEndRootID ids.ID,
		maxLength int,
	) error

	// CommitChangeProof commits the key/value pairs within the [proof] to the db.
	// [end] is the largest possible key in the range this [proof] covers.
	// [end] may be Nothing, meaning that there is no upper bound on the
	// range.
	// The key returned indicates the next key after the largest key in
	// the proof. If the database has all keys from the start of the proof
	// until [end], then Nothing is returned.
	CommitChangeProof(ctx context.Context, end maybe.Maybe[[]byte], proof *ChangeProof) (maybe.Maybe[[]byte], error)
}

type RangeProofer interface {
	// GetRangeProofAtRoot returns a proof for the key/value pairs in this trie within the range
	// [start, end] when the root of the trie was [rootID].
	// If [start] is Nothing, there's no lower bound on the range.
	// If [end] is Nothing, there's no upper bound on the range.
	// Returns ErrEmptyProof if [rootID] is ids.Empty.
	// Note that [rootID] == ids.Empty means the trie is empty
	// (i.e. we don't need a range proof.)
	GetRangeProofAtRoot(
		ctx context.Context,
		rootID ids.ID,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		maxLength int,
	) (*RangeProof, error)

	// Returns nil iff all the following hold:
	//   - [start] <= [end].
	//   - [proof] is non-empty.
	//   - [proof.KeyValues] is not longer than [maxLength].
	//   - All keys in [proof.KeyValues] and [proof.DeletedKeys] are in [start, end].
	//     If [start] is nothing, all keys are considered > [start].
	//     If [end] is nothing, all keys are considered < [end].
	//   - [proof.KeyValues] and [proof.DeletedKeys] are sorted in order of increasing key.
	//   - [proof.StartProof] and [proof.EndProof] are well-formed.
	//   - When the changes in [proof.KeyChanges] are applied,
	//     the root ID of the database is [expectedEndRootID].
	VerifyRangeProof(
		ctx context.Context,
		proof *RangeProof,
		start maybe.Maybe[[]byte],
		end maybe.Maybe[[]byte],
		expectedEndRootID ids.ID,
		maxLength int,
	) error

	// CommitRangeProof commits the key/value pairs within the [proof] to the db.
	// [start] is the smallest possible key in the range this [proof] covers.
	// [end] is the largest possible key in the range this [proof] covers.
	// [end] may be Nothing, meaning that there is no upper bound on the
	// range.
	// The key returned indicates the next key after the largest key in
	// the proof. If the database has all keys from the start of the proof
	// until [end], then Nothing is returned.
	CommitRangeProof(ctx context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error)
}

type Clearer interface {
	// Deletes all key/value pairs from the database
	// and clears the change history.
	Clear() error
}

type Prefetcher interface {
	// PrefetchPath attempts to load all trie nodes on the path of [key]
	// into the cache.
	PrefetchPath(key []byte) error

	// PrefetchPaths attempts to load all trie nodes on the paths of [keys]
	// into the cache.
	//
	// Using PrefetchPaths can be more efficient than PrefetchPath because
	// the underlying view used to compute each path can be reused.
	PrefetchPaths(keys [][]byte) error
}

type MerkleDB interface {
	database.Database
	Clearer
	View
	MerkleRootGetter
	ProofGetter
	ChangeProofer
	RangeProofer
	Prefetcher
}

func NewConfig() Config {
	return Config{
		BranchFactor:                BranchFactor16,
		Hasher:                      DefaultHasher,
		RootGenConcurrency:          0,
		HistoryLength:               300,
		ValueNodeCacheSize:          units.MiB,
		IntermediateNodeCacheSize:   units.MiB,
		IntermediateWriteBufferSize: units.KiB,
		IntermediateWriteBatchSize:  256 * units.KiB,
		TraceLevel:                  InfoTrace,
		Tracer:                      trace.Noop,
	}
}

type Config struct {
	// BranchFactor determines the number of children each node can have.
	BranchFactor BranchFactor

	// Hasher defines the hash function to use when hashing the trie.
	//
	// If not specified, [DefaultHasher] will be used.
	Hasher Hasher

	// RootGenConcurrency is the number of goroutines to use when
	// generating a new state root.
	//
	// If 0 is specified, [runtime.NumCPU] will be used.
	RootGenConcurrency uint

	// The number of changes to the database that we store in memory in order to
	// serve change proofs.
	HistoryLength uint
	// The number of bytes used to cache nodes with values.
	ValueNodeCacheSize uint
	// The number of bytes used to cache nodes without values.
	IntermediateNodeCacheSize uint
	// The number of bytes used to store nodes without values in memory before forcing them onto disk.
	IntermediateWriteBufferSize uint
	// The number of bytes to write to disk when intermediate nodes are evicted
	// from the write buffer and written to disk.
	IntermediateWriteBatchSize uint
	// If [Reg] is nil, metrics are collected locally but not exported through
	// Prometheus.
	// This may be useful for testing.
	Reg        prometheus.Registerer
	Namespace  string
	TraceLevel TraceLevel
	Tracer     trace.Tracer
}

// merkleDB can only be edited by committing changes from a view.
type merkleDB struct {
	// Must be held when reading/writing fields.
	lock sync.RWMutex

	// Must be held when preparing work to be committed to the DB.
	// Used to prevent editing of the trie without restricting read access
	// until the full set of changes is ready to be written.
	// Should be held before taking [db.lock]
	commitLock sync.RWMutex

	// Contains all the key-value pairs stored by this database,
	// including metadata, intermediate nodes and value nodes.
	baseDB database.Database

	valueNodeDB        *valueNodeDB
	intermediateNodeDB *intermediateNodeDB

	// Stores change lists. Used to serve change proofs and construct
	// historical views of the trie.
	history *trieHistory

	// True iff the db has been closed.
	closed bool

	metrics metrics

	debugTracer trace.Tracer
	infoTracer  trace.Tracer

	// The root of this trie.
	// Nothing if the trie is empty.
	root maybe.Maybe[*node]

	rootID ids.ID

	// Valid children of this trie.
	childViews []*view

	// hashNodesKeyPool controls the number of goroutines that are created
	// inside [hashChangedNode] at any given time and provides slices for the
	// keys needed while hashing.
	hashNodesKeyPool *bytesPool

	tokenSize int

	hasher Hasher
}

// New returns a new merkle database.
func New(ctx context.Context, db database.Database, config Config) (MerkleDB, error) {
	metrics, err := newMetrics(config.Namespace, config.Reg)
	if err != nil {
		return nil, err
	}
	return newDatabase(ctx, db, config, metrics)
}

func newDatabase(
	ctx context.Context,
	db database.Database,
	config Config,
	metrics metrics,
) (*merkleDB, error) {
	if err := config.BranchFactor.Valid(); err != nil {
		return nil, err
	}

	hasher := config.Hasher
	if hasher == nil {
		hasher = DefaultHasher
	}

	rootGenConcurrency := runtime.NumCPU()
	if config.RootGenConcurrency != 0 {
		rootGenConcurrency = int(config.RootGenConcurrency)
	}

	// Share a bytes pool between the intermediateNodeDB and valueNodeDB to
	// reduce memory allocations.
	bufferPool := utils.NewBytesPool()

	trieDB := &merkleDB{
		metrics: metrics,
		baseDB:  db,
		intermediateNodeDB: newIntermediateNodeDB(
			db,
			bufferPool,
			metrics,
			int(config.IntermediateNodeCacheSize),
			int(config.IntermediateWriteBufferSize),
			int(config.IntermediateWriteBatchSize),
			BranchFactorToTokenSize[config.BranchFactor],
			hasher,
		),
		valueNodeDB: newValueNodeDB(
			db,
			bufferPool,
			metrics,
			int(config.ValueNodeCacheSize),
			hasher,
		),
		history:          newTrieHistory(int(config.HistoryLength)),
		debugTracer:      getTracerIfEnabled(config.TraceLevel, DebugTrace, config.Tracer),
		infoTracer:       getTracerIfEnabled(config.TraceLevel, InfoTrace, config.Tracer),
		childViews:       make([]*view, 0, defaultPreallocationSize),
		hashNodesKeyPool: newBytesPool(rootGenConcurrency),
		tokenSize:        BranchFactorToTokenSize[config.BranchFactor],
		hasher:           hasher,
	}

	shutdownType, err := trieDB.baseDB.Get(cleanShutdownKey)
	switch err {
	case nil:
	case database.ErrNotFound:
		// If the marker wasn't found then the DB is being created for the first
		// time and there is nothing to do.
		shutdownType = hadCleanShutdown
	default:
		return nil, err
	}
	if bytes.Equal(shutdownType, didNotHaveCleanShutdown) {
		if err := trieDB.rebuild(ctx, int(config.ValueNodeCacheSize)); err != nil {
			return nil, err
		}
	} else {
		if err := trieDB.initializeRoot(); err != nil {
			return nil, err
		}
	}

	// add current root to history (has no changes)
	trieDB.history.record(&changeSummary{
		rootID: trieDB.rootID,
		rootChange: change[maybe.Maybe[*node]]{
			after: trieDB.root,
		},
		sortedKeys: []Key{},
		nodes:      map[Key]*change[*node]{},
		keyChanges: map[Key]*change[maybe.Maybe[[]byte]]{},
	})

	// mark that the db has not yet been cleanly closed
	err = trieDB.baseDB.Put(cleanShutdownKey, didNotHaveCleanShutdown)
	return trieDB, err
}

// Deletes every intermediate node and rebuilds them by re-adding every key/value.
// TODO: make this more efficient by only clearing out the stale portions of the trie.
func (db *merkleDB) rebuild(ctx context.Context, cacheSize int) error {
	db.root = maybe.Nothing[*node]()
	db.rootID = ids.Empty

	// Delete intermediate nodes.
	if err := database.ClearPrefix(db.baseDB, intermediateNodePrefix, rebuildIntermediateDeletionWriteSize); err != nil {
		return err
	}

	// Add all key-value pairs back into the database.
	opsSizeLimit := max(
		cacheSize/rebuildViewSizeFractionOfCacheSize,
		minRebuildViewSizePerCommit,
	)
	currentOps := make([]database.BatchOp, 0, opsSizeLimit)
	valueIt := db.NewIterator()
	// ensure valueIt is captured and release gets called on the latest copy of valueIt
	defer func() { valueIt.Release() }()
	for valueIt.Next() {
		if len(currentOps) >= opsSizeLimit {
			view, err := newView(db, db, ViewChanges{BatchOps: currentOps, ConsumeBytes: true})
			if err != nil {
				return err
			}
			if err := view.commitToDB(ctx); err != nil {
				return err
			}
			currentOps = make([]database.BatchOp, 0, opsSizeLimit)
			// reset the iterator to prevent memory bloat
			nextValue := valueIt.Key()
			valueIt.Release()
			valueIt = db.NewIteratorWithStart(nextValue)
			continue
		}

		currentOps = append(currentOps, database.BatchOp{
			Key:   valueIt.Key(),
			Value: valueIt.Value(),
		})
	}
	if err := valueIt.Error(); err != nil {
		return err
	}
	view, err := newView(db, db, ViewChanges{BatchOps: currentOps, ConsumeBytes: true})
	if err != nil {
		return err
	}
	if err := view.commitToDB(ctx); err != nil {
		return err
	}
	return db.Compact(nil, nil)
}

func (db *merkleDB) CommitChangeProof(ctx context.Context, end maybe.Maybe[[]byte], proof *ChangeProof) (maybe.Maybe[[]byte], error) {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.closed {
		return maybe.Nothing[[]byte](), database.ErrClosed
	}

	// If there's no changes to make, we don't have to commit anything.
	if len(proof.KeyChanges) == 0 {
		return maybe.Nothing[[]byte](), nil
	}

	ops := make([]database.BatchOp, len(proof.KeyChanges))
	for i, kv := range proof.KeyChanges {
		ops[i] = database.BatchOp{
			Key:    kv.Key,
			Value:  kv.Value.Value(),
			Delete: kv.Value.IsNothing(),
		}
	}

	view, err := newView(db, db, ViewChanges{BatchOps: ops})
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// Commit the changes to the DB.
	largestKey := end
	if len(proof.KeyChanges) > 0 {
		if err := view.commitToDB(ctx); err != nil {
			return maybe.Nothing[[]byte](), err
		}
		largestKey = maybe.Some(proof.KeyChanges[len(proof.KeyChanges)-1].Key)
	}
	return db.findNextKey(largestKey, end, proof.EndProof)
}

func (db *merkleDB) CommitRangeProof(ctx context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error) {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.closed {
		return maybe.Nothing[[]byte](), database.ErrClosed
	}

	ops := make([]database.BatchOp, len(proof.KeyChanges))
	keys := set.NewSet[string](len(proof.KeyChanges))
	for i, kv := range proof.KeyChanges {
		keys.Add(string(kv.Key))
		ops[i] = database.BatchOp{
			Key:   kv.Key,
			Value: kv.Value.Value(),
		}
	}

	largestKey := end
	if len(proof.KeyChanges) > 0 {
		largestKey = maybe.Some(proof.KeyChanges[len(proof.KeyChanges)-1].Key)
	}
	keysToDelete, err := db.getKeysNotInSet(start, largestKey, keys)
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}
	for _, keyToDelete := range keysToDelete {
		ops = append(ops, database.BatchOp{
			Key:    keyToDelete,
			Delete: true,
		})
	}

	// Don't need to lock [view] because nobody else has a reference to it.
	view, err := newView(db, db, ViewChanges{BatchOps: ops})
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	if err := view.commitToDB(ctx); err != nil {
		return maybe.Nothing[[]byte](), err
	}
	return db.findNextKey(largestKey, end, proof.EndProof)
}

func (db *merkleDB) Compact(start []byte, limit []byte) error {
	if db.closed {
		return database.ErrClosed
	}
	return db.baseDB.Compact(start, limit)
}

func (db *merkleDB) Close() error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	db.lock.Lock()
	defer db.lock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	// mark all children as no longer valid because the db has closed
	db.invalidateChildrenExcept(nil)

	db.closed = true
	db.valueNodeDB.Close()
	// Flush intermediary nodes to disk.
	if err := db.intermediateNodeDB.Flush(); err != nil {
		return err
	}

	var (
		batch = db.baseDB.NewBatch()
		err   error
	)
	// Write the root key
	if db.root.IsNothing() {
		err = batch.Delete(rootDBKey)
	} else {
		rootKey := encodeKey(db.root.Value().key)
		err = batch.Put(rootDBKey, rootKey)
	}
	if err != nil {
		return err
	}

	// Write the clean shutdown marker
	if err := batch.Put(cleanShutdownKey, hadCleanShutdown); err != nil {
		return err
	}
	return batch.Write()
}

func (db *merkleDB) PrefetchPaths(keys [][]byte) error {
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}

	for _, key := range keys {
		if err := db.prefetchPath(key); err != nil {
			return err
		}
	}

	return nil
}

func (db *merkleDB) PrefetchPath(key []byte) error {
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}
	return db.prefetchPath(key)
}

func (db *merkleDB) prefetchPath(keyBytes []byte) error {
	return visitPathToKey(db, ToKey(keyBytes), func(n *node) error {
		if n.hasValue() {
			db.valueNodeDB.nodeCache.Put(n.key, n)
		} else {
			db.intermediateNodeDB.nodeCache.Put(n.key, n)
		}
		return nil
	})
}

func (db *merkleDB) Get(key []byte) ([]byte, error) {
	// this is a duplicate because the database interface doesn't support
	// contexts, which are used for tracing
	return db.GetValue(context.Background(), key)
}

func (db *merkleDB) GetValues(ctx context.Context, keys [][]byte) ([][]byte, []error) {
	_, span := db.debugTracer.Start(ctx, "MerkleDB.GetValues", oteltrace.WithAttributes(
		attribute.Int("keyCount", len(keys)),
	))
	defer span.End()

	// Lock to ensure no commit happens during the reads.
	db.lock.RLock()
	defer db.lock.RUnlock()

	values := make([][]byte, len(keys))
	getErrors := make([]error, len(keys))
	for i, key := range keys {
		values[i], getErrors[i] = db.getValueCopy(ToKey(key))
	}
	return values, getErrors
}

// GetValue returns the value associated with [key].
// Returns database.ErrNotFound if it doesn't exist.
func (db *merkleDB) GetValue(ctx context.Context, key []byte) ([]byte, error) {
	_, span := db.debugTracer.Start(ctx, "MerkleDB.GetValue")
	defer span.End()

	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getValueCopy(ToKey(key))
}

// getValueCopy returns a copy of the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
// Assumes [db.lock] is read locked.
func (db *merkleDB) getValueCopy(key Key) ([]byte, error) {
	val, err := db.getValueWithoutLock(key)
	if err != nil {
		return nil, err
	}
	return slices.Clone(val), nil
}

// getValue returns the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
// Assumes [db.lock] isn't held.
func (db *merkleDB) getValue(key Key) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.getValueWithoutLock(key)
}

// getValueWithoutLock returns the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
// Assumes [db.lock] is read locked.
func (db *merkleDB) getValueWithoutLock(key Key) ([]byte, error) {
	if db.closed {
		return nil, database.ErrClosed
	}

	n, err := db.getNode(key, true /* hasValue */)
	if err != nil {
		return nil, err
	}
	if n.value.IsNothing() {
		return nil, database.ErrNotFound
	}
	return n.value.Value(), nil
}

func (db *merkleDB) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	_, span := db.infoTracer.Start(ctx, "MerkleDB.GetMerkleRoot")
	defer span.End()

	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return ids.Empty, database.ErrClosed
	}

	return db.getMerkleRoot(), nil
}

// Assumes [db.lock] or [db.commitLock] is read locked.
func (db *merkleDB) getMerkleRoot() ids.ID {
	return db.rootID
}

func (db *merkleDB) GetProof(ctx context.Context, key []byte) (*Proof, error) {
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	_, span := db.infoTracer.Start(ctx, "MerkleDB.GetProof")
	defer span.End()

	if db.closed {
		return nil, database.ErrClosed
	}

	return getProof(db, key)
}

func (db *merkleDB) GetRangeProof(
	ctx context.Context,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) (*RangeProof, error) {
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	_, span := db.infoTracer.Start(ctx, "MerkleDB.GetRangeProof")
	defer span.End()

	if db.closed {
		return nil, database.ErrClosed
	}

	return getRangeProof(db, start, end, maxLength)
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

	_, span := db.infoTracer.Start(ctx, "MerkleDB.GetRangeProofAtRoot")
	defer span.End()

	switch {
	case db.closed:
		return nil, database.ErrClosed
	case maxLength <= 0:
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	case rootID == ids.Empty:
		return nil, ErrEmptyProof
	}

	historicalTrie, err := db.getTrieAtRootForRange(rootID, start, end)
	if err != nil {
		return nil, err
	}
	return getRangeProof(historicalTrie, start, end, maxLength)
}

func (db *merkleDB) GetChangeProof(
	ctx context.Context,
	startRootID ids.ID,
	endRootID ids.ID,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) (*ChangeProof, error) {
	_, span := db.infoTracer.Start(ctx, "MerkleDB.GetChangeProof")
	defer span.End()

	switch {
	case start.HasValue() && end.HasValue() && bytes.Compare(start.Value(), end.Value()) == 1:
		return nil, ErrStartAfterEnd
	case startRootID == endRootID:
		return nil, errSameRoot
	case endRootID == ids.Empty:
		return nil, ErrEmptyProof
	}

	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}

	// [valueChanges] contains a subset of the keys that were added or had their
	// values modified between [startRootID] to [endRootID].
	valueChanges, err := db.history.getValueChanges(startRootID, endRootID, start, end, maxLength)
	if err != nil {
		return nil, err
	}

	result := &ChangeProof{
		KeyChanges: make([]KeyChange, len(valueChanges)),
	}

	for i, valueChange := range valueChanges {
		result.KeyChanges[i] = KeyChange{
			Key: valueChange.key.Bytes(),
			// create a copy so edits of the []byte don't affect the db
			Value: maybe.Bind(valueChange.change.after, slices.Clone[[]byte]),
		}
	}

	largestKey := end
	if len(result.KeyChanges) > 0 {
		largestKey = maybe.Some(result.KeyChanges[len(result.KeyChanges)-1].Key)
	}

	// Since we hold [db.commitlock] we must still have sufficient
	// history to recreate the trie at [endRootID].
	historicalTrie, err := db.getTrieAtRootForRange(endRootID, start, largestKey)
	if err != nil {
		return nil, err
	}

	if largestKey.HasValue() {
		endProof, err := getProof(historicalTrie, largestKey.Value())
		if err != nil {
			return nil, err
		}
		result.EndProof = endProof.Path
	}

	if start.HasValue() {
		startProof, err := getProof(historicalTrie, start.Value())
		if err != nil {
			return nil, err
		}
		result.StartProof = startProof.Path

		// strip out any common nodes to reduce proof size
		commonNodeIndex := 0
		for ; commonNodeIndex < len(result.StartProof) &&
			commonNodeIndex < len(result.EndProof) &&
			result.StartProof[commonNodeIndex].Key == result.EndProof[commonNodeIndex].Key; commonNodeIndex++ {
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

// NewView returns a new view on top of this Trie where the passed changes
// have been applied.
//
// Changes made to the view will only be reflected in the original trie if
// Commit is called.
//
// Assumes [db.commitLock] and [db.lock] aren't held.
func (db *merkleDB) NewView(
	_ context.Context,
	changes ViewChanges,
) (View, error) {
	// ensure the db doesn't change while creating the new view
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	if db.closed {
		return nil, database.ErrClosed
	}

	view, err := newView(db, db, changes)
	if err != nil {
		return nil, err
	}

	// ensure access to childViews is protected
	db.lock.Lock()
	defer db.lock.Unlock()

	db.childViews = append(db.childViews, view)
	return view, nil
}

func (db *merkleDB) Has(k []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if db.closed {
		return false, database.ErrClosed
	}

	_, err := db.getValueWithoutLock(ToKey(k))
	if errors.Is(err, database.ErrNotFound) {
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
	return db.baseDB.HealthCheck(ctx)
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
	return db.valueNodeDB.newIteratorWithStartAndPrefix(start, prefix)
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

	view, err := newView(db, db, ViewChanges{BatchOps: []database.BatchOp{{Key: k, Value: v}}})
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

	if db.closed {
		return database.ErrClosed
	}

	view, err := newView(db, db,
		ViewChanges{
			BatchOps: []database.BatchOp{{
				Key:    key,
				Delete: true,
			}},
			ConsumeBytes: true,
		})
	if err != nil {
		return err
	}
	return view.commitToDB(ctx)
}

// Assumes values inside [ops] are safe to reference after the function
// returns. Assumes [db.lock] isn't held.
func (db *merkleDB) commitBatch(ops []database.BatchOp) error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	if db.closed {
		return database.ErrClosed
	}

	view, err := newView(db, db, ViewChanges{BatchOps: ops, ConsumeBytes: true})
	if err != nil {
		return err
	}
	return view.commitToDB(context.Background())
}

// commitView commits the changes in [trieToCommit] to [db].
// Assumes [trieToCommit]'s node IDs have been calculated.
// Assumes [db.commitLock] is held.
func (db *merkleDB) commitView(ctx context.Context, trieToCommit *view) error {
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
	_, span := db.infoTracer.Start(ctx, "MerkleDB.commitView", oteltrace.WithAttributes(
		attribute.Int("nodesChanged", len(changes.nodes)),
		attribute.Int("valuesChanged", len(changes.keyChanges)),
	))
	defer span.End()

	// invalidate all child views except for the view being committed
	db.invalidateChildrenExcept(trieToCommit)

	// move any child views of the committed trie onto the db
	db.moveChildViewsToDB(trieToCommit)

	if len(changes.nodes) == 0 {
		return nil
	}

	valueNodeBatch := db.baseDB.NewBatch()
	if err := db.applyChanges(ctx, valueNodeBatch, changes); err != nil {
		return err
	}

	if err := db.commitValueChanges(ctx, valueNodeBatch); err != nil {
		return err
	}

	db.history.record(changes)

	// Update root in database.
	db.root = changes.rootChange.after
	db.rootID = changes.rootID
	return nil
}

// moveChildViewsToDB removes any child views from the trieToCommit and moves
// them to the db.
//
// assumes [db.lock] is held
func (db *merkleDB) moveChildViewsToDB(trieToCommit *view) {
	trieToCommit.validityTrackingLock.Lock()
	defer trieToCommit.validityTrackingLock.Unlock()

	for _, childView := range trieToCommit.childViews {
		childView.updateParent(db)
		db.childViews = append(db.childViews, childView)
	}
	trieToCommit.childViews = make([]*view, 0, defaultPreallocationSize)
}

// applyChanges takes the [changes] and applies them to [db.intermediateNodeDB]
// and [valueNodeBatch].
//
// assumes [db.lock] is held
func (db *merkleDB) applyChanges(ctx context.Context, valueNodeBatch database.KeyValueWriterDeleter, changes *changeSummary) error {
	_, span := db.infoTracer.Start(ctx, "MerkleDB.applyChanges")
	defer span.End()

	for key, nodeChange := range changes.nodes {
		shouldAddIntermediate := nodeChange.after != nil && !nodeChange.after.hasValue()
		shouldDeleteIntermediate := !shouldAddIntermediate && nodeChange.before != nil && !nodeChange.before.hasValue()

		shouldAddValue := nodeChange.after != nil && nodeChange.after.hasValue()
		shouldDeleteValue := !shouldAddValue && nodeChange.before != nil && nodeChange.before.hasValue()

		if shouldAddIntermediate {
			if err := db.intermediateNodeDB.Put(key, nodeChange.after); err != nil {
				return err
			}
		} else if shouldDeleteIntermediate {
			if err := db.intermediateNodeDB.Delete(key); err != nil {
				return err
			}
		}

		if shouldAddValue {
			if err := db.valueNodeDB.Write(valueNodeBatch, key, nodeChange.after); err != nil {
				return err
			}
		} else if shouldDeleteValue {
			if err := db.valueNodeDB.Write(valueNodeBatch, key, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// commitValueChanges is a thin wrapper around [valueNodeBatch.Write()] to
// provide tracing.
func (db *merkleDB) commitValueChanges(ctx context.Context, valueNodeBatch database.Batch) error {
	_, span := db.infoTracer.Start(ctx, "MerkleDB.commitValueChanges")
	defer span.End()

	return valueNodeBatch.Write()
}

// CommitToDB is a no-op for db since it is already in sync with itself.
// This exists to satisfy the View interface.
func (*merkleDB) CommitToDB(context.Context) error {
	return nil
}

// This is defined on merkleDB instead of ChangeProof
// because it accesses database internals.
// Assumes [db.lock] isn't held.
func (db *merkleDB) VerifyChangeProof(
	ctx context.Context,
	proof *ChangeProof,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	expectedEndRootID ids.ID,
	maxLength int,
) error {
	if proof == nil {
		return ErrEmptyProof
	}
	if len(proof.KeyChanges) > maxLength {
		return fmt.Errorf("%w: [%d], max: [%d]", errTooManyKeys, len(proof.KeyChanges), maxLength)
	}

	startProofKey := maybe.Bind(start, ToKey)
	endProofKey := maybe.Bind(end, ToKey)

	// Update [endProofKey] with the largest key in [keyValues].
	if len(proof.KeyChanges) > 0 {
		endProofKey = maybe.Some(ToKey(proof.KeyChanges[len(proof.KeyChanges)-1].Key))
	}

	// Validate proof.
	if err := validateChangeProof(
		startProofKey,
		maybe.Bind(end, ToKey),
		proof.StartProof,
		proof.EndProof,
		proof.KeyChanges,
		endProofKey,
		db.tokenSize,
	); err != nil {
		return err
	}

	// Prevent commit writes to DB, but not prevent DB reads
	db.commitLock.RLock()
	defer db.commitLock.RUnlock()

	if db.closed {
		return database.ErrClosed
	}

	// Ensure that the [startProof] has correct values.
	if err := verifyChangeProofKeyValues(
		ctx,
		db,
		proof.KeyChanges,
		proof.StartProof,
		startProofKey,
		endProofKey,
		db.hasher,
	); err != nil {
		return fmt.Errorf("failed to verify start proof nodes: %w", err)
	}

	// Ensure that the [endProof] has correct values.
	if err := verifyChangeProofKeyValues(
		ctx,
		db,
		proof.KeyChanges,
		proof.EndProof,
		startProofKey,
		endProofKey,
		db.hasher,
	); err != nil {
		return fmt.Errorf("failed to validate end proof nodes: %w", err)
	}

	// Prepare ops for the creation of the view.
	ops := make([]database.BatchOp, len(proof.KeyChanges))
	for i, kv := range proof.KeyChanges {
		ops[i] = database.BatchOp{
			Key:    kv.Key,
			Value:  kv.Value.Value(),
			Delete: kv.Value.IsNothing(),
		}
	}

	// Don't need to lock [view] because nobody else has a reference to it.
	view, err := newView(db, db, ViewChanges{BatchOps: ops, ConsumeBytes: true})
	if err != nil {
		return err
	}

	// For all the nodes along the edges of the proofs, insert the children whose
	// keys are less than [insertChildrenLessThan] or whose keys are greater
	// than [insertChildrenGreaterThan] into the trie so that we get the
	// expected root ID (if this proof is valid).
	if err := addPathInfo(
		view,
		proof.StartProof,
		startProofKey,
		endProofKey,
	); err != nil {
		return fmt.Errorf("failed to add start proof path info: %w", err)
	}

	if err := addPathInfo(
		view,
		proof.EndProof,
		startProofKey,
		endProofKey,
	); err != nil {
		return fmt.Errorf("failed to add end proof path info: %w", err)
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

func (db *merkleDB) VerifyRangeProof(
	ctx context.Context,
	proof *RangeProof,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	expectedEndRootID ids.ID,
	maxLength int,
) error {
	return proof.Verify(
		ctx,
		start,
		end,
		expectedEndRootID,
		db.tokenSize,
		db.hasher,
		maxLength,
	)
}

// Invalidates and removes any child views that aren't [exception].
// Assumes [db.lock] is held.
func (db *merkleDB) invalidateChildrenExcept(exception *view) {
	isTrackedView := false

	for _, childView := range db.childViews {
		if childView != exception {
			childView.invalidate()
		} else {
			isTrackedView = true
		}
	}
	db.childViews = make([]*view, 0, defaultPreallocationSize)
	if isTrackedView {
		db.childViews = append(db.childViews, exception)
	}
}

// If the root is on disk, set [db.root] to it.
// Otherwise leave [db.root] as Nothing.
func (db *merkleDB) initializeRoot() error {
	rootKeyBytes, err := db.baseDB.Get(rootDBKey)
	if errors.Is(err, database.ErrNotFound) {
		return nil // Root isn't on disk.
	}
	if err != nil {
		return err
	}

	// Root is on disk.
	rootKey, err := decodeKey(rootKeyBytes)
	if err != nil {
		return err
	}

	// First, see if root is an intermediate node.
	root, err := db.getEditableNode(rootKey, false /* hasValue */)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			return err
		}

		// The root must be a value node.
		root, err = db.getEditableNode(rootKey, true /* hasValue */)
		if err != nil {
			return err
		}
	}

	db.rootID = db.hasher.HashNode(root)
	db.metrics.HashCalculated()

	db.root = maybe.Some(root)
	return nil
}

// Returns a view of the trie as it was when it had root [rootID] for keys within range [start, end].
// If [start] is Nothing, there's no lower bound on the range.
// If [end] is Nothing, there's no upper bound on the range.
// Assumes [db.commitLock] is read locked.
func (db *merkleDB) getTrieAtRootForRange(
	rootID ids.ID,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
) (Trie, error) {
	// looking for the trie's current root id, so return the trie unmodified
	if rootID == db.getMerkleRoot() {
		return db, nil
	}

	changeHistory, err := db.history.getChangesToGetToRoot(rootID, start, end)
	if err != nil {
		return nil, err
	}
	return newViewWithChanges(db, changeHistory)
}

// Returns all keys in range [start, end] that aren't in [keySet].
// If [start] is Nothing, then the range has no lower bound.
// If [end] is Nothing, then the range has no upper bound.
func (db *merkleDB) getKeysNotInSet(start, end maybe.Maybe[[]byte], keySet set.Set[string]) ([][]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	it := db.NewIteratorWithStart(start.Value())
	defer it.Release()

	keysNotInSet := make([][]byte, 0, keySet.Len())
	for it.Next() {
		key := it.Key()
		if end.HasValue() && bytes.Compare(key, end.Value()) > 0 {
			break
		}
		if !keySet.Contains(string(key)) {
			keysNotInSet = append(keysNotInSet, key)
		}
	}
	return keysNotInSet, it.Error()
}

// Returns a copy of the node with the given [key].
// hasValue determines which db the key is looked up in (intermediateNodeDB or valueNodeDB)
// This copy may be edited by the caller without affecting the database state.
// Returns database.ErrNotFound if the node doesn't exist.
// Assumes [db.lock] isn't held.
func (db *merkleDB) getEditableNode(key Key, hasValue bool) (*node, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	n, err := db.getNode(key, hasValue)
	if err != nil {
		return nil, err
	}
	return n.clone(), nil
}

// Returns the node with the given [key].
// hasValue determines which db the key is looked up in (intermediateNodeDB or valueNodeDB)
// Editing the returned node affects the database state.
// Returns database.ErrNotFound if the node doesn't exist.
// Assumes [db.lock] is read locked.
func (db *merkleDB) getNode(key Key, hasValue bool) (*node, error) {
	switch {
	case db.closed:
		return nil, database.ErrClosed
	case db.root.HasValue() && key == db.root.Value().key:
		return db.root.Value(), nil
	case hasValue:
		return db.valueNodeDB.Get(key)
	default:
		return db.intermediateNodeDB.Get(key)
	}
}

// Assumes [db.lock] or [db.commitLock] is read locked.
func (db *merkleDB) getRoot() maybe.Maybe[*node] {
	return db.root
}

func (db *merkleDB) Clear() error {
	db.commitLock.Lock()
	defer db.commitLock.Unlock()

	db.lock.Lock()
	defer db.lock.Unlock()

	// Clear nodes from disk and caches
	if err := db.valueNodeDB.Clear(); err != nil {
		return err
	}
	if err := db.intermediateNodeDB.Clear(); err != nil {
		return err
	}

	// Clear root
	db.root = maybe.Nothing[*node]()
	db.rootID = ids.Empty

	// Clear history
	db.history = newTrieHistory(db.history.maxHistoryLen)
	db.history.record(&changeSummary{
		rootID:     db.rootID,
		sortedKeys: []Key{},
		nodes:      map[Key]*change[*node]{},
		keyChanges: map[Key]*change[maybe.Maybe[[]byte]]{},
	})
	return nil
}

func (db *merkleDB) getTokenSize() int {
	return db.tokenSize
}

// Returns [key] prefixed by [prefix].
// The returned *[]byte is taken from [bufferPool] and should be returned to it
// when the caller is done with it.
func addPrefixToKey(bufferPool *utils.BytesPool, prefix []byte, key []byte) *[]byte {
	prefixLen := len(prefix)
	keyLen := prefixLen + len(key)
	prefixedKey := bufferPool.Get(keyLen)
	copy(*prefixedKey, prefix)
	copy((*prefixedKey)[prefixLen:], key)
	return prefixedKey
}

// cacheEntrySize returns a rough approximation of the memory consumed by storing the key and node.
func cacheEntrySize(key Key, n *node) int {
	if n == nil {
		return cacheEntryOverHead + len(key.Bytes())
	}
	return cacheEntryOverHead + len(key.Bytes()) + encodedDBNodeSize(&n.dbNode)
}

// findNextKey returns the start of the key range that should be fetched next
// given that we just received a range/change proof that proved a range of
// key-value pairs ending at [lastReceivedKey].
//
// [rangeEnd] is the end of the range that we want to fetch.
//
// Returns Nothing if there are no more keys to fetch in [lastReceivedKey, rangeEnd].
//
// [endProof] is the end proof of the last proof received.
//
// Invariant: [lastReceivedKey] < [rangeEnd].
// If [rangeEnd] is Nothing it's considered > [lastReceivedKey].
func (db *merkleDB) findNextKey(
	largestHandledKey maybe.Maybe[[]byte],
	rangeEnd maybe.Maybe[[]byte],
	endProof []ProofNode,
) (maybe.Maybe[[]byte], error) {
	if maybe.Equal(largestHandledKey, rangeEnd, bytes.Equal) {
		return maybe.Nothing[[]byte](), nil
	}

	// The largest handled key isn't equal to the end of the range.
	// Find the start of the next key range to fetch.
	// Note that [largestHandledKey] can't be Nothing.
	// Proof: Suppose it is. That means that we got a range/change proof that proved up to the
	// greatest key-value pair in the database. That means we requested a proof with no upper
	// bound. That is, [workItem.end] is Nothing. Since we're here, [bothNothing] is false,
	// which means [workItem.end] isn't Nothing. Contradiction.
	lastReceivedKey := largestHandledKey.Value()

	if len(endProof) == 0 {
		// We try to find the next key to fetch by looking at the end proof.
		// If the end proof is empty, we have no information to use.
		// Start fetching from the next key after [lastReceivedKey].
		nextKey := lastReceivedKey
		nextKey = append(nextKey, 0)
		return maybe.Some(nextKey), nil
	}

	// We want the first key larger than the [lastReceivedKey].
	// This is done by taking two proofs for the same key
	// (one that was just received as part of a proof, and one from the local db)
	// and traversing them from the longest key to the shortest key.
	// For each node in these proofs, compare if the children of that node exist
	// or have the same ID in the other proof.
	proofKeyPath := ToKey(lastReceivedKey)

	// If the received proof is an exclusion proof, the last node may be for a
	// key that is after the [lastReceivedKey].
	// If the last received node's key is after the [lastReceivedKey], it can
	// be removed to obtain a valid proof for a prefix of the [lastReceivedKey].
	if !proofKeyPath.HasPrefix(endProof[len(endProof)-1].Key) {
		endProof = endProof[:len(endProof)-1]
		// update the proofKeyPath to be for the prefix
		proofKeyPath = endProof[len(endProof)-1].Key
	}

	// get a proof for the same key as the received proof from the local db
	localProofOfKey, err := getProof(db, lastReceivedKey)
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}
	localProofNodes := localProofOfKey.Path

	// The local proof may also be an exclusion proof with an extra node.
	// Remove this extra node if it exists to get a proof of the same key as the received proof
	if !proofKeyPath.HasPrefix(localProofNodes[len(localProofNodes)-1].Key) {
		localProofNodes = localProofNodes[:len(localProofNodes)-1]
	}

	nextKey := maybe.Nothing[[]byte]()

	// Add sentinel node back into the localProofNodes, if it is missing.
	// Required to ensure that a common node exists in both proofs
	if len(localProofNodes) > 0 && localProofNodes[0].Key.Length() != 0 {
		sentinel := ProofNode{
			Children: map[byte]ids.ID{
				localProofNodes[0].Key.Token(0, db.tokenSize): ids.Empty,
			},
		}
		localProofNodes = append([]ProofNode{sentinel}, localProofNodes...)
	}

	// Add sentinel node back into the endProof, if it is missing.
	// Required to ensure that a common node exists in both proofs
	if len(endProof) > 0 && endProof[0].Key.Length() != 0 {
		sentinel := ProofNode{
			Children: map[byte]ids.ID{
				endProof[0].Key.Token(0, db.tokenSize): ids.Empty,
			},
		}
		endProof = append([]ProofNode{sentinel}, endProof...)
	}

	localProofNodeIndex := len(localProofNodes) - 1
	receivedProofNodeIndex := len(endProof) - 1

	// traverse the two proofs from the deepest nodes up to the sentinel node until a difference is found
	for localProofNodeIndex >= 0 && receivedProofNodeIndex >= 0 && nextKey.IsNothing() {
		localProofNode := localProofNodes[localProofNodeIndex]
		receivedProofNode := endProof[receivedProofNodeIndex]

		// [deepestNode] is the proof node with the longest key (deepest in the trie) in the
		// two proofs that hasn't been handled yet.
		// [deepestNodeFromOtherProof] is the proof node from the other proof with
		// the same key/depth if it exists, nil otherwise.
		var deepestNode, deepestNodeFromOtherProof *ProofNode

		// select the deepest proof node from the two proofs
		switch {
		case receivedProofNode.Key.Length() > localProofNode.Key.Length():
			// there was a branch node in the received proof that isn't in the local proof
			// see if the received proof node has children not present in the local proof
			deepestNode = &receivedProofNode

			// we have dealt with this received node, so move on to the next received node
			receivedProofNodeIndex--

		case localProofNode.Key.Length() > receivedProofNode.Key.Length():
			// there was a branch node in the local proof that isn't in the received proof
			// see if the local proof node has children not present in the received proof
			deepestNode = &localProofNode

			// we have dealt with this local node, so move on to the next local node
			localProofNodeIndex--

		default:
			// the two nodes are at the same depth
			// see if any of the children present in the local proof node are different
			// from the children in the received proof node
			deepestNode = &localProofNode
			deepestNodeFromOtherProof = &receivedProofNode

			// we have dealt with this local node and received node, so move on to the next nodes
			localProofNodeIndex--
			receivedProofNodeIndex--
		}

		// We only want to look at the children with keys greater than the proofKey.
		// The proof key has the deepest node's key as a prefix,
		// so only the next token of the proof key needs to be considered.

		// If the deepest node has the same key as [proofKeyPath],
		// then all of its children have keys greater than the proof key,
		// so we can start at the 0 token.
		startingChildToken := 0

		// If the deepest node has a key shorter than the key being proven,
		// we can look at the next token index of the proof key to determine which of that
		// node's children have keys larger than [proofKeyPath].
		// Any child with a token greater than the [proofKeyPath]'s token at that
		// index will have a larger key.
		if deepestNode.Key.Length() < proofKeyPath.Length() {
			startingChildToken = int(proofKeyPath.Token(deepestNode.Key.Length(), db.tokenSize)) + 1
		}

		// determine if there are any differences in the children for the deepest unhandled node of the two proofs
		if childIndex, hasDifference := findChildDifference(deepestNode, deepestNodeFromOtherProof, startingChildToken); hasDifference {
			nextKey = maybe.Some(deepestNode.Key.Extend(ToToken(childIndex, db.tokenSize)).Bytes())
			break
		}
	}

	// If the nextKey is before or equal to the [lastReceivedKey]
	// then we couldn't find a better answer than the [lastReceivedKey].
	// Set the nextKey to [lastReceivedKey] + 0, which is the first key in
	// the open range (lastReceivedKey, rangeEnd).
	if nextKey.HasValue() && bytes.Compare(nextKey.Value(), lastReceivedKey) <= 0 {
		nextKeyVal := slices.Clone(lastReceivedKey)
		nextKeyVal = append(nextKeyVal, 0)
		nextKey = maybe.Some(nextKeyVal)
	}

	// If the [nextKey] is larger than the end of the range, return Nothing to signal that there is no next key in range
	if rangeEnd.HasValue() && bytes.Compare(nextKey.Value(), rangeEnd.Value()) >= 0 {
		return maybe.Nothing[[]byte](), nil
	}

	// the nextKey is within the open range (lastReceivedKey, rangeEnd), so return it
	return nextKey, nil
}

// findChildDifference returns the first child index that is different between node 1 and node 2 if one exists and
// a bool indicating if any difference was found
func findChildDifference(node1, node2 *ProofNode, startIndex int) (byte, bool) {
	// Children indices >= [startIndex] present in at least one of the nodes.
	childIndices := set.Set[byte]{}
	for _, node := range []*ProofNode{node1, node2} {
		if node == nil {
			continue
		}
		for key := range node.Children {
			if int(key) >= startIndex {
				childIndices.Add(key)
			}
		}
	}

	sortedChildIndices := maps.Keys(childIndices)
	slices.Sort(sortedChildIndices)
	var (
		child1, child2 ids.ID
		ok1, ok2       bool
	)
	for _, childIndex := range sortedChildIndices {
		if node1 != nil {
			child1, ok1 = node1.Children[childIndex]
		}
		if node2 != nil {
			child2, ok2 = node2.Children[childIndex]
		}
		// if one node has a child and the other doesn't or the children ids don't match,
		// return the current child index as the first difference
		if (ok1 || ok2) && child1 != child2 {
			return childIndex, true
		}
	}
	// there were no differences found
	return 0, false
}
