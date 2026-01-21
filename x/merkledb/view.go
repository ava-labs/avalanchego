// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"errors"
	"slices"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/maybe"

	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	initKeyValuesSize        = 256
	defaultPreallocationSize = 100
)

var (
	_ View = (*view)(nil)

	ErrCommitted                  = errors.New("view has been committed")
	ErrInvalid                    = errors.New("the trie this view was based on has changed, rendering this view invalid")
	ErrPartialByteLengthWithValue = errors.New(
		"the underlying db only supports whole number of byte keys, so cannot record changes with partial byte lengths",
	)
	ErrVisitPathToKey         = errors.New("failed to visit expected node during insertion")
	ErrStartAfterEnd          = errors.New("start key > end key")
	ErrNoChanges              = errors.New("no changes provided")
	ErrParentNotDatabase      = errors.New("parent trie is not database")
	ErrNodesAlreadyCalculated = errors.New("cannot modify the trie after the node changes have been calculated")
)

type view struct {
	// If true, this view has been committed.
	// [commitLock] must be held while accessing this field.
	committed  bool
	commitLock sync.RWMutex

	// valueChangesApplied is used to enforce that no changes are made to the
	// trie after the nodes have been calculated
	valueChangesApplied utils.Atomic[bool]

	// applyValueChangesOnce prevents node calculation from occurring multiple
	// times
	applyValueChangesOnce sync.Once

	// Controls the view's validity related fields.
	// Must be held while reading/writing [childViews], [invalidated], and [parentTrie].
	// Only use to lock current view or descendants of the current view
	// DO NOT grab the [validityTrackingLock] of any ancestor trie while this is held.
	validityTrackingLock sync.RWMutex

	// If true, this view has been invalidated and can't be used.
	//
	// Invariant: This view is marked as invalid before any of its ancestors change.
	// Since we ensure that all subviews are marked invalid before making an invalidating change
	// then if we are still valid at the end of the function, then no corrupting changes could have
	// occurred during execution.
	// Namely, if we have a method with:
	//
	// *Code Accessing Ancestor State*
	//
	// if v.isInvalid() {
	//     return ErrInvalid
	//  }
	// return [result]
	//
	// If the invalidated check passes, then we're guaranteed that no ancestor changes occurred
	// during the code that accessed ancestor state and the result of that work is still valid
	//
	// [validityTrackingLock] must be held when reading/writing this field.
	invalidated bool

	// the uncommitted parent trie of this view
	// [validityTrackingLock] must be held when reading/writing this field.
	parentTrie View

	// The valid children of this view.
	// [validityTrackingLock] must be held when reading/writing this field.
	childViews []*view

	// Changes made to this view.
	// May include nodes that haven't been updated
	// but will when their ID is recalculated.
	changes *changeSummary

	db *merkleDB

	// The root of the trie represented by this view.
	root maybe.Maybe[*node]

	tokenSize int
}

// NewView returns a new view on top of this view where the passed changes
// have been applied.
// Adds the new view to [v.childViews].
// Assumes [v.commitLock] isn't held.
func (v *view) NewView(
	ctx context.Context,
	changes ViewChanges,
) (View, error) {
	if v.isInvalid() {
		return nil, ErrInvalid
	}
	v.commitLock.RLock()
	defer v.commitLock.RUnlock()

	if v.committed {
		return v.getParentTrie().NewView(ctx, changes)
	}

	if err := v.applyValueChanges(ctx); err != nil {
		return nil, err
	}

	childView, err := newView(v.db, v, changes)
	if err != nil {
		return nil, err
	}

	v.validityTrackingLock.Lock()
	defer v.validityTrackingLock.Unlock()

	if v.invalidated {
		return nil, ErrInvalid
	}
	v.childViews = append(v.childViews, childView)

	return childView, nil
}

