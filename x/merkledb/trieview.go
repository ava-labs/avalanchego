// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	defaultPreallocationSize         = 100
	minNodeCountForConcurrentHashing = 500
	initialProofPathSize             = 16
)

var (
	ErrCommitted       = errors.New("view has been committed")
	ErrChangedBaseRoot = errors.New("the trie this view was based on has changed its root")
	ErrEditLocked      = errors.New(
		"view has been edit locked. Any view generated from this view would be corrupted by edits",
	)
	ErrOddLengthWithValue = errors.New(
		"the underlying db only supports whole number of byte keys, so cannot record changes with odd nibble length",
	)
	ErrGetClosestNodeFailure = errors.New("GetClosestNode failed to return the closest node")
	ErrStartAfterEnd         = errors.New("start key > end key")

	_ TrieView = &trieView{}

	numCPU = runtime.NumCPU()
)

// Editable view of a trie, collects changes on top of a base trie.
// Delays adding key/value pairs to the trie.
type trieView struct {
	// Must be held when reading/writing fields.
	lock sync.Mutex

	// Changes made to this view.
	// May include nodes that haven't been updated
	// but will when their ID is recalculated.
	changes *changeSummary

	// Key/value pairs that have been inserted/removed but not
	// yet reflected in the trie's structure. This allows us to
	// defer the cost of updating the trie until we calculate node IDs.
	// A Nothing value indicates that the key has been removed.
	unappliedValueChanges map[path]Maybe[[]byte]

	// The trie below this one in the current view stack.
	// This is either [baseView] or [db].
	// Used to get information missing from the local view.
	baseTrie Trie

	// The root of [db] when this view was created.
	basedOnRoot ids.ID
	db          *Database

	// the view that this view is based upon (if it exists, nil otherwise).
	// If non-nil, is [baseTrie].
	baseView *trieView

	// The root of the trie represented by this view.
	root *node

	// True if the IDs of nodes in this view need to be recalculated.
	needsRecalculation bool

	// If true, this view has been committed and cannot be edited.
	// Calls to Insert and Remove will return ErrCommitted.
	committed bool

	// If true, this view has been edit locked because another view
	// exists atop it.
	// Calls to Insert and Remove will return ErrEditLocked.
	changeLocked  bool
	estimatedSize int
}

// Returns a new view on top of this one.
// Assumes this view stack is unlocked.
func (t *trieView) NewView(ctx context.Context) (TrieView, error) {
	return t.NewPreallocatedView(ctx, defaultPreallocationSize)
}

// Returns a new view on top of this one with memory allocated to store the
// [estimatedChanges] number of key/value changes.
// Assumes this view stack is unlocked.
func (t *trieView) NewPreallocatedView(
	ctx context.Context,
	estimatedChanges int,
) (TrieView, error) {
	t.lockStack()
	defer t.unlockStack()

	return newTrieView(ctx, t.db, t, nil, estimatedChanges)
}

// Creates a new view atop the given [baseView].
// If [baseView] is nil, the view is created atop [db].
// If [baseView] isn't nil, sets [baseView.changeLocked] to true.
// If [changes] is nil, a new changeSummary is created.
// Assumes [db.lock] is read locked.
// Assumes [baseView] is nil or locked.
func newTrieView(
	ctx context.Context,
	db *Database,
	baseView *trieView,
	changes *changeSummary,
	estimatedSize int,
) (*trieView, error) {
	if changes == nil {
		changes = newChangeSummary(estimatedSize)
	}

	baseTrie := Trie(db)
	if baseView != nil {
		baseTrie = baseView
		baseView.changeLocked = true
	}

	baseRoot := db.getMerkleRoot()

	result := &trieView{
		db:                    db,
		baseView:              baseView,
		baseTrie:              baseTrie,
		basedOnRoot:           baseRoot,
		changes:               changes,
		estimatedSize:         estimatedSize,
		unappliedValueChanges: make(map[path]Maybe[[]byte], estimatedSize),
	}
	var err error
	result.root, err = result.getNodeWithID(ctx, ids.Empty, RootPath)
	return result, err
}

// Write locks this view and read locks all views/the database below it.
func (t *trieView) lockStack() {
	t.lock.Lock()
	t.baseTrie.lockStack()
}

func (t *trieView) unlockStack() {
	t.baseTrie.unlockStack()
	t.lock.Unlock()
}

