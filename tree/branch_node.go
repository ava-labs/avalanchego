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
	nodes         map[Unit]Node // TODO this can probably be an array if it help ?
	sharedAddress []Unit
	parent        Node
	hash          []byte
}

// NewBranchNode returns a new BranchNode
func NewBranchNode(sharedAddress []Unit, parent Node) Node {
	return &BranchNode{
		sharedAddress: sharedAddress,
		nodes:         map[Unit]Node{},
		parent:        parent,
	}
}

// GetChild returns a child node
// returns Node - one of it's children
// if there isn't one
// it returns EmptyNode - no node in this position
//
func (b *BranchNode) GetChild(key []Unit) Node {
	// key - ABCDE
	// b.sharedAddress - AC
	// SharedPrefix() - A != b.sharedAddress
	// if the node CAN'T exist in this prefix return an EmptyNode
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
	if node, ok := b.nodes[FirstNonPrefix(b.sharedAddress, key)]; ok {
		newBranch := NewBranchNode(SharedPrefix(node.Key(), key), b)
		newBranch.SetChild(node)
		newBranch.Insert(key, value)

		b.nodes[FirstNonPrefix(b.sharedAddress, key)] = newBranch
		return
	}

	// all good, insert a LeafNode
	b.nodes[FirstNonPrefix(b.sharedAddress, key)] = NewLeafNode(key, value, b)
}

// Delete
// the delete request comes from a child node always (upwards direction)
// if the child is a LeafNode, it requests the parent the deletion
// if the child is a BranchNode,
//     it either deletes + updates the tree
//     or if there's no nodes left it deletes + requests parent the deletion
//
func (b *BranchNode) Delete(key []Unit) bool {
	if _, ok := b.nodes[FirstNonPrefix(b.sharedAddress, key)]; ok {
		// the child node that called the delete
		// is either a LeafNode or an empty BranchNode
		delete(b.nodes, FirstNonPrefix(b.sharedAddress, key))

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
		hashSet = append(hashSet, child.GetHash())

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
		node.Print()
	}
}
