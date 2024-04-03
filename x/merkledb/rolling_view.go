// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

type RollingParent interface {
	trieInternals

	NewRollingView(context.Context, int) (*RollingView, error)
}

type RollingView struct {
	parentLock sync.Mutex
	parentTrie RollingParent

	child *RollingView // assumed only ever 1

	// Changes made to this view.
	// May include nodes that haven't been updated
	// but will when their ID is recalculated.
	changes *changeSummary

	rootGenerated bool
	rootOnce      sync.Once

	commitLock sync.Mutex
	db         *merkleDB

	// The root of the trie represented by this view.
	root maybe.Maybe[*node]

	tokenSize int
}

func (v *RollingView) NewRollingView(_ context.Context, changes int) (*RollingView, error) {
	v.commitLock.Lock()
	defer v.commitLock.Unlock()

	if !v.rootGenerated {
		return nil, errors.New("root not generated")
	}

	// RollingView is not meant to be used with more than 1 child
	if v.child != nil {
		return nil, errors.New("RollingView already has a child")
	}

	nv := newRollingView(v.db, v, changes)
	v.child = nv
	return nv, nil
}

func newRollingView(db *merkleDB, parentTrie RollingParent, changes int) *RollingView {
	return &RollingView{
		// root will not be updated by the time we touch it
		root:       maybe.Bind(parentTrie.getRoot(), (*node).clone),
		db:         db,
		parentTrie: parentTrie,
		changes:    newChangeSummary(changes),
		tokenSize:  db.tokenSize,
	}
}

func (v *RollingView) Update(ctx context.Context, skey string, val maybe.Maybe[[]byte]) error {
	key := toKey(stringToByteSlice(skey))
	if err := v.recordValueChange(key, maybe.Bind(val, slices.Clone[[]byte])); err != nil {
		return err
	}
	if val.IsNothing() {
		// Note we're setting [err] defined outside this function.
		if err := v.remove(key); err != nil {
			return err
		}
		// Note we're setting [err] defined outside this function.
	} else if _, err := v.insert(key, val); err != nil {
		return err
	}
	return nil
}

func (v *RollingView) getTokenSize() int {
	return v.tokenSize
}

func (v *RollingView) getRoot() maybe.Maybe[*node] {
	return v.root
}