// Calculates the IDs of all nodes in this trie.
func (t *trieView) CalculateIDs(ctx context.Context) error {
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.CalculateIDs")
	defer span.End()

	t.lockStack()
	defer t.unlockStack()

	return t.calculateIDs(ctx)
}

// Recalculates the node IDs for all changed nodes in the trie.
// Assumes this view stack is locked.
func (t *trieView) calculateIDs(ctx context.Context) error {
	if !t.needsRecalculation {
		return nil
	}
	if t.committed {
		// Note that this should never happen. If a view is committed, it should
		// never be edited, so [t.needsRecalculation] should always be false.
		return ErrCommitted
	}

	// We wait to create the span until after checking that we need to actually
	// calculateIDs to make traces more useful (otherwise there may be a span
	// per key modified even though IDs are not re-calculated).
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.calculateIDs")
	defer span.End()

	// ensure that the view under this one is up to date before potentially pulling in nodes from it
	if t.baseView != nil {
		if err := t.baseView.calculateIDs(ctx); err != nil {
			return err
		}
	}

	if err := t.applyChangedValuesToTrie(ctx); err != nil {
		return err
	}

	_, helperSpan := t.db.tracer.Start(ctx, "MerkleDB.trieview.calculateIDsHelper")
	defer helperSpan.End()

	// [eg] limits the number of goroutines we start.
	var eg errgroup.Group
	eg.SetLimit(numCPU)
	if err := t.calculateIDsHelper(ctx, t.root, &eg); err != nil {
		return err
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	t.needsRecalculation = false
	return nil
}

// Calculates the ID of all descendants of [n] which need to be recalculated,
// and then calculates the ID of [n] itself.
func (t *trieView) calculateIDsHelper(ctx context.Context, n *node, eg *errgroup.Group) error {
	var (
		// We use [wg] to wait until all descendants of [n] have been updated.
		// Note we can't wait on [eg] because [eg] may have started goroutines
		// that aren't calculating IDs for descendants of [n].
		wg              sync.WaitGroup
		updatedChildren = make(chan *node, len(n.children))
	)

	for childIndex, child := range n.children {
		childIndex, child := childIndex, child

		childPath := n.key + path(childIndex) + child.compressedPath
		childNodeChange, ok := t.changes.nodes[childPath]
		if !ok {
			// This child wasn't changed.
			continue
		}

		wg.Add(1)
		updateChild := func() error {
			defer wg.Done()

			if err := t.calculateIDsHelper(ctx, childNodeChange.after, eg); err != nil {
				return err
			}

			// Note that this will never block
			updatedChildren <- childNodeChange.after
			return nil
		}

		// Try updating the child and its descendants in a goroutine.
		if ok := eg.TryGo(updateChild); !ok {
			// We're at the goroutine limit; do the work in this goroutine.
			if err := updateChild(); err != nil {
				return err
			}
		}
	}

	// Wait until all descendants of [n] have been updated.
	wg.Wait()
	close(updatedChildren)

	for child := range updatedChildren {
		n.addChild(child)
	}

	// The IDs [n]'s descendants are up to date so we can calculate [n]'s ID.
	return n.calculateID(t.db.metrics)
}

// Returns a proof that [bytesPath] is in or not in trie [t].
func (t *trieView) GetProof(ctx context.Context, key []byte) (*Proof, error) {
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.GetProof")
	defer span.End()

	t.lockStack()
	defer t.unlockStack()

	if err := t.calculateIDs(ctx); err != nil {
		return nil, err
	}
	return t.getProof(ctx, key)
}

// Returns a proof that [bytesPath] is in or not in trie [t].
// Assumes this view stack is locked.
func (t *trieView) getProof(ctx context.Context, key []byte) (*Proof, error) {
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.getProof")
	defer span.End()

	proof := &Proof{
		Key: key,
	}

	// Get the node at the given path, or the node closest to it.
	keyPath := newPath(key)

	proofPath, err := t.getPathTo(ctx, keyPath)
	if err != nil {
		return nil, err
	}

	// From root --> node from left --> right.
	proof.Path = make([]ProofNode, len(proofPath), len(proofPath)+1)
	for i, node := range proofPath {
		proof.Path[i] = node.asProofNode()
	}

	closestNode := proofPath[len(proofPath)-1]

	if closestNode.key.Compare(keyPath) == 0 {
		// There is a node with the given [key].
		proof.Value = closestNode.value
		return proof, nil
	}

	// There is no node with the given [key].
	// If there is a child at the index where the node would be
	// if it existed, include that child in the proof.
	nextIndex := keyPath[len(closestNode.key)]
	child, ok := closestNode.children[nextIndex]
	if !ok {
		return proof, nil
	}

	childPath := closestNode.key + path(nextIndex) + child.compressedPath
	childNode, err := t.getNodeFromParent(ctx, closestNode, childPath)
	if err != nil {
		return nil, err
	}
	proof.Path = append(proof.Path, childNode.asProofNode())
	return proof, nil
}

// Returns a range proof for (at least part of) the key range [start, end].
// The returned proof's [KeyValues] has at most [maxLength] values.
// [maxLength] must be > 0.
func (t *trieView) GetRangeProof(
	ctx context.Context,
	start, end []byte,
	maxLength int,
) (*RangeProof, error) {
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.GetRangeProof")
	defer span.End()

	t.lockStack()
	defer t.unlockStack()

	return t.getRangeProof(ctx, start, end, maxLength)
}

// Returns a range proof for (at least part of) the key range [start, end].
// The returned proof's [KeyValues] has at most [maxLength] values.
// [maxLength] must be > 0.
// Assumes this view stack is locked.
func (t *trieView) getRangeProof(
	ctx context.Context,
	start, end []byte,
	maxLength int,
) (*RangeProof, error) {
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.getRangeProof")
	defer span.End()

	if len(end) > 0 && bytes.Compare(start, end) == 1 {
		return nil, ErrStartAfterEnd
	}

	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	if err := t.calculateIDs(ctx); err != nil {
		return nil, err
	}

	var (
		result RangeProof
		err    error
	)

	result.KeyValues, err = t.getKeyValues(ctx, start, end, maxLength, set.Set[string]{})
	if err != nil {
		return nil, err
	}

	// This proof may not contain all key-value pairs in [start, end] due to size limitations.
	// The end proof we provide should be for the last key-value pair in the proof, not for
	// the last key-value pair requested, which may not be in this proof.
	if len(result.KeyValues) > 0 {
		end = result.KeyValues[len(result.KeyValues)-1].Key
	}

	if len(end) > 0 {
		endProof, err := t.getProof(ctx, end)
		if err != nil {
			return nil, err
		}
		result.EndProof = endProof.Path
	}

	if len(start) > 0 {
		startProof, err := t.getProof(ctx, start)
		if err != nil {
			return nil, err
		}
		result.StartProof = startProof.Path

		// strip out any common nodes to reduce proof size
		i := 0
		for ; i < len(result.StartProof) &&
			i < len(result.EndProof) &&
			result.StartProof[i].KeyPath.Equal(result.EndProof[i].KeyPath); i++ {
		}
		result.StartProof = result.StartProof[i:]
	}

	if len(result.StartProof) == 0 && len(result.EndProof) == 0 && len(result.KeyValues) == 0 {
		// If the range is empty, return the root proof.
		rootProof, err := t.getProof(ctx, rootKey)
		if err != nil {
			return nil, err
		}
		result.EndProof = rootProof.Path
	}

	return &result, nil
}

// Removes from the view stack views that have been committed or whose
// changes are already in the database.
// Returns true if [t]'s changes are already in the database.
// Assumes this view stack is locked.
func (t *trieView) cleanupCommittedViews(ctx context.Context) (bool, error) {
	if t.committed {
		return true, nil
	}

	root, err := t.getMerkleRoot(ctx)
	if err != nil {
		return false, err
	}

	if root == t.db.getMerkleRoot() {
		// this view's root matches the db's root, so the changes in it are already in the db.
		t.markViewStackCommitted()
		return true, nil
	}

	if t.baseView == nil {
		// There are no views under this one so we're done cleaning the view stack.
		return false, nil
	}

	inDatabase, err := t.baseView.cleanupCommittedViews(ctx)
	if err != nil {
		return false, err
	}
	if !inDatabase {
		// [t.baseView]'s changes aren't in the database yet
		// so we can't remove our reference to it.
		return false, nil
	}

	// [t.baseView]'s changes are in the database, so we can remove our reference to it.
	// We don't need to commit it to the database.
	t.baseView = nil
	// There's no view under this one, so we should read/write changes to the database.
	t.baseTrie = t.db
	return false, nil
}

// Commits changes from this trie to the underlying DB.
func (t *trieView) Commit(ctx context.Context) error {
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.Commit")
	defer span.End()

	t.lock.Lock()
	defer t.lock.Unlock()

	t.db.lock.Lock()
	defer t.db.lock.Unlock()

	// Note that we don't call lockStack() here because that would grab
	// [t.db]'s read lock, but we want its write lock because we're going
	// to modify [t.db]. No other view's call to lockStack() can proceed
	// until this method returns because we hold [t.db]'s write lock.

	if err := t.validateDBRoot(ctx); err != nil {
		return err
	}

	return t.commit(ctx)
}

// Commits the changes from this trie to the underlying DB.
// Assumes [t.lock] and [t.db.lock] are held.
func (t *trieView) commit(ctx context.Context) error {
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.triview.commit", oteltrace.WithAttributes(
		attribute.Int("changeCount", len(t.changes.values)),
	))
	defer span.End()

	if t.committed {
		return ErrCommitted
	}

	if err := t.calculateIDs(ctx); err != nil {
		return err
	}

	// ensure we don't recommit any committed tries
	if alreadyCommitted, err := t.cleanupCommittedViews(ctx); alreadyCommitted || err != nil {
		return err
	}

	// commit [t.baseView] before committing the current view
	if t.baseView != nil {
		// We have [db.lock] here so [t.baseView] can't be changing.
		if err := t.baseView.commit(ctx); err != nil {
			return err
		}
		t.baseView = nil
		t.baseTrie = t.db
	}

	if err := t.db.commitChanges(ctx, t.changes); err != nil {
		return err
	}
	t.committed = true
	return nil
}