// Creates a new view with the given [parentTrie].
func newView(
	db *merkleDB,
	parentTrie View,
	changes ViewChanges,
) (*view, error) {
	v := &view{
		root:       maybe.Bind(parentTrie.getRoot(), (*node).clone),
		db:         db,
		parentTrie: parentTrie,
		changes:    newChangeSummary(len(changes.BatchOps) + len(changes.MapOps)),
		tokenSize:  db.tokenSize,
	}

	keyChanges := map[Key]*change[maybe.Maybe[[]byte]]{}

	for _, op := range changes.BatchOps {
		key := op.Key
		if !changes.ConsumeBytes {
			key = slices.Clone(op.Key)
		}

		newVal := maybe.Nothing[[]byte]()
		if !op.Delete {
			newVal = maybe.Some(op.Value)
			if !changes.ConsumeBytes {
				newVal = maybe.Some(slices.Clone(op.Value))
			}
		}

		if err := recordValueChange(v, keyChanges, toKey(key), newVal); err != nil {
			return nil, err
		}
	}

	for key, val := range changes.MapOps {
		if !changes.ConsumeBytes {
			val = maybe.Bind(val, slices.Clone[[]byte])
		}
		if err := recordValueChange(v, keyChanges, toKey(stringToByteSlice(key)), val); err != nil {
			return nil, err
		}
	}

	sortedKeys := maps.Keys(keyChanges)
	slices.SortFunc(sortedKeys, func(a, b Key) int {
		return a.Compare(b)
	})

	v.changes.keyChanges = keyChanges
	v.changes.sortedKeys = sortedKeys

	return v, nil
}

func recordValueChange(v *view, keyChanges map[Key]*change[maybe.Maybe[[]byte]], key Key, value maybe.Maybe[[]byte]) error {
	// update the existing change if it exists
	if existing, ok := keyChanges[key]; ok {
		existing.after = value
		return nil
	}

	// grab the before value
	var beforeMaybe maybe.Maybe[[]byte]
	before, err := v.getParentTrie().getValue(key)
	switch err {
	case nil:
		beforeMaybe = maybe.Some(before)
	case database.ErrNotFound:
		beforeMaybe = maybe.Nothing[[]byte]()
	default:
		return err
	}

	keyChanges[key] = &change[maybe.Maybe[[]byte]]{
		before: beforeMaybe,
		after:  value,
	}

	return nil
}

// Creates a view of the db at a historical root using the provided [changes].
// Returns ErrNoChanges if [changes] is empty.
func newViewWithChanges(
	db *merkleDB,
	changes *changeSummary,
) (*view, error) {
	if changes == nil {
		return nil, ErrNoChanges
	}

	v := &view{
		root:       changes.rootChange.after,
		db:         db,
		parentTrie: db,
		changes:    changes,
		tokenSize:  db.tokenSize,
	}
	// since this is a set of historical changes, all nodes have already been calculated
	// since no new changes have occurred, no new calculations need to be done
	v.applyValueChangesOnce.Do(func() {})
	v.valueChangesApplied.Set(true)
	return v, nil
}

func (v *view) getTokenSize() int {
	return v.tokenSize
}

func (v *view) getRoot() maybe.Maybe[*node] {
	return v.root
}

// applyValueChanges generates the node changes from the value changes. It then
// hashes the changed nodes to calculate the new trie.
//
// Cancelling [ctx] doesn't cancel the operation. It's used only for tracing.
func (v *view) applyValueChanges(ctx context.Context) error {
	var err error
	v.applyValueChangesOnce.Do(func() {
		// Create the span inside the once wrapper to make traces more useful.
		// Otherwise, spans would be created during calls where the IDs are not
		// re-calculated.
		ctx, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.applyValueChanges")
		defer span.End()

		if v.isInvalid() {
			err = ErrInvalid
			return
		}
		defer v.valueChangesApplied.Set(true)

		oldRoot := maybe.Bind(v.root, (*node).clone)

		// Note we're setting [err] defined outside this function.
		if err = v.calculateNodeChanges(ctx); err != nil {
			return
		}
		v.hashChangedNodes(ctx)

		v.changes.rootChange = change[maybe.Maybe[*node]]{
			before: oldRoot,
			after:  v.root,
		}

		// ensure no ancestor changes occurred during execution
		if v.isInvalid() {
			err = ErrInvalid
			return
		}
	})
	return err
}

