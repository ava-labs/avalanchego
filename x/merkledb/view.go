// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"errors"
	"sync"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/maybe"
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

	// tracking bool to enforce that no changes are made to the trie after the nodes have been calculated
	nodesAlreadyCalculated utils.Atomic[bool]

	// calculateNodesOnce is a once to ensure that node calculation only occurs a single time
	calculateNodesOnce sync.Once

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

	if err := v.calculateNodeIDs(ctx); err != nil {
		return nil, err
	}

	newView, err := newView(v.db, v, changes)
	if err != nil {
		return nil, err
	}

	v.validityTrackingLock.Lock()
	defer v.validityTrackingLock.Unlock()

	if v.invalidated {
		return nil, ErrInvalid
	}
	v.childViews = append(v.childViews, newView)

	return newView, nil
}

// Creates a new view with the given [parentTrie].
func newView(
	db *merkleDB,
	parentTrie View,
	changes ViewChanges,
) (*view, error) {
	newView := &view{
		root:       maybe.Bind(parentTrie.getRoot(), (*node).clone),
		db:         db,
		parentTrie: parentTrie,
		changes:    newChangeSummary(len(changes.BatchOps) + len(changes.MapOps)),
		tokenSize:  db.tokenSize,
	}

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
		if err := newView.recordValueChange(toKey(key), newVal); err != nil {
			return nil, err
		}
	}
	for key, val := range changes.MapOps {
		if !changes.ConsumeBytes {
			val = maybe.Bind(val, slices.Clone[[]byte])
		}
		if err := newView.recordValueChange(toKey(stringToByteSlice(key)), val); err != nil {
			return nil, err
		}
	}
	return newView, nil
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

	newView := &view{
		root:       changes.rootChange.after,
		db:         db,
		parentTrie: db,
		changes:    changes,
		tokenSize:  db.tokenSize,
	}
	// since this is a set of historical changes, all nodes have already been calculated
	// since no new changes have occurred, no new calculations need to be done
	newView.calculateNodesOnce.Do(func() {})
	newView.nodesAlreadyCalculated.Set(true)
	return newView, nil
}

func (v *view) getTokenSize() int {
	return v.tokenSize
}

func (v *view) getRoot() maybe.Maybe[*node] {
	return v.root
}