// Returns the ID of the root of this trie.
func (t *trieView) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	t.lockStack()
	defer t.unlockStack()

	return t.getMerkleRoot(ctx)
}

// Returns the ID of the root node of this trie.
// Assumes this view stack is locked.
func (t *trieView) getMerkleRoot(ctx context.Context) (ids.ID, error) {
	if err := t.calculateIDs(ctx); err != nil {
		return ids.Empty, err
	}
	return t.root.id, nil
}

// Returns up to [maxLength] key/values from keys in closed range [start, end].
// Acts similarly to the merge step of a merge sort to combine state from the view
// with state from the base trie.
// Assumes this view stack is locked.
func (t *trieView) getKeyValues(
	ctx context.Context,
	start []byte,
	end []byte,
	maxLength int,
	keysToIgnore set.Set[string],
) ([]KeyValue, error) {
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieView.getKeyValues")
	defer span.End()

	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	// collect all values that have changed or been deleted
	changes := make([]KeyValue, 0, len(t.changes.values))
	for key, change := range t.changes.values {
		if change.after.IsNothing() {
			// This was deleted
			keysToIgnore.Add(string(key.Serialize().Value))
		} else {
			changes = append(changes, KeyValue{
				Key:   key.Serialize().Value,
				Value: change.after.value,
			})
		}
	}
	// sort [changes] so they can be merged with the base trie's state
	slices.SortFunc(changes, func(a, b KeyValue) bool {
		return bytes.Compare(a.Key, b.Key) == -1
	})

	baseKeyValues, err := t.baseTrie.getKeyValues(ctx, start, end, maxLength, keysToIgnore)
	if err != nil {
		return nil, err
	}

	var (
		// True if there are more key/value pairs from [baseKeyValues] to add to result
		baseKeyValuesFinished = false
		// True if there are more key/value pairs from [changes] to add to result
		changesFinished = false
		// The index of the next key/value pair to add from [baseKeyValues].
		baseKeyValuesIndex = 0
		// The index of the next key/value pair to add from [changes].
		changesIndex    = 0
		remainingLength = maxLength
		hasUpperBound   = len(end) > 0
		result          = make([]KeyValue, 0, len(baseKeyValues))
	)

	// keep adding key/value pairs until one of the following:
	// * a key that is lexicographically larger than the end key is hit
	// * the maxLength is hit
	// * no more values are available to add
	for remainingLength > 0 {
		// the baseKeyValues iterator is finished when we have run out of keys or hit a key greater than the end key
		baseKeyValuesFinished = baseKeyValuesFinished ||
			(baseKeyValuesIndex >= len(baseKeyValues) || (hasUpperBound && bytes.Compare(baseKeyValues[baseKeyValuesIndex].Key, end) == 1))

		// the changes iterator is finished when we have run out of keys or hit a key greater than the end key
		changesFinished = changesFinished ||
			(changesIndex >= len(changes) || (hasUpperBound && bytes.Compare(changes[changesIndex].Key, end) == 1))

		// if both the base state and changes are finished, return the result of the merge
		if baseKeyValuesFinished && changesFinished {
			return result, nil
		}

		// one or both iterators still have values, so one will be added to the result
		remainingLength--

		// both still have key/values available, so add the smallest key
		if !changesFinished && !baseKeyValuesFinished {
			currentChangeState := changes[changesIndex]
			currentKeyValues := baseKeyValues[baseKeyValuesIndex]

			switch bytes.Compare(currentChangeState.Key, currentKeyValues.Key) {
			case -1:
				result = append(result, currentChangeState)
				changesIndex++
			case 0:
				// the keys are the same, so override the base value with the changed value
				result = append(result, currentChangeState)
				changesIndex++
				baseKeyValuesIndex++
			case 1:
				result = append(result, currentKeyValues)
				baseKeyValuesIndex++
			}
			continue
		}

		// the base state is not finished, but the changes is finished.
		// add the next base state value.
		if !baseKeyValuesFinished {
			currentBaseState := baseKeyValues[baseKeyValuesIndex]
			result = append(result, currentBaseState)
			baseKeyValuesIndex++
			continue
		}

		// the base state is finished, but the changes is not finished.
		// add the next changes value.
		currentChangeState := changes[changesIndex]
		result = append(result, currentChangeState)
		changesIndex++
	}

	return result, nil
}

