// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"sync"

	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

var _ StatelessView = (*statelessView)(nil)

type StatelessView interface {
	MerkleRootGetter

	// NewPreallocatedView returns a new view on top of this Trie with space allocated for changes
	NewStatelessView(estimatedChanges int) (StatelessView, error)

	// GetValue gets the value associated with the specified key
	// database.ErrNotFound if the key is not present
	GetValue(ctx context.Context, key []byte) ([]byte, error)

	// GetValues gets the values associated with the specified keys
	// database.ErrNotFound if the key is not present
	GetValues(ctx context.Context, keys [][]byte) ([][]byte, []error)

	// Insert a key/value pair into the Trie
	Insert(ctx context.Context, key, value []byte) error

	// Remove will delete a key from the Trie
	Remove(ctx context.Context, key []byte) error

	// get the value associated with the key in path form
	// database.ErrNotFound if the key is not present
	getValue(key Path, lock bool) ([]byte, error)

	// get an editable copy of the node with the given key path
	getEditableNode(key Path) (*Node, error)
}

// Editable view of a trie, collects changes on top of a parent trie.
// Delays adding key/value pairs to the trie.
type statelessView struct {
	// Must be held when reading/writing fields except validity tracking fields:
	// [childViews], [parentTrie], and [invalidated].
	// Only use to lock current trieView or ancestors of the current trieView
	lock sync.RWMutex

	// the uncommitted parent trie of this view
	// [validityTrackingLock] must be held when reading/writing this field.
	parentTrie StatelessView

	// Changes made to this view.
	// May include nodes that haven't been updated
	// but will when their ID is recalculated.
	changes *changeSummary

	// Key/value pairs that have been inserted/removed but not
	// yet reflected in the trie's structure. This allows us to
	// defer the cost of updating the trie until we calculate node IDs.
	// A Nothing value indicates that the key has been removed.
	unappliedValueChanges map[Path]Maybe[[]byte]

	db *merkleDB

	// The root of the trie represented by this view.
	root *Node

	// True if the IDs of nodes in this view need to be recalculated.
	needsRecalculation bool

	// If true, this view has been committed and cannot be edited.
	// Calls to Insert and Remove will return ErrCommitted.
	committed bool

	estimatedSize int

	verifierIntercepter *trieViewVerifierIntercepter
}

// Creates a new view with the given [parentTrie].
func newStatelessView(
	db *merkleDB,
	parentTrie StatelessView,
	root *Node,
	estimatedSize int,
) (*statelessView, error) {
	if root == nil {
		return nil, ErrNoValidRoot
	}

	return &statelessView{
		root:                  root,
		db:                    db,
		parentTrie:            parentTrie,
		changes:               newChangeSummary(estimatedSize),
		estimatedSize:         estimatedSize,
		unappliedValueChanges: make(map[Path]Maybe[[]byte], estimatedSize),
	}, nil
}

// NewPreallocatedView returns a new view on top of this one with memory allocated to store the
// [estimatedChanges] number of key/value changes.
// If this view is already committed, the new view's parent will
// be set to the parent of the current view.
// Otherwise, adds the new view to [t.childViews].
// Assumes [t.lock] is not held.
func (t *statelessView) NewStatelessView(
	estimatedChanges int,
) (StatelessView, error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return newStatelessView(t.db, t, t.root.clone(), estimatedChanges)
}