// Recalculates the node IDs for all changed nodes in the trie.
// Cancelling [ctx] doesn't cancel calculation. It's used only for tracing.
func (v *view) calculateNodeIDs(ctx context.Context) error {
	var err error
	v.calculateNodesOnce.Do(func() {
		if v.isInvalid() {
			err = ErrInvalid
			return
		}
		defer v.nodesAlreadyCalculated.Set(true)

		oldRoot := maybe.Bind(v.root, (*node).clone)

		// We wait to create the span until after checking that we need to actually
		// calculateNodeIDs to make traces more useful (otherwise there may be a span
		// per key modified even though IDs are not re-calculated).
		_, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.calculateNodeIDs")
		defer span.End()

		// add all the changed key/values to the nodes of the trie
		for key, change := range v.changes.values {
			if change.after.IsNothing() {
				// Note we're setting [err] defined outside this function.
				if err = v.remove(key); err != nil {
					return
				}
				// Note we're setting [err] defined outside this function.
			} else if _, err = v.insert(key, change.after); err != nil {
				return
			}
		}

		if !v.root.IsNothing() {
			_ = v.db.calculateNodeIDsSema.Acquire(context.Background(), 1)
			v.changes.rootID = v.calculateNodeIDsHelper(v.root.Value())
			v.db.calculateNodeIDsSema.Release(1)
		} else {
			v.changes.rootID = ids.Empty
		}

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

// Calculates the ID of all descendants of [n] which need to be recalculated,
// and then calculates the ID of [n] itself.
func (v *view) calculateNodeIDsHelper(n *node) ids.ID {
	// We use [wg] to wait until all descendants of [n] have been updated.
	var wg sync.WaitGroup

	for childIndex := range n.children {
		childEntry := n.children[childIndex]
		childKey := n.key.Extend(ToToken(childIndex, v.tokenSize), childEntry.compressedKey)
		childNodeChange, ok := v.changes.nodes[childKey]
		if !ok {
			// This child wasn't changed.
			continue
		}
		childEntry.hasValue = childNodeChange.after.hasValue()

		// Try updating the child and its descendants in a goroutine.
		if ok := v.db.calculateNodeIDsSema.TryAcquire(1); ok {
			wg.Add(1)
			go func() {
				childEntry.id = v.calculateNodeIDsHelper(childNodeChange.after)
				v.db.calculateNodeIDsSema.Release(1)
				wg.Done()
			}()
		} else {
			// We're at the goroutine limit; do the work in this goroutine.
			childEntry.id = v.calculateNodeIDsHelper(childNodeChange.after)
		}
	}

	// Wait until all descendants of [n] have been updated.
	wg.Wait()

	// The IDs [n]'s descendants are up to date so we can calculate [n]'s ID.
	return n.calculateID(v.db.metrics)
}

// GetProof returns a proof that [bytesPath] is in or not in trie [t].
func (v *view) GetProof(ctx context.Context, key []byte) (*Proof, error) {
	_, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.GetProof")
	defer span.End()

	if err := v.calculateNodeIDs(ctx); err != nil {
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

	if err := v.calculateNodeIDs(ctx); err != nil {
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

// Commits the changes from [trieToCommit] to this view,
// this view to its parent, and so on until committing to the db.
// Assumes [v.db.commitLock] is held.
func (v *view) commitToDB(ctx context.Context) error {
	v.commitLock.Lock()
	defer v.commitLock.Unlock()

	ctx, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.commitToDB", oteltrace.WithAttributes(
		attribute.Int("changeCount", len(v.changes.values)),
	))
	defer span.End()

	// Call this here instead of in [v.db.commitChanges]
	// because doing so there would be a deadlock.
	if err := v.calculateNodeIDs(ctx); err != nil {
		return err
	}

	if err := v.db.commitChanges(ctx, v); err != nil {
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
	if err := v.calculateNodeIDs(ctx); err != nil {
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

	if change, ok := v.changes.values[key]; ok {
		v.db.metrics.ViewValueCacheHit()
		if change.after.IsNothing() {
			return nil, database.ErrNotFound
		}
		return change.after.Value(), nil
	}
	v.db.metrics.ViewValueCacheMiss()

	// if we don't have local copy of the key, then grab a copy from the parent trie
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

// Must not be called after [calculateNodeIDs] has returned.
func (v *view) remove(key Key) error {
	if v.nodesAlreadyCalculated.Get() {
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

	nodeToDelete.setValue(maybe.Nothing[[]byte]())

	// if the removed node has no children, the node can be removed from the trie
	if len(nodeToDelete.children) == 0 {
		if err := v.recordNodeDeleted(nodeToDelete); err != nil {
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

	// merge this node and its descendants into a single node if possible
	return v.compressNodePath(parent, nodeToDelete)
}

// Merges together nodes in the inclusive descendants of [n] that
// have no value and a single child into one node with a compressed
// path until a node that doesn't meet those criteria is reached.
// [parent] is [n]'s parent. If [parent] is nil, [n] is the root
// node and [v.root] is updated to [n].
// Assumes at least one of the following is true:
// * [n] has a value.
// * [n] has children.
// Must not be called after [calculateNodeIDs] has returned.
func (v *view) compressNodePath(parent, n *node) error {
	if v.nodesAlreadyCalculated.Get() {
		return ErrNodesAlreadyCalculated
	}

	if len(n.children) != 1 || n.hasValue() {
		return nil
	}

	if err := v.recordNodeDeleted(n); err != nil {
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
// Must not be called after [calculateNodeIDs] has returned.
func (v *view) insert(
	key Key,
	value maybe.Maybe[[]byte],
) (*node, error) {
	if v.nodesAlreadyCalculated.Get() {
		return nil, ErrNodesAlreadyCalculated
	}

	if v.root.IsNothing() {
		// the trie is empty, so create a new root node.
		root := newNode(key)
		root.setValue(value)
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
			oldRootID          = oldRoot.calculateID(v.db.metrics)
		)

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
		closestNode.setValue(value)
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
		newNode.setValue(value)
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
		branchNode.setValue(value)
	} else {
		// the key to be inserted is a child of the branch node
		// create a new node and add the value to it
		newNode := newNode(key)
		newNode.setValue(value)
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
// Must not be called after [calculateNodeIDs] has returned.
func (v *view) recordNewNode(after *node) error {
	return v.recordKeyChange(after.key, after, after.hasValue(), true /* newNode */)
}

// Records that an existing node has been changed.
// Must not be called after [calculateNodeIDs] has returned.
func (v *view) recordNodeChange(after *node) error {
	return v.recordKeyChange(after.key, after, after.hasValue(), false /* newNode */)
}

// Records that the node associated with the given key has been deleted.
// Must not be called after [calculateNodeIDs] has returned.
func (v *view) recordNodeDeleted(after *node) error {
	return v.recordKeyChange(after.key, nil, after.hasValue(), false /* newNode */)
}

// Records that the node associated with the given key has been changed.
// If it is an existing node, record what its value was before it was changed.
// Must not be called after [calculateNodeIDs] has returned.
func (v *view) recordKeyChange(key Key, after *node, hadValue bool, newNode bool) error {
	if v.nodesAlreadyCalculated.Get() {
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
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	v.changes.nodes[key] = &change[*node]{
		before: before,
		after:  after,
	}
	return nil
}

// Records that a key's value has been added or updated.
// Doesn't actually change the trie data structure.
// That's deferred until we call [calculateNodeIDs].
// Must not be called after [calculateNodeIDs] has returned.
func (v *view) recordValueChange(key Key, value maybe.Maybe[[]byte]) error {
	if v.nodesAlreadyCalculated.Get() {
		return ErrNodesAlreadyCalculated
	}

	// update the existing change if it exists
	if existing, ok := v.changes.values[key]; ok {
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

	v.changes.values[key] = &change[maybe.Maybe[[]byte]]{
		before: beforeMaybe,
		after:  value,
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
		v.db.metrics.ViewNodeCacheHit()
		if nodeChange.after == nil {
			return nil, database.ErrNotFound
		}
		return nodeChange.after, nil
	}

	// get the node from the parent trie and store a local copy
	return v.getParentTrie().getEditableNode(key, hasValue)
}

// Get the parent trie of the view
func (v *view) getParentTrie() View {
	v.validityTrackingLock.RLock()
	defer v.validityTrackingLock.RUnlock()
	return v.parentTrie
}