func (t *trieView) GetValues(ctx context.Context, keys [][]byte) ([][]byte, []error) {
	t.lockStack()
	defer t.unlockStack()

	results := make([][]byte, len(keys))
	errors := make([]error, len(keys))

	if err := t.validateDBRoot(ctx); err != nil {
		for i := range keys {
			errors[i] = err
		}
		return results, errors
	}

	for i, key := range keys {
		results[i], errors[i] = t.getValue(ctx, newPath(key))
	}
	return results, errors
}

// Returns the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
func (t *trieView) GetValue(ctx context.Context, key []byte) ([]byte, error) {
	t.lockStack()
	defer t.unlockStack()

	if err := t.validateDBRoot(ctx); err != nil {
		return nil, err
	}
	return t.getValue(ctx, newPath(key))
}

// Assumes this view stack is locked.
func (t *trieView) getValue(ctx context.Context, key path) ([]byte, error) {
	if change, ok := t.changes.values[key]; ok {
		t.db.metrics.ViewValueCacheHit()
		if change.after.IsNothing() {
			return nil, database.ErrNotFound
		}
		return change.after.value, nil
	}
	t.db.metrics.ViewValueCacheMiss()

	// if we don't have local copy of the key, then grab a copy from the base trie
	value, err := t.baseTrie.getValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return value, nil
}