func (v *RollingView) hashChangedNodes(ctx context.Context) {
	_, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.hashChangedNodes")
	defer span.End()

	if v.root.IsNothing() {
		v.changes.rootID = ids.Empty
		return
	}

	// If there are no children, we can avoid allocating [keyBuffer].
	root := v.root.Value()
	if len(root.children) == 0 {
		v.changes.rootID = root.calculateID(v.db.metrics)
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
func (v *RollingView) hashChangedNode(n *node, keyBuffer []byte) (ids.ID, []byte) {
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
			childEntry.id = childNode.calculateID(v.db.metrics)
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
	return n.calculateID(v.db.metrics), keyBuffer
}

// setKeyBuffer expands [keyBuffer] to have sufficient size for any of [n]'s
// child keys and populates [n]'s key into [keyBuffer]. If [keyBuffer] already
// has sufficient size, this function will not perform any memory allocations.
func (v *RollingView) setKeyBuffer(n *node, keyBuffer []byte) []byte {
	keyBuffer = v.setLengthForChildren(n, keyBuffer)
	copy(keyBuffer, n.key.value)
	return keyBuffer
}

// setLengthForChildren expands [keyBuffer] to have sufficient size for any of
// [n]'s child keys.
func (v *RollingView) setLengthForChildren(n *node, keyBuffer []byte) []byte {
	// Calculate the size of the largest child key of this node.
	var maxBitLength int
	for _, childEntry := range n.children {
		maxBitLength = max(maxBitLength, childEntry.compressedKey.length)
	}
	maxBytesNeeded := bytesNeeded(n.key.length + v.tokenSize + maxBitLength)
	return setBytesLength(keyBuffer, maxBytesNeeded)
}

func (v *RollingView) getValue(key Key) ([]byte, error) {
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
func (v *RollingView) remove(key Key) error {
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

		// We call node changed on this on the way to the child, so we don't need to call it again.

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
func (v *RollingView) compressNodePath(parent, n *node) error {
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
func (v *RollingView) getEditableNode(key Key, hadValue bool) (*node, error) {
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
func (v *RollingView) insert(
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

			// TODO: We need to remove this calculation if we want to allow views to be built
			// on top of other views prior to their root calculation.
			oldRootID = oldRoot.calculateID(v.db.metrics)
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
func (v *RollingView) recordNewNode(after *node) error {
	return v.recordKeyChange(after.key, after, after.hasValue(), true /* newNode */)
}

// Records that an existing node has been changed.
// Must not be called after [calculateNodeIDs] has returned.
func (v *RollingView) recordNodeChange(after *node) error {
	return v.recordKeyChange(after.key, after, after.hasValue(), false /* newNode */)
}

// Records that the node associated with the given key has been deleted.
// Must not be called after [calculateNodeIDs] has returned.
func (v *RollingView) recordNodeDeleted(after *node, hadValue bool) error {
	return v.recordKeyChange(after.key, nil, hadValue, false /* newNode */)
}

// Records that the node associated with the given key has been changed.
// If it is an existing node, record what its value was before it was changed.
// Must not be called after [calculateNodeIDs] has returned.
func (v *RollingView) recordKeyChange(key Key, after *node, hadValue bool, newNode bool) error {
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
func (v *RollingView) recordValueChange(key Key, value maybe.Maybe[[]byte]) error {
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
func (v *RollingView) getNode(key Key, hasValue bool) (*node, error) {
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

func (v *RollingView) updateParent(newParent RollingParent) {
	v.parentLock.Lock()
	defer v.parentLock.Unlock()

	v.parentTrie = newParent
}

// Get the parent trie of the view
func (v *RollingView) getParentTrie() RollingParent {
	v.parentLock.Lock()
	defer v.parentLock.Unlock()

	return v.parentTrie
}

func (v *RollingView) Merklize(ctx context.Context) (ids.ID, error) {
	v.rootOnce.Do(func() {
		oldRoot := maybe.Bind(v.root, (*node).clone)

		// We wait to create the span until after checking that we need to actually
		// calculateNodeIDs to make traces more useful (otherwise there may be a span
		// per key modified even though IDs are not re-calculated).
		_, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.calculateNodeIDs")
		defer span.End()

		// Remove no-op up value changes
		wg := &sync.WaitGroup{}
		wg.Add(2)
		go func() {
			defer wg.Done()

			for key, change := range v.changes.values {
				if !maybe.Equal(change.before, change.after, func(a, b []byte) bool { return bytes.Equal(a, b) }) {
					continue
				}
				delete(v.changes.values, key)
			}
		}()

		// Compute root
		go func() {
			defer wg.Done()

			v.hashChangedNodes(ctx)

			v.changes.rootChange = change[maybe.Maybe[*node]]{
				before: oldRoot,
				after:  v.root,
			}

			// Remove nodes that didn't change
			//
			// This can happen if we add a value and then remove it
			// or if through a series of updates the value is the same
			// as the parent view.
			//
			// We can't do this during root generation because we may not
			// touch all nodes in [v.changes.nodes].
			//
			// TODO: find a more efficient way to do this (where we avoid iterating over everything)
			for key, change := range v.changes.nodes {
				if !change.before.equals(change.after) {
					continue
				}
				delete(v.changes.nodes, key)
			}

			// Mark root as generated so we can commit
			v.rootGenerated = true
		}()

		// Wait for work to be done
		wg.Wait()
	})
	return v.changes.rootID, nil
}

// Only call after we merklize (where duplicates are removed)
func (v *RollingView) Changes() (int, int) {
	return len(v.changes.nodes), len(v.changes.values)
}

// Commit commits changes from this view to the underlying DB.
func (v *RollingView) Commit(ctx context.Context) error {
	ctx, span := v.db.infoTracer.Start(ctx, "MerkleDB.view.CommitToDB")
	defer span.End()

	v.commitLock.Lock()
	defer v.commitLock.Unlock()

	if !v.rootGenerated {
		return errors.New("root not generated")
	}

	v.db.commitLock.Lock()
	defer v.db.commitLock.Unlock()

	if err := v.db.commitRollingChanges(ctx, v); err != nil {
		return err
	}

	if v.child != nil {
		v.child.updateParent(v.db)
	}
	return nil
}