func (v *view) calculateNodeChanges(ctx context.Context) error {
	_, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.calculateNodeChanges")
	defer span.End()

	// Add all the changed key/values to the nodes of the trie
	for key, keyChange := range v.changes.keyChanges {
		if keyChange.after.IsNothing() {
			if err := v.remove(key); err != nil {
				return err
			}
		} else if _, err := v.insert(key, keyChange.after); err != nil {
			return err
		}
	}

	return nil
}

func (v *view) hashChangedNodes(ctx context.Context) {
	_, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.hashChangedNodes")
	defer span.End()

	if v.root.IsNothing() {
		v.changes.rootID = ids.Empty
		return
	}

	// If there are no children, we can avoid allocating [keyBuffer].
	root := v.root.Value()
	if len(root.children) == 0 {
		v.changes.rootID = v.db.hasher.HashNode(root)
		v.db.metrics.HashCalculated()
		return
	}

	// Allocate [keyBuffer] and populate it with the root node's key.
	keyBuffer := v.db.hashNodesKeyPool.Acquire()
	keyBuffer = v.setKeyBuffer(root, keyBuffer)
	v.changes.rootID, keyBuffer = v.hashChangedNode(root, keyBuffer)
	v.db.hashNodesKeyPool.Release(keyBuffer)
}

// Calculates the ID of all descendants of [n] which need to be recalculated,
// and then calculates the ID of [n] itself.
//
// Returns a potentially expanded [keyBuffer]. By returning this value this
// function is able to have a maximum total number of allocations shared across
// multiple invocations.
//
// Invariant: [keyBuffer] must be populated with [n]'s key and have sufficient
// length to contain any of [n]'s child keys.
func (v *view) hashChangedNode(n *node, keyBuffer []byte) (ids.ID, []byte) {
	var (
		// childBuffer is allocated on the stack.
		childBuffer = make([]byte, 1)
		dualIndex   = dualBitIndex(v.tokenSize)
		bytesForKey = bytesNeeded(n.key.length)
		// We track the last byte of [n.key] so that we can reset the value for
		// each key. This is needed because the child buffer may get ORed at
		// this byte.
		lastKeyByte byte

		// We use [wg] to wait until all descendants of [n] have been updated.
		wg waitGroup
	)
	if bytesForKey > 0 {
		lastKeyByte = keyBuffer[bytesForKey-1]
	}

	// This loop is optimized to avoid allocations when calculating the
	// [childKey] by reusing [keyBuffer] and leaving the first [bytesForKey-1]
	// bytes unmodified.
	for childIndex, childEntry := range n.children {
		childBuffer[0] = childIndex << dualIndex
		childIndexAsKey := Key{
			// It is safe to use byteSliceToString because [childBuffer] is not
			// modified while [childIndexAsKey] is in use.
			value:  byteSliceToString(childBuffer),
			length: v.tokenSize,
		}

		totalBitLength := n.key.length + v.tokenSize + childEntry.compressedKey.length
		// Because [keyBuffer] may have been modified in a prior iteration of
		// this loop, it is not guaranteed that its length is at least
		// [bytesNeeded(totalBitLength)]. However, that's fine. The below
		// slicing would only panic if the buffer didn't have sufficient
		// capacity.
		keyBuffer = keyBuffer[:bytesNeeded(totalBitLength)]
		// We don't need to copy this node's key. It's assumed to already be
		// correct; except for the last byte. We must make sure the last byte of
		// the key is set correctly because extendIntoBuffer may OR bits from
		// the extension and overwrite the last byte. However, extendIntoBuffer
		// does not modify the first [bytesForKey-1] bytes of [keyBuffer].
		if bytesForKey > 0 {
			keyBuffer[bytesForKey-1] = lastKeyByte
		}
		extendIntoBuffer(keyBuffer, childIndexAsKey, n.key.length)
		extendIntoBuffer(keyBuffer, childEntry.compressedKey, n.key.length+v.tokenSize)
		childKey := Key{
			// It is safe to use byteSliceToString because [keyBuffer] is not
			// modified while [childKey] is in use.
			value:  byteSliceToString(keyBuffer),
			length: totalBitLength,
		}

		childNodeChange, ok := v.changes.nodes[childKey]
		if !ok {
			// This child wasn't changed.
			continue
		}

		childNode := childNodeChange.after
		childEntry.hasValue = childNode.hasValue()

		// If there are no children of the childNode, we can avoid constructing
		// the buffer for the child keys.
		if len(childNode.children) == 0 {
			childEntry.id = v.db.hasher.HashNode(childNode)
			v.db.metrics.HashCalculated()
			continue
		}

		// Try updating the child and its descendants in a goroutine.
		if childKeyBuffer, ok := v.db.hashNodesKeyPool.TryAcquire(); ok {
			wg.Add(1)
			go func(wg *sync.WaitGroup, childEntry *child, childNode *node, childKeyBuffer []byte) {
				childKeyBuffer = v.setKeyBuffer(childNode, childKeyBuffer)
				childEntry.id, childKeyBuffer = v.hashChangedNode(childNode, childKeyBuffer)
				v.db.hashNodesKeyPool.Release(childKeyBuffer)
				wg.Done()
			}(wg.wg, childEntry, childNode, childKeyBuffer)
		} else {
			// We're at the goroutine limit; do the work in this goroutine.
			//
			// We can skip copying the key here because [keyBuffer] is already
			// constructed to be childNode's key.
			keyBuffer = v.setLengthForChildren(childNode, keyBuffer)
			childEntry.id, keyBuffer = v.hashChangedNode(childNode, keyBuffer)
		}
	}

	// Wait until all descendants of [n] have been updated.
	wg.Wait()

	// The IDs [n]'s descendants are up to date so we can calculate [n]'s ID.
	v.db.metrics.HashCalculated()
	return v.db.hasher.HashNode(n), keyBuffer
}

