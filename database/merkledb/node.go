package merkledb

import (
	"github.com/ava-labs/avalanchego/utils/hashing"
)

type Node interface {
	GetChild(key Key) (Node, error)
	GetNextNode(prefix Key, start Key, key Key) (Node, error)
	Insert(key Key, value []byte) error
	Delete(key Key) error
	SetChild(node Node) error
	SetParent(b Node)
	SetPersistence(p *Persistence)
	Value() []byte
	Hash(key Key, hash []byte) error
	GetHash() []byte
	GetPreviousHash() []byte
	References(change int32) int32
	Key() Key
	Print()
}

func Hash(bs ...[]byte) []byte {
	hash := hashing.ByteArraysToHash256Array(bs...)
	return hash[:]

}
