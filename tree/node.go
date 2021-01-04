package tree

import "golang.org/x/crypto/sha3"

type Node interface {
	GetChild(key []Unit) Node
	GetNextNode(prefix []Unit, start []Unit, key []Unit) Node
	Insert(key []Unit, value []byte)
	Delete(key []Unit) bool
	SetChild(node Node)
	SetParent(b Node)
	Value() []byte
	Hash()
	GetHash() []byte
	Key() []Unit
	Print()
}

func Hash(bs ...[]byte) []byte {
	// A MAC with 32 bytes of output has 256-bit security strength -- if you use at least a 32-byte-long key.
	h := make([]byte, 32)
	d := sha3.NewShake256()
	for _, entry := range bs {
		_, _ = d.Write(entry)
	}
	_, _ = d.Read(h)

	return h

}