// setKeyBuffer expands [keyBuffer] to have sufficient size for any of [n]'s
// child keys and populates [n]'s key into [keyBuffer]. If [keyBuffer] already
// has sufficient size, this function will not perform any memory allocations.
func (v *view) setKeyBuffer(n *node, keyBuffer []byte) []byte {
	keyBuffer = v.setLengthForChildren(n, keyBuffer)
	copy(keyBuffer, n.key.value)
	return keyBuffer
}

// setLengthForChildren expands [keyBuffer] to have sufficient size for any of
// [n]'s child keys.
func (v *view) setLengthForChildren(n *node, keyBuffer []byte) []byte {
	// Calculate the size of the largest child key of this node.
	var maxBitLength int
	for _, childEntry := range n.children {
		maxBitLength = max(maxBitLength, childEntry.compressedKey.length)
	}
	maxBytesNeeded := bytesNeeded(n.key.length + v.tokenSize + maxBitLength)
	return setBytesLength(keyBuffer, maxBytesNeeded)
}

func setBytesLength(b []byte, size int) []byte {
	if size <= cap(b) {
		return b[:size]
	}
	return append(b[:cap(b)], make([]byte, size-cap(b))...)
}

// GetProof returns a proof that [bytesPath] is in or not in trie [t].
func (v *view) GetProof(ctx context.Context, key []byte) (*Proof, error) {
	_, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.GetProof")
	defer span.End()

	if err := v.applyValueChanges(ctx); err != nil {
		return nil, err
	}

	result, err := getProof(v, key)
	if err != nil {
		return nil, err
	}
	if v.isInvalid() {
		return nil, ErrInvalid
	}
	return result, nil
}