// Upserts the key/value pair into the trie.
func (t *trieView) Insert(ctx context.Context, key []byte, value []byte) error {
	t.lockStack()
	defer t.unlockStack()

	return t.insert(ctx, key, value)
}

// Assumes this view stack is locked.
func (t *trieView) insert(ctx context.Context, key []byte, value []byte) error {
	if t.committed {
		return ErrCommitted
	}
	if t.changeLocked {
		return ErrEditLocked
	}
	valCopy := slices.Clone(value)
	return t.recordValueChange(ctx, newPath(key), Some(valCopy))
}

// Removes the value associated with [key] from this trie.
func (t *trieView) Remove(ctx context.Context, key []byte) error {
	t.lockStack()
	defer t.unlockStack()

	return t.remove(ctx, key)
}

// Assumes this view stack is locked.
func (t *trieView) remove(ctx context.Context, key []byte) error {
	if t.committed {
		return ErrCommitted
	}

	if t.changeLocked {
		return ErrEditLocked
	}

	return t.recordValueChange(ctx, newPath(key), Nothing[[]byte]())
}

// Returns nil iff at least one of the following is true:
//   - The root of the db hasn't changed since this view was created.
//   - This view's root is the same as the db's root.
//   - This method returns nil for the view under this one.
//
// Assumes this view stack is locked.
func (t *trieView) validateDBRoot(ctx context.Context) error {
	dbRoot := t.db.getMerkleRoot()

	// the root has not changed, so the trieview is still valid
	if dbRoot == t.basedOnRoot {
		return nil
	}

	if t.baseView != nil {
		// if the view that this view is based on is valid,
		// then this view is valid too.
		if err := t.baseView.validateDBRoot(ctx); err == nil {
			return nil
		}
	}

	// this view has no base view or an invalid base view.
	// calculate the current view's root and check if it matches the db.
	localRoot, err := t.getMerkleRoot(ctx)
	if err != nil {
		return err
	}

	// the roots don't match, which means that that the changes
	// in this view aren't already represented in the db
	if localRoot != dbRoot {
		return ErrChangedBaseRoot
	}

	return nil
}

