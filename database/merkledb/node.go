package merkledb

import (
	"github.com/ava-labs/avalanchego/utils/hashing"
)

type Node interface {
	GetChild(key []Unit) (Node, error)
	GetNextNode(prefix []Unit, start []Unit, key []Unit) (Node, error)
	Insert(key []Unit, value []byte) error
	Delete(key []Unit) error
	SetChild(node Node) error
	SetParent(b Node)
	SetPersistence(p *Persistence)
	Value() []byte
	Hash(key []Unit, hash []byte) error
	GetHash() []byte
	GetPreviousHash() []byte
	Key() []Unit
	Print()
}

func Hash(bs ...[]byte) []byte {
	hash := hashing.ByteArraysToHash256Array(bs...)
	return hash[:]

}