// GetRangeProof returns a range proof for (at least part of) the key range [start, end].
// The returned proof's [KeyValues] has at most [maxLength] values.
// [maxLength] must be > 0.
func (v *view) GetRangeProof(
	ctx context.Context,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) (*RangeProof, error) {
	_, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.GetRangeProof")
	defer span.End()

	if err := v.applyValueChanges(ctx); err != nil {
		return nil, err
	}
	result, err := getRangeProof(v, start, end, maxLength)
	if err != nil {
		return nil, err
	}
	if v.isInvalid() {
		return nil, ErrInvalid
	}
	return result, nil
}

// CommitToDB commits changes from this view to the underlying DB.
func (v *view) CommitToDB(ctx context.Context) error {
	ctx, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.CommitToDB")
	defer span.End()

	v.db.commitLock.Lock()
	defer v.db.commitLock.Unlock()

	return v.commitToDB(ctx)
}

// Commits the changes from [trieToCommit] to the db.
// Assumes that its parent view has already been committed to the db.
// Assumes [v.db.commitLock] is held.
func (v *view) commitToDB(ctx context.Context) error {
	v.commitLock.Lock()
	defer v.commitLock.Unlock()

	ctx, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.commitToDB", oteltrace.WithAttributes(
		attribute.Int("changeCount", len(v.changes.keyChanges)),
	))
	defer span.End()

	// Call this here instead of in [v.db.commitView] because doing so there
	// would be a deadlock.
	if err := v.applyValueChanges(ctx); err != nil {
		return err
	}

	if err := v.db.commitView(ctx, v); err != nil {
		return err
	}

	v.committed = true

	return nil
}

// Assumes [v.validityTrackingLock] isn't held.
func (v *view) isInvalid() bool {
	v.validityTrackingLock.RLock()
	defer v.validityTrackingLock.RUnlock()

	return v.invalidated
}

// Invalidates this view and all descendants.
// Assumes [v.validityTrackingLock] isn't held.
func (v *view) invalidate() {
	v.validityTrackingLock.Lock()
	defer v.validityTrackingLock.Unlock()

	v.invalidated = true

	for _, childView := range v.childViews {
		childView.invalidate()
	}

	// after invalidating the children, they no longer need to be tracked
	v.childViews = make([]*view, 0, defaultPreallocationSize)
}

func (v *view) updateParent(newParent View) {
	v.validityTrackingLock.Lock()
	defer v.validityTrackingLock.Unlock()

	v.parentTrie = newParent
}

// GetMerkleRoot returns the ID of the root of this view.
func (v *view) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	if err := v.applyValueChanges(ctx); err != nil {
		return ids.Empty, err
	}
	return v.changes.rootID, nil
}

func (v *view) GetValues(ctx context.Context, keys [][]byte) ([][]byte, []error) {
	_, span := v.db.debugTracer.Start(ctx, "MerkleDB.view.GetValues", oteltrace.WithAttributes(
		attribute.Int("keyCount", len(keys)),
	))
	defer span.End()

	results := make([][]byte, len(keys))
	valueErrors := make([]error, len(keys))

	for i, key := range keys {
		results[i], valueErrors[i] = v.getValueCopy(ToKey(key))
	}
	return results, valueErrors
}

// GetValue returns the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
func (v *view) GetValue(ctx context.Context, key []byte) ([]byte, error) {
	_, span := v.db.debugTracer.Start(ctx, "MerkleDB.view.GetValue")
	defer span.End()

	return v.getValueCopy(ToKey(key))
}

// getValueCopy returns a copy of the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
func (v *view) getValueCopy(key Key) ([]byte, error) {
	val, err := v.getValue(key)
	if err != nil {
		return nil, err
	}
	return slices.Clone(val), nil
}