// Assumes this view stack is locked.
func (t *trieView) applyChangedValuesToTrie(ctx context.Context) error {
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.applyChangedValuesToTrie")
	defer span.End()

	unappliedValues := t.unappliedValueChanges
	t.unappliedValueChanges = make(map[path]Maybe[[]byte], t.estimatedSize)

	for key, change := range unappliedValues {
		if change.IsNothing() {
			if err := t.removeFromTrie(ctx, key); err != nil {
				return err
			}
		} else {
			if _, err := t.insertIntoTrie(ctx, key, change); err != nil {
				return err
			}
		}
	}
	return nil
}

// Merges together nodes in the inclusive descendants of [node] that
// have no value and a single child into one node with a compressed
// path until a node that doesn't meet those criteria is reached.
// [parent] is [node]'s parent.
// Assumes at least one of the following is true:
// * [node] has a value.
// * [node] has children.
// Assumes this view stack is locked.
func (t *trieView) compressNodePath(ctx context.Context, parent, node *node) error {
	// don't collapse into this node if it's the root, doesn't have 1 child, or has a value
	if len(node.children) != 1 || node.hasValue() {
		return nil
	}

	// delete all empty nodes with a single child under [node]
	for len(node.children) == 1 && !node.hasValue() {
		if err := t.recordNodeDeleted(ctx, node); err != nil {
			return err
		}

		nextNode, err := t.getNodeFromParent(ctx, node, node.getSingleChildPath())
		if err != nil {
			return err
		}
		node = nextNode
	}

	// [node] is the first node with multiple children.
	// combine it with the [node] passed in.
	parent.addChild(node)
	return t.recordNodeChange(ctx, parent)
}

// Starting from the last node in [nodePath], traverses toward the root
// and deletes each node that has no value and no children.
// Stops when a node with a value or children is reached.
// Assumes [nodePath] is a path from the root to a node.
// Assumes this view stack is locked.
func (t *trieView) deleteEmptyNodes(ctx context.Context, nodePath []*node) error {
	node := nodePath[len(nodePath)-1]
	nextParentIndex := len(nodePath) - 2

	for ; nextParentIndex >= 0 && len(node.children) == 0 && !node.hasValue(); nextParentIndex-- {
		if err := t.recordNodeDeleted(ctx, node); err != nil {
			return err
		}

		parent := nodePath[nextParentIndex]

		parent.removeChild(node)
		if err := t.recordNodeChange(ctx, parent); err != nil {
			return err
		}

		node = parent
	}

	if nextParentIndex < 0 {
		return nil
	}
	parent := nodePath[nextParentIndex]

	return t.compressNodePath(ctx, parent, node)
}

// Returns the nodes along the path to [key].
// The first node is the root, and the last node is either the node with the
// given [key], if it's in the trie, or the node with the largest prefix of
// the [key] if it isn't in the trie.
// Always returns at least the root node.
func (t *trieView) getPathTo(ctx context.Context, key path) ([]*node, error) {
	var (
		// all paths start at the root
		currentNode     = t.root
		matchedKeyIndex = 0
		nodes           = []*node{t.root}
	)

	// while the entire path hasn't been matched
	for matchedKeyIndex < len(key) {
		// confirm that a child exists and grab its ID before attempting to load it
		nextChildEntry, hasChild := currentNode.children[key[matchedKeyIndex]]

		// the nibble for the child entry has now been handled, so increment the matchedPathIndex
		matchedKeyIndex += 1

		if !hasChild || !key[matchedKeyIndex:].HasPrefix(nextChildEntry.compressedPath) {
			// there was no child along the path or the child that was there doesn't match the remaining path
			return nodes, nil
		}

		// the compressed path of the entry there matched the path, so increment the matched index
		matchedKeyIndex += len(nextChildEntry.compressedPath)

		// grab the next node along the path
		var err error
		currentNode, err = t.getNodeWithID(ctx, nextChildEntry.id, key[:matchedKeyIndex])
		if err != nil {
			return nil, err
		}

		// add node to path
		nodes = append(nodes, currentNode)
	}
	return nodes, nil
}

func getLengthOfCommonPrefix(first, second path) int {
	commonIndex := 0
	for len(first) > commonIndex && len(second) > commonIndex && first[commonIndex] == second[commonIndex] {
		commonIndex++
	}
	return commonIndex
}

