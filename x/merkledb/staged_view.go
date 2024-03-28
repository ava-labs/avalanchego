// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

type StagedParent interface {
	trieInternals

	NewStagedView(context.Context) (*StagedView, error)
}

type StagedView struct {
	// the uncommitted parent trie of this view
	// [validityTrackingLock] must be held when reading/writing this field.
	ancestry   sync.Mutex
	parentTrie StagedParent
	child      *StagedView

	// Changes made to this view.
	// May include nodes that haven't been updated
	// but will when their ID is recalculated.
	changes *changeSummary

	db *merkleDB

	// The root of the trie represented by this view.
	root maybe.Maybe[*node]

	tokenSize int
}

func (v *StagedView) NewStagedView(context.Context) (*StagedView, error) {
	return &StagedView{
		root:       maybe.Bind(v.getRoot(), (*node).clone),
		db:         v.db,
		parentTrie: v,
		tokenSize:  v.db.tokenSize,
	}, nil
}

func (v *StagedView) Add(ctx context.Context, changes map[string]maybe.Maybe[[]byte]) error {
	// TODO: turn this into an async queue
	for skey, val := range changes {
		key := toKey(stringToByteSlice(skey))
		change, err := v.recordValueChange(key, maybe.Bind(val, slices.Clone[[]byte]))
		if err != nil {
			return err
		}
		if change.after.IsNothing() {
			// Note we're setting [err] defined outside this function.
			if err := v.remove(key); err != nil {
				return err
			}
			// Note we're setting [err] defined outside this function.
		} else if _, err := v.insert(key, change.after); err != nil {
			return err
		}
	}
	return nil
}

// Done waits for queue to finish processing changes to trie
func (v *StagedView) Done() {
}

func (v *StagedView) getTokenSize() int {
	return v.tokenSize
}

func (v *StagedView) getRoot() maybe.Maybe[*node] {
	return v.root
}

// Recalculates the node IDs for all changed nodes in the trie.
// Cancelling [ctx] doesn't cancel calculation. It's used only for tracing.
func (v *StagedView) calculateNodeIDs(ctx context.Context) error {
	oldRoot := maybe.Bind(v.root, (*node).clone)

	// We wait to create the span until after checking that we need to actually
	// calculateNodeIDs to make traces more useful (otherwise there may be a span
	// per key modified even though IDs are not re-calculated).
	_, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.calculateNodeIDs")
	defer span.End()

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
	return nil
}

// Calculates the ID of all descendants of [n] which need to be recalculated,
// and then calculates the ID of [n] itself.
func (v *StagedView) calculateNodeIDsHelper(n *node) ids.ID {
	// We use [wg] to wait until all descendants of [n] have been updated.
	var wg sync.WaitGroup

	for childIndex, childEntry := range n.children {
		childEntry := childEntry // New variable so goroutine doesn't capture loop variable.
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

// CommitToDB commits changes from this view to the underlying DB.
func (v *StagedView) CommitToDB(ctx context.Context) error {
	ctx, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.CommitToDB")
	defer span.End()

	v.db.commitLock.Lock()
	defer v.db.commitLock.Unlock()

	if err := v.calculateNodeIDs(ctx); err != nil {
		return err
	}
	if err := v.db.commitStagedChanges(ctx, v); err != nil {
		return err
	}

	// Update child with correct parent trie
	if v.child != nil {
		v.child.ancestry.Lock()
		v.parentTrie = v.db
		v.child.ancestry.Unlock()
	}
	return nil
}

func (v *StagedView) getValue(key Key) ([]byte, error) {
	if change, ok := v.changes.values[key]; ok {
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
	return value, nil
}

// Must not be called after [calculateNodeIDs] has returned.
func (v *StagedView) remove(key Key) error {
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
	nodeToDelete.setValue(maybe.Nothing[[]byte]())

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
// Must not be called after [calculateNodeIDs] has returned.
func (v *StagedView) compressNodePath(parent, n *node) error {
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
func (v *StagedView) getEditableNode(key Key, hadValue bool) (*node, error) {
	// grab the node in question
	n, err := v.getNode(key, hadValue)
	if err != nil {
		return nil, err
	}

	// return a clone of the node, so it can be edited without affecting this view
	return n.clone(), nil
}

// insert a key/value pair into the correct node of the trie.
// Must not be called after [calculateNodeIDs] has returned.
func (v *StagedView) insert(
	key Key,
	value maybe.Maybe[[]byte],
) (*node, error) {
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

// Records that a node has been created.
// Must not be called after [calculateNodeIDs] has returned.
func (v *StagedView) recordNewNode(after *node) error {
	return v.recordKeyChange(after.key, after, after.hasValue(), true /* newNode */)
}

// Records that an existing node has been changed.
// Must not be called after [calculateNodeIDs] has returned.
func (v *StagedView) recordNodeChange(after *node) error {
	return v.recordKeyChange(after.key, after, after.hasValue(), false /* newNode */)
}

// Records that the node associated with the given key has been deleted.
// Must not be called after [calculateNodeIDs] has returned.
func (v *StagedView) recordNodeDeleted(after *node, hadValue bool) error {
	return v.recordKeyChange(after.key, nil, hadValue, false /* newNode */)
}

// Records that the node associated with the given key has been changed.
// If it is an existing node, record what its value was before it was changed.
// Must not be called after [calculateNodeIDs] has returned.
func (v *StagedView) recordKeyChange(key Key, after *node, hadValue bool, newNode bool) error {
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

// Records that a key's value has been added or updated.
// Doesn't actually change the trie data structure.
// That's deferred until we call [calculateNodeIDs].
// Must not be called after [calculateNodeIDs] has returned.
func (v *StagedView) recordValueChange(key Key, value maybe.Maybe[[]byte]) (*change[maybe.Maybe[[]byte]], error) {
	// update the existing change if it exists
	if existing, ok := v.changes.values[key]; ok {
		existing.after = value
		return existing, nil
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
		return nil, err
	}

	c := &change[maybe.Maybe[[]byte]]{
		before: beforeMaybe,
		after:  value,
	}
	v.changes.values[key] = c
	return c, nil
}

// Retrieves a node with the given [key].
// If the node is fetched from [v.parentTrie] and [id] isn't empty,
// sets the node's ID to [id].
// If the node is loaded from the baseDB, [hasValue] determines which database the node is stored in.
// Returns database.ErrNotFound if the node doesn't exist.
func (v *StagedView) getNode(key Key, hasValue bool) (*node, error) {
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
func (v *StagedView) getParentTrie() StagedParent {
	v.ancestry.Lock()
	defer v.ancestry.Unlock()

	return v.parentTrie
}