func (v *view) getValue(key Key) ([]byte, error) {
	if v.isInvalid() {
		return nil, ErrInvalid
	}

	if change, ok := v.changes.keyChanges[key]; ok {
		v.db.metrics.ViewChangesValueHit()
		if change.after.IsNothing() {
			return nil, database.ErrNotFound
		}
		return change.after.Value(), nil
	}
	v.db.metrics.ViewChangesValueMiss()

	// if we don't have local copy of the value, then grab a copy from the parent trie
	value, err := v.getParentTrie().getValue(key)
	if err != nil {
		return nil, err
	}

	// ensure no ancestor changes occurred during execution
	if v.isInvalid() {
		return nil, ErrInvalid
	}

	return value, nil
}

// Must not be called after [applyValueChanges] has returned.
func (v *view) remove(key Key) error {
	if v.valueChangesApplied.Get() {
		return ErrNodesAlreadyCalculated
	}

	// confirm a node exists with a value
	keyNode, err := v.getNode(key, true)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			// [key] isn't in the trie.
			return nil
		}
		return err
	}

	if !keyNode.hasValue() {
		// [key] doesn't have a value.
		return nil
	}

	// if the node exists and contains a value
	// mark all ancestor for change
	// grab parent and grandparent nodes for path compression
	var grandParent, parent, nodeToDelete *node
	if err := visitPathToKey(v, key, func(n *node) error {
		grandParent = parent
		parent = nodeToDelete
		nodeToDelete = n
		return v.recordNodeChange(n)
	}); err != nil {
		return err
	}

	hadValue := nodeToDelete.hasValue()
	nodeToDelete.setValue(v.db.hasher, maybe.Nothing[[]byte]())

	// if the removed node has no children, the node can be removed from the trie
	if len(nodeToDelete.children) == 0 {
		if err := v.recordNodeDeleted(nodeToDelete, hadValue); err != nil {
			return err
		}

		if nodeToDelete.key == v.root.Value().key {
			// We deleted the root. The trie is empty now.
			v.root = maybe.Nothing[*node]()
			return nil
		}

		// Note [parent] != nil since [nodeToDelete.key] != [v.root.key].
		// i.e. There's the root and at least one more node.
		parent.removeChild(nodeToDelete, v.tokenSize)

		// merge the parent node and its child into a single node if possible
		return v.compressNodePath(grandParent, parent)
	}

	// merge this node and its parent into a single node if possible
	return v.compressNodePath(parent, nodeToDelete)
}

// Merges [n] with its [parent] if [n] has only one child and no value.
// If [parent] is nil, [n] is the root node and [v.root] is updated to [n].
// Assumes at least one of the following is true:
// * [n] has a value.
// * [n] has children.
// Must not be called after [applyValueChanges] has returned.
func (v *view) compressNodePath(parent, n *node) error {
	if v.valueChangesApplied.Get() {
		return ErrNodesAlreadyCalculated
	}

	if len(n.children) != 1 || n.hasValue() {
		return nil
	}

	// We know from above that [n] has no value.
	if err := v.recordNodeDeleted(n, false /* hasValue */); err != nil {
		return err
	}

	var (
		childEntry *child
		childKey   Key
	)
	// There is only one child, but we don't know the index.
	// "Cycle" over the key/values to find the only child.
	// Note this iteration once because len(node.children) == 1.
	for index, entry := range n.children {
		childKey = n.key.Extend(ToToken(index, v.tokenSize), entry.compressedKey)
		childEntry = entry
	}

	if parent == nil {
		root, err := v.getNode(childKey, childEntry.hasValue)
		if err != nil {
			return err
		}
		v.root = maybe.Some(root)
		return nil
	}

	parent.setChildEntry(childKey.Token(parent.key.length, v.tokenSize),
		&child{
			compressedKey: childKey.Skip(parent.key.length + v.tokenSize),
			id:            childEntry.id,
			hasValue:      childEntry.hasValue,
		})
	return v.recordNodeChange(parent)
}

