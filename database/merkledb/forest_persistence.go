package merkledb

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/codec/linearcodec"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
)

// ForestPersistence holds the DB + the RootNode
// Its a persistence layer that can be used by a forest to have multiple merkle radix trees
// it ensures the reference counting of the objects storing one Node just one time
// even when referred multiple times on multiple trees
type ForestPersistence struct {
	db    database.Database
	codec codec.Manager
}

// NewForestPersistence creates a new Persistence
func NewForestPersistence(db database.Database) (Persistence, error) {
	c := linearcodec.NewDefault()
	_ = c.RegisterType(&BranchNode{})
	_ = c.RegisterType(&LeafNode{})
	_ = c.RegisterType(&RootNode{})
	codecManager := codec.NewDefaultManager()
	if err := codecManager.RegisterCodec(0, c); err != nil {
		return nil, err
	}

	persistence := ForestPersistence{
		db:    versiondb.New(db),
		codec: codecManager,
	}

	return &persistence, nil
}

// GetNodeByHash fetches a Node given its hash
func (fp *ForestPersistence) GetNodeByHash(nodeHash []byte) (Node, error) {
	nodeBytes, err := fp.db.Get(nodeHash)
	if err != nil {
		return nil, err
	}

	var node Node
	_, err = fp.codec.Unmarshal(nodeBytes, &node)
	node.SetPersistence(fp)

	return node, err
}

// GetRootNode returns the RootNode
// the RootNodes have a fixed Hash->Key as they'll need to be referenced ad-hoc
func (fp *ForestPersistence) GetRootNode(rootNodeID uint32) (Node, error) {
	return fp.GetNodeByHash(genRootNodeID(rootNodeID))
}

// NewRoot creates a new Root and returns it - fails it it already exists
func (fp *ForestPersistence) NewRoot(rootNodeID uint32) (Node, error) {
	_, err := fp.GetRootNode(rootNodeID)
	if err == database.ErrNotFound {
		newRoot := NewRootNode(rootNodeID, fp)
		return newRoot, nil
	} else if err != nil {
		return nil, err
	}

	return nil, fmt.Errorf("root already exists")
}

// DuplicateRoot creates a new RootNode setting its child to point at the oldRootID
func (fp *ForestPersistence) DuplicateRoot(oldRootID uint32, newRootID uint32) (Node, error) {

	oldRoot, err := fp.GetRootNode(oldRootID)
	if err != nil {
		return nil, err
	}

	newRoot, err := fp.NewRoot(newRootID)
	if err != nil {
		return nil, err
	}

	oldRootChild, err := oldRoot.GetChild(Key{})
	if err != nil {
		return nil, err
	}

	oldRootChild.References(1)

	err = newRoot.SetChild(oldRootChild)
	if err != nil {
		return nil, err
	}

	err = fp.StoreNode(oldRootChild, true)
	if err != nil {
		return nil, err
	}

	err = fp.StoreNode(newRoot, true)
	if err != nil {
		return nil, err
	}

	return newRoot, nil
}

// StoreNode stores a node in the ForestPersistence
//
// For BranchNode and LeafNode increment children and increment current node references depending on
// where the node is positioned in the tree (decideReferenceChanges)
//
// While incrementChildren is the same for DeleteNode and StoreNode, decrementing previous version of the node
// is done differently in a Delete and Store actions
func (fp *ForestPersistence) StoreNode(n Node, force bool) error {
	if force {
		return fp.storeNode(n)
	}

	// before storing lets take care of the leftover nodes
	// and ensure the reference counting is correct
	err := fp.refCountStoreNode(n)
	if err != nil {
		return err
	}

	// if a node with the same hash already exists increment the refs
	// and decrement the references of it's children
	_, err = fp.GetNodeByHash(n.GetHash())
	if err == nil {
		n.References(1)
		for _, childHash := range n.GetChildrenHashes() {
			childNode, err := fp.GetNodeByHash(childHash)
			if err != nil {
				return err
			}
			if childNode.References(-1) > 0 {
				err := fp.storeNode(childNode)
				if err != nil {
					return err
				}
			}
		}
	} else {
		if err == database.ErrNotFound {
			return fp.storeNode(n)
		}
		return err
	}

	return fp.storeNode(n)
}