// Assumes this view stack is locked.
func (t *trieView) getNode(ctx context.Context, key path) (*node, error) {
	if err := t.calculateIDs(ctx); err != nil {
		return nil, err
	}

	n, err := t.getNodeWithID(ctx, ids.Empty, key)
	if err != nil {
		return nil, err
	}
	return n.clone(), nil
}

// Inserts a key/value pair into the trie.
// Assumes this view stack is locked.
func (t *trieView) insertIntoTrie(
	ctx context.Context,
	key path,
	value Maybe[[]byte],
) (*node, error) {
	// find the node that most closely matches [key]
	pathToNode, err := t.getPathTo(ctx, key)
	if err != nil {
		return nil, err
	}

	// We're inserting a node whose ancestry is [pathToNode]
	// so we'll need to recalculate their IDs.
	for _, node := range pathToNode {
		if err := t.recordNodeChange(ctx, node); err != nil {
			return nil, err
		}
	}

	closestNode := pathToNode[len(pathToNode)-1]

	// a node with that exact path already exists so update its value
	if closestNode.key.Compare(key) == 0 {
		closestNode.setValue(value)
		return closestNode, nil
	}

	closestNodeKeyLength := len(closestNode.key)
	// A node with the exact key doesn't exist so determine the portion of the
	// key that hasn't been matched yet
	// Note that [key] has prefix [closestNodeFullPath] but exactMatch was false,
	// so [key] must be longer than [closestNodeFullPath] and the following slice won't OOB.
	remainingKey := key[closestNodeKeyLength+1:]

	existingChildEntry, hasChild := closestNode.children[key[closestNodeKeyLength]]
	// there are no existing nodes along the path [fullPath], so create a new node to insert [value]
	if !hasChild {
		newNode := newNode(
			closestNode,
			key,
		)
		newNode.setValue(value)
		return newNode, t.recordNodeChange(ctx, newNode)
	} else if err != nil {
		return nil, err
	}

	// if we have reached this point, then the [fullpath] we are trying to insert and
	// the existing path node have some common prefix.
	// a new branching node will be created that will represent this common prefix and
	// have the existing path node and the value being inserted as children.

	// generate the new branch node
	branchNode := newNode(
		closestNode,
		key[:closestNodeKeyLength+1+getLengthOfCommonPrefix(existingChildEntry.compressedPath, remainingKey)],
	)
	if err := t.recordNodeChange(ctx, closestNode); err != nil {
		return nil, err
	}
	nodeWithValue := branchNode

	if len(key)-len(branchNode.key) == 0 {
		// there was no residual path for the inserted key, so the value goes directly into the new branch node
		branchNode.setValue(value)
	} else {
		// generate a new node and add it as a child of the branch node
		newNode := newNode(
			branchNode,
			key,
		)
		newNode.setValue(value)
		if err := t.recordNodeChange(ctx, newNode); err != nil {
			return nil, err
		}
		nodeWithValue = newNode
	}

	existingChildKey := key[:closestNodeKeyLength+1] + existingChildEntry.compressedPath

	// the existing child's key is of length: len(closestNodekey) + 1 for the child index + len(existing child's compressed key)
	// if that length is less than or equal to the branch node's key that implies that the existing child's key matched the key to be inserted
	// since it matched the key to be inserted, it should have been returned by getClosestNode
	if len(existingChildKey) <= len(branchNode.key) {
		return nil, ErrGetClosestNodeFailure
	}

	branchNode.addChildWithoutNode(
		existingChildKey[len(branchNode.key)],
		existingChildKey[len(branchNode.key)+1:],
		existingChildEntry.id,
	)

	return nodeWithValue, t.recordNodeChange(ctx, branchNode)
}

// Mark this view and all views under this view as committed.
// Assumes this view stack is locked.
func (t *trieView) markViewStackCommitted() {
	currentView := t
	for currentView != nil {
		currentView.committed = true
		currentView = currentView.baseView
	}
}

// Records that a node has been changed.
// Assumes this view stack is locked.
func (t *trieView) recordNodeChange(ctx context.Context, after *node) error {
	return t.recordKeyChange(ctx, after.key, after)
}

// Records that the node associated with the given key has been deleted.
// Assumes this view stack is locked.
func (t *trieView) recordNodeDeleted(ctx context.Context, after *node) error {
	// don't delete the root.
	if len(after.key) == 0 {
		return t.recordKeyChange(ctx, after.key, after)
	}
	return t.recordKeyChange(ctx, after.key, nil)
}