// Get a copy of the node matching the passed key from the view.
// Used by views to get nodes from their ancestors.
func (v *view) getEditableNode(key Key, hadValue bool) (*node, error) {
	if v.isInvalid() {
		return nil, ErrInvalid
	}

	// grab the node in question
	n, err := v.getNode(key, hadValue)
	if err != nil {
		return nil, err
	}

	// ensure no ancestor changes occurred during execution
	if v.isInvalid() {
		return nil, ErrInvalid
	}

	// return a clone of the node, so it can be edited without affecting this view
	return n.clone(), nil
}

// insert a key/value pair into the correct node of the trie.
// Must not be called after [applyValueChanges] has returned.
func (v *view) insert(
	key Key,
	value maybe.Maybe[[]byte],
) (*node, error) {
	if v.valueChangesApplied.Get() {
		return nil, ErrNodesAlreadyCalculated
	}

	if v.root.IsNothing() {
		// the trie is empty, so create a new root node.
		root := newNode(key)
		root.setValue(v.db.hasher, value)
		v.root = maybe.Some(root)
		return root, v.recordNewNode(root)
	}

	// Find the node that most closely matches [key].
	var closestNode *node
	if err := visitPathToKey(v, key, func(n *node) error {
		closestNode = n
		// Need to recalculate ID for all nodes on path to [key].
		return v.recordNodeChange(n)
	}); err != nil {
		return nil, err
	}

	if closestNode == nil {
		// [v.root.key] isn't a prefix of [key].
		var (
			oldRoot            = v.root.Value()
			commonPrefixLength = getLengthOfCommonPrefix(oldRoot.key, key, 0 /*offset*/, v.tokenSize)
			commonPrefix       = oldRoot.key.Take(commonPrefixLength)
			newRoot            = newNode(commonPrefix)
			oldRootID          = v.db.hasher.HashNode(oldRoot)
		)
		v.db.metrics.HashCalculated()

		// Call addChildWithID instead of addChild so the old root is added
		// to the new root with the correct ID.
		// TODO:
		// [oldRootID] shouldn't need to be calculated here.
		// Either oldRootID should already be calculated or will be calculated at the end with the other nodes
		// Initialize the v.changes.rootID during newView and then use that here instead of oldRootID
		newRoot.addChildWithID(oldRoot, v.tokenSize, oldRootID)
		if err := v.recordNewNode(newRoot); err != nil {
			return nil, err
		}
		v.root = maybe.Some(newRoot)

		closestNode = newRoot
	}

	// a node with that exact key already exists so update its value
	if closestNode.key == key {
		closestNode.setValue(v.db.hasher, value)
		// closestNode was already marked as changed in the ancestry loop above
		return closestNode, nil
	}

	// A node with the exact key doesn't exist so determine the portion of the
	// key that hasn't been matched yet
	// Note that [key] has prefix [closestNode.key], so [key] must be longer
	// and the following index won't OOB.
	existingChildEntry, hasChild := closestNode.children[key.Token(closestNode.key.length, v.tokenSize)]
	if !hasChild {
		// there are no existing nodes along the key [key], so create a new node to insert [value]
		newNode := newNode(key)
		newNode.setValue(v.db.hasher, value)
		closestNode.addChild(newNode, v.tokenSize)
		return newNode, v.recordNewNode(newNode)
	}

	// if we have reached this point, then the [key] we are trying to insert and
	// the existing path node have some common prefix.
	// a new branching node will be created that will represent this common prefix and
	// have the existing path node and the value being inserted as children.

	// generate the new branch node
	// find how many tokens are common between the existing child's compressed key and
	// the current key(offset by the closest node's key),
	// then move all the common tokens into the branch node
	commonPrefixLength := getLengthOfCommonPrefix(
		existingChildEntry.compressedKey,
		key,
		closestNode.key.length+v.tokenSize,
		v.tokenSize,
	)

	if existingChildEntry.compressedKey.length <= commonPrefixLength {
		// Since the compressed key is shorter than the common prefix,
		// we should have visited [existingChildEntry] in [visitPathToKey].
		return nil, ErrVisitPathToKey
	}

	branchNode := newNode(key.Take(closestNode.key.length + v.tokenSize + commonPrefixLength))
	closestNode.addChild(branchNode, v.tokenSize)
	nodeWithValue := branchNode

	if key.length == branchNode.key.length {
		// the branch node has exactly the key to be inserted as its key, so set the value on the branch node
		branchNode.setValue(v.db.hasher, value)
	} else {
		// the key to be inserted is a child of the branch node
		// create a new node and add the value to it
		newNode := newNode(key)
		newNode.setValue(v.db.hasher, value)
		branchNode.addChild(newNode, v.tokenSize)
		if err := v.recordNewNode(newNode); err != nil {
			return nil, err
		}
		nodeWithValue = newNode
	}

	// add the existing child onto the branch node
	branchNode.setChildEntry(
		existingChildEntry.compressedKey.Token(commonPrefixLength, v.tokenSize),
		&child{
			compressedKey: existingChildEntry.compressedKey.Skip(commonPrefixLength + v.tokenSize),
			id:            existingChildEntry.id,
			hasValue:      existingChildEntry.hasValue,
		})

	return nodeWithValue, v.recordNewNode(branchNode)
}

