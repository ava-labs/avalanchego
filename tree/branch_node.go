package tree

import "fmt"

// BranchNode
// [ 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, a, b, c, d, f ]
// [ x ] -> LeafNode
// [ not found ] -> EmptyNode
//
type BranchNode struct {
	nodes         map[Unit]Node // TODO this can probably be an array if it help ?
	sharedAddress []Unit
	parent        Node
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
// returns EmptyNode - no node in this position
// or returns a LeafNode - end node belongs in this branch
// or return a BranchNode - it has a shared addresses with this branch
//
func (b *BranchNode) GetChild(key []Unit) Node {
	// key - ABCDE
	// b.sharedAddress - AC
	// SharedPrefix() - A != b.sharedAddress
	// if the node CAN'T exist in this prefix return an EmptyNode with the SharedAddress
	if !EqualUnits(SharedPrefix(b.sharedAddress, key), b.sharedAddress) {
		return NewEmptyNode(b, key, b.sharedAddress)
	}

	// if the node CAN exist in this prefix return an EmptyNode with no SharedAddress
	node := b.nodes[FirstNonPrefix(b.sharedAddress, key)]
	if node == nil {
		return NewEmptyNode(b, key, nil)
	}

	return node
}

// Insert a LeafNode
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

	b.nodes[FirstNonPrefix(b.sharedAddress, key)] = NewLeafNode(key, value, b)
}

// SetChild
func (b *BranchNode) SetChild(node Node) {
	if len(b.nodes) == 1 {
		b.parent.SetChild(node)
		return
	}

	node.SetParent(b)
	b.nodes[FirstNonPrefix(b.sharedAddress, node.Key())] = node
}

func (b *BranchNode) Parent() Node {
	return b.parent
}

func (b *BranchNode) SetParent(node Node) {
	b.parent = node
}

func (b *BranchNode) Print() {
	fmt.Printf("Branch ID: %p - SharedAddress: %v - Parent: %p \n\t↪ Nodes: %v \n", b, b.sharedAddress, b.parent, b.nodes)
	for _, node := range b.nodes {
		node.Print()
	}
}

func (b *BranchNode) Link(address []Unit, node Node) {
	b.nodes[FirstNonPrefix(b.sharedAddress, address)] = node
}

func (b *BranchNode) Value() []byte {
	return nil
}

// Delete
// the delete request comes from a child node always (upwards direction)
// if the child is a leafNode, it requests the parent the deletion
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

func (b *BranchNode) Key() []Unit {
	return b.sharedAddress
}