// Recalculates the node IDs for all changed nodes in the trie.
// Assumes [t.lock] is held.
func (t *statelessView) calculateNodeIDs(ctx context.Context) error {
	switch {
	case !t.needsRecalculation:
		return nil
	case t.committed:
		// Note that this should never happen. If a view is committed, it should
		// never be edited, so [t.needsRecalculation] should always be false.
		return ErrCommitted
	}

	// We wait to create the span until after checking that we need to actually
	// calculateNodeIDs to make traces more useful (otherwise there may be a span
	// per key modified even though IDs are not re-calculated).
	ctx, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.calculateNodeIDs")
	defer span.End()

	// ensure that the view under this one is up-to-date before potentially pulling in nodes from it
	// getting the Merkle root forces any unupdated nodes to recalculate their ids
	if _, err := t.getParentTrie().GetMerkleRoot(ctx); err != nil {
		return err
	}

	if err := t.applyChangedValuesToTrie(ctx); err != nil {
		return err
	}

	_, helperSpan := t.db.tracer.Start(ctx, "MerkleDB.trieview.calculateNodeIDsHelper")
	defer helperSpan.End()

	// [eg] limits the number of goroutines we start.
	var eg errgroup.Group
	eg.SetLimit(numCPU)
	if err := t.calculateNodeIDsHelper(ctx, t.root, &eg); err != nil {
		return err
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	t.needsRecalculation = false
	t.changes.rootID = t.root.id

	return nil
}

// Calculates the ID of all descendants of [n] which need to be recalculated,
// and then calculates the ID of [n] itself.
func (t *statelessView) calculateNodeIDsHelper(ctx context.Context, n *Node, eg *errgroup.Group) error {
	var (
		// We use [wg] to wait until all descendants of [n] have been updated.
		// Note we can't wait on [eg] because [eg] may have started goroutines
		// that aren't calculating IDs for descendants of [n].
		wg              sync.WaitGroup
		updatedChildren = make(chan *Node, len(n.children))
	)

	for childIndex, child := range n.children {
		childIndex, child := childIndex, child

		childPath := n.key + Path(childIndex) + child.compressedPath
		childNodeChange, ok := t.changes.nodes[childPath]
		if !ok {
			// This child wasn't changed.
			continue
		}

		wg.Add(1)
		updateChild := func() error {
			defer wg.Done()

			if err := t.calculateNodeIDsHelper(ctx, childNodeChange.after, eg); err != nil {
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

// GetProof returns a proof that [bytesPath] is in or not in trie [t].
func (t *statelessView) GetProof(ctx context.Context, key []byte) (*Proof, error) {
	_, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.GetProof")
	defer span.End()

	t.lock.RLock()
	defer t.lock.RUnlock()

	// only need full lock if nodes ids need to be calculated
	// looped to ensure that the value didn't change after the lock was released
	for t.needsRecalculation {
		t.lock.RUnlock()
		t.lock.Lock()
		if err := t.calculateNodeIDs(ctx); err != nil {
			return nil, err
		}
		t.lock.Unlock()
		t.lock.RLock()
	}

	return t.getProof(ctx, key)
}

// GetProof returns a proof that [bytesPath] is in or not in trie [t].
func (t *statelessView) GetPathProof(ctx context.Context, key Path) (*PathProof, error) {
	_, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.GetPathProof")
	defer span.End()

	t.lock.RLock()
	defer t.lock.RUnlock()

	// only need full lock if nodes ids need to be calculated
	// looped to ensure that the value didn't change after the lock was released
	for t.needsRecalculation {
		t.lock.RUnlock()
		t.lock.Lock()
		if err := t.calculateNodeIDs(ctx); err != nil {
			return nil, err
		}
		t.lock.Unlock()
		t.lock.RLock()
	}

	return t.getPathProof(ctx, key)
}

// Returns a proof that [bytesPath] is in or not in trie [t].
// Assumes [t.lock] is held.
func (t *statelessView) getProof(ctx context.Context, key []byte) (*Proof, error) {
	_, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.getProof")
	defer span.End()

	proof := &Proof{
		Key: key,
	}

	// Get the node at the given path, or the node closest to it.
	keyPath := NewPath(key)

	proofPath, err := t.getPathTo(keyPath)
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
		proof.Value = Clone(closestNode.value)
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

	childPath := closestNode.key + Path(nextIndex) + child.compressedPath
	childNode, err := t.getNodeFromParent(closestNode, childPath)
	if err != nil {
		return nil, err
	}
	proof.Path = append(proof.Path, childNode.asProofNode())
	return proof, nil
}

// Returns a proof that [bytesPath] is in or not in trie [t].
// Assumes [t.lock] is held.
func (t *statelessView) getPathProof(ctx context.Context, key Path) (*PathProof, error) {
	_, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.getPathProof")
	defer span.End()

	proof := &PathProof{
		KeyPath: key.Serialize(),
	}

	// Get the node at the given path, or the node closest to it.
	proofPath, err := t.getPathTo(key)
	if err != nil {
		return nil, err
	}

	// From root --> node from left --> right.
	proof.Path = make([]ProofNode, len(proofPath), len(proofPath)+1)
	for i, node := range proofPath {
		proof.Path[i] = node.asProofNode()
	}

	closestNode := proofPath[len(proofPath)-1]
	if closestNode.key.Compare(key) == 0 {
		// There is a node with the given [key].

		for nextIndex := byte(0); nextIndex < NodeBranchFactor; nextIndex++ {
			child, ok := closestNode.children[nextIndex]
			if !ok {
				continue
			}

			childPath := closestNode.key + Path(nextIndex) + child.compressedPath
			childNode, err := t.getNodeFromParent(closestNode, childPath)
			if err != nil {
				return nil, err
			}
			proof.Children = append(proof.Children, childNode.asProofNode())
		}

		proof.Value = Clone(closestNode.value)
		return proof, nil
	}

	// There is no node with the given [key].
	// If there is a child at the index where the node would be
	// if it existed, include that child in the proof.
	nextIndex := key[len(closestNode.key)]
	child, ok := closestNode.children[nextIndex]
	if !ok {
		return proof, nil
	}

	childPath := closestNode.key + Path(nextIndex) + child.compressedPath
	childNode, err := t.getNodeFromParent(closestNode, childPath)
	if err != nil {
		return nil, err
	}
	proof.Path = append(proof.Path, childNode.asProofNode())
	return proof, nil
}

// GetMerkleRoot returns the ID of the root of this trie.
func (t *statelessView) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.getMerkleRoot(ctx)
}

// Returns the ID of the root node of this trie.
// Assumes [t.lock] is held.
func (t *statelessView) getMerkleRoot(ctx context.Context) (ids.ID, error) {
	if err := t.calculateNodeIDs(ctx); err != nil {
		return ids.Empty, err
	}
	return t.root.id, nil
}

func (t *statelessView) GetValues(_ context.Context, keys [][]byte) ([][]byte, []error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	results := make([][]byte, len(keys))
	valueErrors := make([]error, len(keys))

	for i, key := range keys {
		results[i], valueErrors[i] = t.getValueCopy(NewPath(key), false)
	}
	return results, valueErrors
}

// GetValue returns the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
func (t *statelessView) GetValue(_ context.Context, key []byte) ([]byte, error) {
	return t.getValueCopy(NewPath(key), true)
}

// getValueCopy returns a copy of the value for the given [key].
// Returns database.ErrNotFound if it doesn't exist.
func (t *statelessView) getValueCopy(key Path, lock bool) ([]byte, error) {
	val, err := t.getValue(key, lock)
	if err != nil {
		return nil, err
	}
	return slices.Clone(val), nil
}

func (t *statelessView) getValue(key Path, lock bool) ([]byte, error) {
	if lock {
		t.lock.RLock()
		defer t.lock.RUnlock()
	}

	if change, ok := t.changes.values[key]; ok {
		t.db.metrics.ViewValueCacheHit()
		if change.after.IsNothing() {
			return nil, database.ErrNotFound
		}
		return change.after.value, nil
	}
	t.db.metrics.ViewValueCacheMiss()

	// if we don't have local copy of the key, then grab a copy from the parent trie
	value, err := t.getParentTrie().getValue(key, true /*lock*/)
	if err != nil {
		return nil, err
	}

	return value, nil
}

// Insert will upsert the key/value pair into the trie.
func (t *statelessView) Insert(_ context.Context, key []byte, value []byte) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.insert(key, value)
}

// Assumes [t.lock] is held.
// Assumes [t.validityTrackingLock] isn't held.
func (t *statelessView) insert(key []byte, value []byte) error {
	if t.committed {
		return ErrCommitted
	}

	valCopy := slices.Clone(value)
	return t.recordValueChange(NewPath(key), Some(valCopy))
}

// Remove will delete the value associated with [key] from this trie.
func (t *statelessView) Remove(_ context.Context, key []byte) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.remove(key)
}

// Assumes [t.lock] is held.
// Assumes [t.validityTrackingLock] isn't held.
func (t *statelessView) remove(key []byte) error {
	if t.committed {
		return ErrCommitted
	}

	return t.recordValueChange(NewPath(key), Nothing[[]byte]())
}

// Assumes [t.lock] is held.
func (t *statelessView) applyChangedValuesToTrie(ctx context.Context) error {
	_, span := t.db.tracer.Start(ctx, "MerkleDB.trieview.applyChangedValuesToTrie")
	defer span.End()

	unappliedValues := t.unappliedValueChanges
	t.unappliedValueChanges = make(map[Path]Maybe[[]byte], t.estimatedSize)

	for key, change := range unappliedValues {
		if change.IsNothing() {
			if err := t.removeFromTrie(key); err != nil {
				return err
			}
		} else if _, err := t.insertIntoTrie(key, change); err != nil {
			return err
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
// Assumes [t.lock] is held.
func (t *statelessView) compressNodePath(parent, node *Node) error {
	// don't collapse into this node if it's the root, doesn't have 1 child, or has a value
	if len(node.children) != 1 || node.hasValue() {
		return nil
	}

	// delete all empty nodes with a single child under [node]
	for len(node.children) == 1 && !node.hasValue() {
		if err := t.recordNodeDeleted(node); err != nil {
			return err
		}

		nextNode, err := t.getNodeFromParent(node, node.getSingleChildPath())
		if err != nil {
			return err
		}
		node = nextNode
	}

	// [node] is the first node with multiple children.
	// combine it with the [node] passed in.
	parent.addChild(node)
	return t.recordNodeChange(parent)
}

// Starting from the last node in [nodePath], traverses toward the root
// and deletes each node that has no value and no children.
// Stops when a node with a value or children is reached.
// Assumes [nodePath] is a path from the root to a node.
// Assumes [t.lock] is held.
func (t *statelessView) deleteEmptyNodes(nodePath []*Node) error {
	node := nodePath[len(nodePath)-1]
	nextParentIndex := len(nodePath) - 2

	for ; nextParentIndex >= 0 && len(node.children) == 0 && !node.hasValue(); nextParentIndex-- {
		if err := t.recordNodeDeleted(node); err != nil {
			return err
		}

		parent := nodePath[nextParentIndex]

		parent.removeChild(node)
		if err := t.recordNodeChange(parent); err != nil {
			return err
		}

		node = parent
	}

	if nextParentIndex < 0 {
		return nil
	}
	parent := nodePath[nextParentIndex]

	return t.compressNodePath(parent, node)
}

// Returns the nodes along the path to [key].
// The first node is the root, and the last node is either the node with the
// given [key], if it's in the trie, or the node with the largest prefix of
// the [key] if it isn't in the trie.
// Always returns at least the root node.
func (t *statelessView) getPathTo(key Path) ([]*Node, error) {
	var (
		// all paths start at the root
		currentNode     = t.root
		matchedKeyIndex = 0
		nodes           = []*Node{t.root}
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
		currentNode, err = t.getNodeWithID(nextChildEntry.id, key[:matchedKeyIndex])
		if err != nil {
			return nil, err
		}

		// add node to path
		nodes = append(nodes, currentNode)
	}
	return nodes, nil
}

// Get a copy of the node matching the passed key from the trie
// Used by views to get nodes from their ancestors
// assumes that [t.needsRecalculation] is false
func (t *statelessView) getEditableNode(key Path) (*Node, error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	// grab the node in question
	n, err := t.getNodeWithID(ids.Empty, key)
	if err != nil {
		return nil, err
	}

	// return a clone of the node, so it can be edited without affecting this trie
	return n.clone(), nil
}

// Inserts a key/value pair into the trie.
// Assumes [t.lock] is held.
func (t *statelessView) insertIntoTrie(
	key Path,
	value Maybe[[]byte],
) (*Node, error) {
	// find the node that most closely matches [key]
	pathToNode, err := t.getPathTo(key)
	if err != nil {
		return nil, err
	}

	// We're inserting a node whose ancestry is [pathToNode]
	// so we'll need to recalculate their IDs.
	for _, node := range pathToNode {
		if err := t.recordNodeChange(node); err != nil {
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
		return newNode, t.recordNodeChange(newNode)
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
	if err := t.recordNodeChange(closestNode); err != nil {
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
		if err := t.recordNodeChange(newNode); err != nil {
			return nil, err
		}
		nodeWithValue = newNode
	}

	existingChildKey := key[:closestNodeKeyLength+1] + existingChildEntry.compressedPath

	// the existing child's key is of length: len(closestNodeKey) + 1 for the child index + len(existing child's compressed key)
	// if that length is less than or equal to the branch node's key that implies that the existing child's key matched the key to be inserted
	// since it matched the key to be inserted, it should have been returned by GetPathTo
	if len(existingChildKey) <= len(branchNode.key) {
		return nil, ErrGetPathToFailure
	}

	branchNode.addChildWithoutNode(
		existingChildKey[len(branchNode.key)],
		existingChildKey[len(branchNode.key)+1:],
		existingChildEntry.id,
	)

	return nodeWithValue, t.recordNodeChange(branchNode)
}

// Records that a node has been changed.
// Assumes [t.lock] is held.
func (t *statelessView) recordNodeChange(after *Node) error {
	return t.recordKeyChange(after.key, after)
}

// Records that the node associated with the given key has been deleted.
// Assumes [t.lock] is held.
func (t *statelessView) recordNodeDeleted(after *Node) error {
	// don't delete the root.
	if len(after.key) == 0 {
		return t.recordKeyChange(after.key, after)
	}
	return t.recordKeyChange(after.key, nil)
}

// Records that the node associated with the given key has been changed.
// Assumes [t.lock] is held.
func (t *statelessView) recordKeyChange(key Path, after *Node) error {
	t.needsRecalculation = true

	if existing, ok := t.changes.nodes[key]; ok {
		existing.after = after
		return nil
	}

	before, err := t.getParentTrie().getEditableNode(key)
	if err != nil {
		if err != database.ErrNotFound {
			return err
		}
		before = nil
	}

	t.changes.nodes[key] = &change[*Node]{
		before: before,
		after:  after,
	}
	return nil
}

// Records that a key's value has been added or updated.
// Doesn't actually change the trie data structure.
// That's deferred until we calculate node IDs.
// Assumes [t.lock] is held.
func (t *statelessView) recordValueChange(key Path, value Maybe[[]byte]) error {
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
	before, err := t.getParentTrie().getValue(key, true /*lock*/)
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
// Assumes [t.lock] write lock is held.
func (t *statelessView) removeFromTrie(key Path) error {
	nodePath, err := t.getPathTo(key)
	if err != nil {
		return err
	}

	nodeToDelete := nodePath[len(nodePath)-1]

	if nodeToDelete.key.Compare(key) != 0 || !nodeToDelete.hasValue() {
		// the key wasn't in the trie or doesn't have a value so there's nothing to do
		return nil
	}

	// A node with ancestry [nodePath] is being deleted, so we need to recalculate
	// all the nodes in this path.
	for _, node := range nodePath {
		if err := t.recordNodeChange(node); err != nil {
			return err
		}
	}

	nodeToDelete.setValue(Nothing[[]byte]())
	if err := t.recordNodeChange(nodeToDelete); err != nil {
		return err
	}

	// if the removed node has no children, the node can be removed from the trie
	if len(nodeToDelete.children) == 0 {
		return t.deleteEmptyNodes(nodePath)
	}

	if len(nodePath) == 1 {
		return nil
	}
	parent := nodePath[len(nodePath)-2]

	// merge this node and its descendants into a single node if possible
	return t.compressNodePath(parent, nodeToDelete)
}

// Retrieves the node with the given [key], which is a child of [parent], and
// uses the [parent] node to initialize the child node's ID.
// Returns database.ErrNotFound if the child doesn't exist.
// Assumes [t.lock] write or read lock is held.
func (t *statelessView) getNodeFromParent(parent *Node, key Path) (*Node, error) {
	// confirm the child exists and get its ID before attempting to load it
	if child, exists := parent.children[key[len(parent.key)]]; exists {
		return t.getNodeWithID(child.id, key)
	}

	return nil, database.ErrNotFound
}

// Retrieves a node with the given [key].
// If the node is fetched from [t.parentTrie] and [id] isn't empty,
// sets the node's ID to [id].
// Returns database.ErrNotFound if the node doesn't exist.
// Assumes [t.lock] write or read lock is held.
func (t *statelessView) getNodeWithID(id ids.ID, key Path) (*Node, error) {
	// check for the key within the changed nodes
	if nodeChange, isChanged := t.changes.nodes[key]; isChanged {
		t.db.metrics.ViewNodeCacheHit()
		if nodeChange.after == nil {
			return nil, database.ErrNotFound
		}
		return nodeChange.after, nil
	}

	// get the node from the parent trie and store a local copy
	parentTrieNode, err := t.getParentTrie().getEditableNode(key)
	if err != nil {
		return nil, err
	}

	// only need to initialize the id if it's from the parent trie.
	// nodes in the current view change list have already been initialized.
	if id != ids.Empty {
		parentTrieNode.id = id
	}
	return parentTrieNode, nil
}

// Get the parent trie of the view
func (t *statelessView) getParentTrie() StatelessView {
	verifierIntercepter := *t.verifierIntercepter
	verifierIntercepter.StatelessView = t.parentTrie
	return &verifierIntercepter
}