// Records that the node associated with the given key has been changed.
// Assumes this view stack is locked.
func (t *trieView) recordKeyChange(ctx context.Context, key path, after *node) error {
	t.needsRecalculation = true

	if existing, ok := t.changes.nodes[key]; ok {
		existing.after = after
		return nil
	}

	before, err := t.baseTrie.getNode(ctx, key)
	if err != nil {
		if err != database.ErrNotFound {
			return err
		}
		before = nil
	}

	t.changes.nodes[key] = &change[*node]{
		before: before,
		after:  after,
	}
	return nil
}

// Records that a key's value has been added or updated.
// Doesn't actually change the trie data structure.
// That's deferred until we calculate node IDs.
// Assumes this view stack is locked.
func (t *trieView) recordValueChange(ctx context.Context, key path, value Maybe[[]byte]) error {
	t.needsRecalculation = true

	// record the value change so that it can be inserted
	// into a trie nodes later
	t.unappliedValueChanges[key] = value

	// update the existing change if it exists
	if existing, ok := t.changes.values[key]; ok {
		existing.after = value
		return nil
	}

	// grab the before value
	var beforeMaybe Maybe[[]byte]
	before, err := t.baseTrie.getValue(ctx, key)
	switch err {
	case nil:
		beforeMaybe = Some(before)
	case database.ErrNotFound:
		beforeMaybe = Nothing[[]byte]()
	default:
		return err
	}

	t.changes.values[key] = &change[Maybe[[]byte]]{
		before: beforeMaybe,
		after:  value,
	}
	return nil
}

// Removes the provided [key] from the trie.
// Assumes this view stack is locked.
func (t *trieView) removeFromTrie(ctx context.Context, key path) error {
	nodePath, err := t.getPathTo(ctx, key)
	if err != nil {
		return err
	}

	nodeToDelete := nodePath[len(nodePath)-1]

	if nodeToDelete.key.Compare(key) != 0 || !nodeToDelete.hasValue() {
		// the key wasn't in the trie or doesn't have a value so there's nothing to do
		return nil
	}

	// A node with ancestry [nodePath] is being deleted, so we need to recalculate
	// all of the nodes in this path.
	for _, node := range nodePath {
		if err := t.recordNodeChange(ctx, node); err != nil {
			return err
		}
	}

	nodeToDelete.setValue(Nothing[[]byte]())
	if err := t.recordNodeChange(ctx, nodeToDelete); err != nil {
		return err
	}

	// if the removed node has no children, the node can be removed from the trie
	if len(nodeToDelete.children) == 0 {
		return t.deleteEmptyNodes(ctx, nodePath)
	}

	if len(nodePath) == 1 {
		return nil
	}
	parent := nodePath[len(nodePath)-2]

	// merge this node and its descendants into a single node if possible
	return t.compressNodePath(ctx, parent, nodeToDelete)
}

// Retrieves the node with the given [key], which is a child of [parent], and
// uses the [parent] node to initialize the child node's ID.
// Returns database.ErrNotFound if the child doesn't exist.
// Assumes this view stack is locked.
func (t *trieView) getNodeFromParent(ctx context.Context, parent *node, key path) (*node, error) {
	// confirm the child exists and get its ID before attempting to load it
	if child, exists := parent.children[key[len(parent.key)]]; exists {
		return t.getNodeWithID(ctx, child.id, key)
	}

	return nil, database.ErrNotFound
}

// Retrieves a node with the given [key].
// If the node is fetched from [t.baseTrie] and [id] isn't empty,
// sets the node's ID to [id].
// Returns database.ErrNotFound if the node doesn't exist.
// Assumes this view stack is locked.
func (t *trieView) getNodeWithID(ctx context.Context, id ids.ID, key path) (*node, error) {
	// check for the key within the changed nodes
	if nodeChange, isChanged := t.changes.nodes[key]; isChanged {
		t.db.metrics.ViewNodeCacheHit()
		if nodeChange.after == nil {
			return nil, database.ErrNotFound
		}
		return nodeChange.after, nil
	}

	// get the node from the base trie and store a localy copy
	baseTrieNode, err := t.baseTrie.getNode(ctx, key)
	if err != nil {
		return nil, err
	}

	// copy the node so any alterations to it don't affect the base trie
	node := baseTrieNode.clone()

	// only need to initialize the id if it's from the base trie.
	// nodes in the current view change list have already been initialized.
	if id != ids.Empty {
		node.id = id
	}
	return node, nil
}
