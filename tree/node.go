package tree

type Node interface {
	GetChild(key []Unit) Node
	Insert(key []Unit, value []byte)
	SetChild(node Node)
	Print()
	Value() []byte
	Delete(key []Unit) bool
	Key() []Unit
	SetParent(b Node)
}
