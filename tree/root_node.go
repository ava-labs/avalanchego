package tree

import "fmt"

type RootNode struct {
	child Node
}

func NewRootNode(child Node) Node {
	return &RootNode{}
}

func (r *RootNode) Parent() Node {
	return nil
}

func (r *RootNode) GetNode(key []Unit) Node {
	return nil
}

func (r *RootNode) GetChild(key []Unit) Node {
	return r.child
}

func (r *RootNode) SetChild(node Node) {
	r.child = node
}

func (r *RootNode) Insert(key []Unit, value []byte) {
}

func (r *RootNode) Print() {
	fmt.Printf("Root ID: %p - Child: %p \n", r, r.child)
	if r.child != nil {
		r.child.Print()
	}
}

func (r *RootNode) Link(address []Unit, node Node) {}

func (r *RootNode) Value() []byte { return nil }

func (r *RootNode) Delete(key []Unit) bool {
	r.child = nil
	return true
}
func (r *RootNode) Key() []Unit { return nil }

func (r *RootNode) SetParent(node Node) {}
