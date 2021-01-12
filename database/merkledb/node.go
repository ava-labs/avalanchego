package merkledb

import (
	"golang.org/x/crypto/sha3"
)

type Node interface {
	GetChild(key []Unit) (Node, error)
	GetNextNode(prefix []Unit, start []Unit, key []Unit) (Node, error)
	Insert(key []Unit, value []byte) error
	Delete(key []Unit) bool
	SetChild(node Node) error
	SetParent(b Node) error
	Value() []byte
	Hash(key []Unit, hash []byte) error
	GetHash() []byte
	Key() []Unit
	StorageKey() []Unit
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
