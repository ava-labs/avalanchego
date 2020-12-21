package tree

import "fmt"

// BranchNode represents a Node with an array of nodes and a sharedAddress
// sharedAddress - part of the address that it links to
// nodes are links to other nodes, which their addresses are suffixed by sharedAddress
// [ 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, a, b, c, d, f ]
//     [ x ] -> LeafNode
//     [ not found ] -> EmptyNode
//
type BranchNode struct {
	nodes         [UnitSize]Node
	sharedAddress []Unit
	parent        Node
	hash          []byte
}

// NewBranchNode returns a new BranchNode
func NewBranchNode(sharedAddress []Unit, parent Node) Node {
	return &BranchNode{
		sharedAddress: sharedAddress,
		nodes:         [UnitSize]Node{},
		parent:        parent,
	}
}

// GetChild returns a child node
// returns Node - one of it's children
// if there isn't one
// it returns EmptyNode - no node in this position
//
func (b *BranchNode) GetChild(key []Unit) Node {
	if !EqualUnits(SharedPrefix(b.sharedAddress, key), b.sharedAddress) {
		return NewEmptyNode(b, key)
	}

	// if the node CAN exist in this prefix but doesn't return an EmptyNode
	node := b.nodes[FirstNonPrefix(b.sharedAddress, key)]
	if node == nil {
		return NewEmptyNode(b, key)
	}

	return node
}

// GetNextNode returns the next node in increasing key order
// returns Node - one of it's children
//
func (b *BranchNode) GetNextNode(key []Unit) Node {

	// return the first, left-most child
	if key == nil {
		for _, node := range b.nodes {
			if node != nil {
				return node
			}
		}
		return NewEmptyNode(b, key)
	}

	if !EqualUnits(SharedPrefix(b.sharedAddress, key), b.sharedAddress) {
		return NewEmptyNode(b, key)
	}

	// search the next node after the address one
	for _, node := range b.nodes[FirstNonPrefix(b.sharedAddress, key):] {
		if node != nil {
			// TODO Think theres a better way of doing this
			if EqualUnits(node.Key(), key) {
				continue
			}
			return node
		}
	}
	// if the node CAN exist in this sharedAddress but doesn't, return an EmptyNode
	return NewEmptyNode(b, key)
}

// Insert adds a new node in the branch
// if the node doesn't belong in the branch it inserts in the parent
// if there's already a node on that position it creates a BranchNode and branches the position out
// otherwise just inserts a LeafNode
func (b *BranchNode) Insert(key []Unit, value []byte) {

	// if the node CAN'T exist in this prefix request the parent to insert
	if !EqualUnits(SharedPrefix(b.sharedAddress, key), b.sharedAddress) {
		b.parent.Insert(key, value)
		return
	}

	// if the node already exists then it's a new suffixed address
	// needs a new branchNode
	if node := b.nodes[FirstNonPrefix(b.sharedAddress, key)]; node != nil {
		newBranch := NewBranchNode(SharedPrefix(node.Key(), key), b)

		newBranch.SetChild(node)
		newBranch.Insert(key, value)

		b.nodes[FirstNonPrefix(b.sharedAddress, key)] = newBranch

		// we inserted a new BranchNode - rehash the Branch
		newBranch.Hash()
		return
	}

	// all good, insert a LeafNode
	newLeafNode := NewLeafNode(key, value, b)
	b.nodes[FirstNonPrefix(b.sharedAddress, key)] = newLeafNode

	// we inserted a new LeafNode - rehash the Branch
	newLeafNode.Hash()
}

// Delete
// the delete request comes from a child node always (upwards direction)
// if the child is a LeafNode, it requests the parent the deletion
// if the child is a BranchNode,
//     it either deletes + updates the tree
//     or if there's no nodes left it deletes + requests parent the deletion
//
func (b *BranchNode) Delete(key []Unit) bool {

	if node := b.nodes[FirstNonPrefix(b.sharedAddress, key)]; node != nil {
		// the child node that called the delete
		// is either a LeafNode or an empty BranchNode
		b.nodes[FirstNonPrefix(b.sharedAddress, key)] = nil

		// if the current BranchNode has no children
		// delete if from the parent
		if len(b.nodes) == 0 {
			return b.parent.Delete(b.sharedAddress)
		}
		// if the current BranchNode has one children
		// request the parent to take it
		if len(b.nodes) == 1 {
			var singleNode Node
			for _, v := range b.nodes {
				singleNode = v
			}
			b.parent.SetChild(singleNode)
		}
		return true
	}

	return false
}

// SetChild force sets a child in the BranchNode
// if the BranchNode only has one node we can link the parent to the child and skip this node
func (b *BranchNode) SetChild(node Node) {
	if len(b.nodes) == 1 {
		b.parent.SetChild(node)
		return
	}

	node.SetParent(b)
	b.nodes[FirstNonPrefix(b.sharedAddress, node.Key())] = node
}

// SetParent force sets the parent
func (b *BranchNode) SetParent(node Node) {
	b.parent = node
}

// Value not used
func (b *BranchNode) Value() []byte { return nil }

func (b *BranchNode) Hash() {
	hashSet := [][]byte{
		ToBytes(b.sharedAddress),
	}
	for _, child := range b.nodes {
		if child != nil {
			hashSet = append(hashSet, child.GetHash())
		}
	}
	b.hash = Hash(hashSet...)
	b.parent.Hash()
}

func (b *BranchNode) GetHash() []byte {
	return b.hash
}

// Key returns the BranchNode sharedAddress
func (b *BranchNode) Key() []Unit {
	return b.sharedAddress
}

// Print prints the node
func (b *BranchNode) Print() {
	fmt.Printf("Branch ID: %p - SharedAddress: %v - Parent: %p \n\t↪ Nodes: %v \n", b, b.sharedAddress, b.parent, b.nodes)
	for _, node := range b.nodes {
		if node != nil {
			node.Print()
		}
	}
}