func getLengthOfCommonPrefix(first, second Key, secondOffset int, tokenSize int) int {
	commonIndex := 0
	for first.length > commonIndex && second.length > commonIndex+secondOffset &&
		first.Token(commonIndex, tokenSize) == second.Token(commonIndex+secondOffset, tokenSize) {
		commonIndex += tokenSize
	}
	return commonIndex
}

// Records that a node has been created.
// Must not be called after [applyValueChanges] has returned.
func (v *view) recordNewNode(after *node) error {
	return v.recordKeyChange(after.key, after, after.hasValue(), true /* newNode */)
}

// Records that an existing node has been changed.
// Must not be called after [applyValueChanges] has returned.
func (v *view) recordNodeChange(after *node) error {
	return v.recordKeyChange(after.key, after, after.hasValue(), false /* newNode */)
}

// Records that the node associated with the given key has been deleted.
// Must not be called after [applyValueChanges] has returned.
func (v *view) recordNodeDeleted(after *node, hadValue bool) error {
	return v.recordKeyChange(after.key, nil, hadValue, false /* newNode */)
}

// Records that the node associated with the given key has been changed.
// If it is an existing node, record what its value was before it was changed.
// Must not be called after [applyValueChanges] has returned.
func (v *view) recordKeyChange(key Key, after *node, hadValue bool, newNode bool) error {
	if v.valueChangesApplied.Get() {
		return ErrNodesAlreadyCalculated
	}

	if existing, ok := v.changes.nodes[key]; ok {
		existing.after = after
		return nil
	}

	if newNode {
		v.changes.nodes[key] = &change[*node]{
			after: after,
		}
		return nil
	}

	before, err := v.getParentTrie().getEditableNode(key, hadValue)
	if err != nil {
		return err
	}
	v.changes.nodes[key] = &change[*node]{
		before: before,
		after:  after,
	}
	return nil
}

// Retrieves a node with the given [key].
// If the node is fetched from [v.parentTrie] and [id] isn't empty,
// sets the node's ID to [id].
// If the node is loaded from the baseDB, [hasValue] determines which database the node is stored in.
// Returns database.ErrNotFound if the node doesn't exist.
func (v *view) getNode(key Key, hasValue bool) (*node, error) {
	// check for the key within the changed nodes
	if nodeChange, isChanged := v.changes.nodes[key]; isChanged {
		v.db.metrics.ViewChangesNodeHit()
		if nodeChange.after == nil {
			return nil, database.ErrNotFound
		}
		return nodeChange.after, nil
	}
	v.db.metrics.ViewChangesNodeMiss()

	// get the node from the parent trie and store a local copy
	return v.getParentTrie().getEditableNode(key, hasValue)
}

// Get the parent trie of the view
func (v *view) getParentTrie() View {
	v.validityTrackingLock.RLock()
	defer v.validityTrackingLock.RUnlock()
	return v.parentTrie
}