// DeleteNode deletes a node from the ForestPersistence
//
// For BranchNode and LeafNode increment children and decrement current node references depending on
// where the node is positioned in the tree (decideReferenceChanges)
//
// While incrementChildren is the same for DeleteNode and StoreNode, decrementing previous version of the node
// is done differently in a Delete and Store actions
func (fp *ForestPersistence) DeleteNode(n Node) error {

	switch n.(type) {
	case *RootNode:
		// Deleting a RootNode never takes into account reference counting
		// the RootNode is just a Node that holds the first Node of the Tree
		return fp.deleteNode(n)
	case *BranchNode, *LeafNode:
		prevNode, err := fp.GetNodeByHash(n.GetHash())
		if err != nil {
			return err
		}

		// The previous Node is the current Node for the sake of reference counting
		// however when making changes we will need to modify the previous version of the node
		// to guarantee that other nodes pointing to the previous version are not affected by this nodes changes
		prevNodeRefs := prevNode.References(0)
		incrementChildren, decrementOldNode := decideReferenceChanges(n, prevNode)

		if incrementChildren {
			err = fp.incrementChildren(n, prevNode)
			if err != nil {
				return err
			}
		}

		if decrementOldNode {
			prevNodeRefs = prevNode.References(-1)

			if prevNodeRefs == 0 {
				return fp.deleteNode(n)
			}

			// otherwise store the new references on an unchanged copy of the previous version of the node
			// this ensures that we are modifiying the copy that other nodes are referencing
			copyNode, err := fp.GetNodeByHash(n.GetHash())
			if err != nil {
				return err
			}
			copyNode.References(-1)

			return fp.storeNode(copyNode)
		}

	default:
		return fmt.Errorf("unexpected type in DeleteNode")
	}

	return nil
}

// Commit commits any pending nodes in the underlying db
func (fp *ForestPersistence) Commit(err error) error {
	if err != nil {
		fp.db.(*versiondb.Database).Abort()
		return err
	}
	return fp.db.(*versiondb.Database).Commit()
}

func (fp *ForestPersistence) storeNode(n Node) error {
	nBytes, err := fp.codec.Marshal(0, &n)
	if err != nil {
		return err
	}

	switch node := n.(type) {
	case *RootNode:
		err = fp.db.Put(genRootNodeID(node.RootID), nBytes)
		if err != nil {
			return err
		}
		return nil
	default:
		err = fp.db.Put(n.GetHash(), nBytes)
		if err != nil {
			return err
		}
	}

	return nil
}

func (fp *ForestPersistence) deleteNode(n Node) error {
	var hash []byte
	switch node := n.(type) {
	case *RootNode:
		hash = genRootNodeID(node.RootID)
	default:
		hash = n.GetHash()
	}

	return fp.db.Delete(hash)
}

func (fp *ForestPersistence) refCountStoreNode(n Node) error {

	previousID := n.GetPreviousHash()
	if len(previousID) != 0 {

		prevNode, err := fp.GetNodeByHash(previousID)
		if err != nil {
			return err
		}

		prevNodeRefs := prevNode.References(0)
		incrementChildren, decrementOldNode := decideReferenceChanges(n, prevNode)

		if incrementChildren {
			err = fp.incrementChildren(n, prevNode)
		}

		if decrementOldNode {
			prevNodeRefs = prevNode.References(-1)

			if prevNodeRefs == 0 {
				return fp.deleteNode(prevNode)
			}

			// otherwise store the new references on this version of the node
			return fp.storeNode(prevNode)
		}
	}

	return nil
}

func decideReferenceChanges(n Node, prevNode Node) (bool, bool) {
	var incrementChildren, decrementOldNode bool

	pivotWasReached := n.PivotPoint().reached

	switch {
	case !pivotWasReached:
		// a new branch will be created
		// remove the old version of the node
		decrementOldNode = true

	case pivotWasReached && bytes.Equal(n.PivotPoint().hash, prevNode.GetHash()): // pivot was reached and this is the pivot node
		// a new branch will be created
		// remove the old version of the node - or decrement the existing refs in the old node
		decrementOldNode = true

		// mark the pivot as passed so that future changes now that
		// and allow for changes in the children
		n.PivotPoint().passed = true

		// since the pivot was reached
		// increment the children refs of the new branch
		incrementChildren = true

	case pivotWasReached && !n.PivotPoint().passed:
		// since the pivot was reached downwards and we have not yet reached the pass upwards
		// increment the children refs of the new branch but don't change its references
		incrementChildren = true

	case n.References(0) > 1:
		// the pivot was reached downwards and we have passed the pivot upwards
		// only increment the children if the ref count is > 1 ( as this is a new node)
		// and because its a new node, remove the old version of the node - or decrement the existing refs in the old node
		incrementChildren = true
		decrementOldNode = true

	case n.References(0) == 1:
		// the pivot was reached downwards and we have passed the pivot upwards
		// since the ref count is == 1
		// and because its a new node, remove the old version of the node
		// but don't increment the children
		decrementOldNode = true

	default:
		panic("these should never be zero here")
	}
	return incrementChildren, decrementOldNode
}

func (fp *ForestPersistence) incrementChildren(n Node, prevNode Node) error {
	// increment all children in the newNode that the previous node
	// also contain - these children are referred by other roots
	prevNodeChildren := prevNode.GetChildrenHashes()
	for _, newChildHash := range n.GetChildrenHashes() {
		if contains(prevNodeChildren, newChildHash) {
			newChild, err := fp.GetNodeByHash(newChildHash)
			if err != nil {
				return err
			}
			newChild.References(1)

			err = fp.storeNode(newChild)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func contains(s [][]byte, e []byte) bool {
	for _, a := range s {
		if bytes.Equal(a, e) {
			return true
		}
	}
	return false
}
