// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"github.com/ava-labs/gecko/ids"
)

// Element ...
type Element struct {
	Key    []byte
	Value  []byte
	Traits [][]byte
}

// SharedMemory ...
type SharedMemory interface {
	// Adds to the peer chain's side
	Put(peerChainID ids.ID, elems []*Element) error

	// Fetches from this chain's side
	Get(peerChainID ids.ID, keys [][]byte) (values [][]byte, err error)
	Indexed(
		peerChainID ids.ID,
		traits [][]byte,
		startTrait,
		startKey []byte,
		limit int,
	) (
		values [][]byte,
		lastTrait,
		lastKey []byte,
		err error,
	)
	Remove(peerChainID ids.ID, keys [][]byte) error
}

// sharedMemory provides the API for a blockchain to interact with shared memory
// of another blockchain
type sharedMemory struct {
	m           *Memory
	thisChainID ids.ID
}

func (bm *sharedMemory) Put(peerChainID ids.ID, elems []*Element) error {
	sharedID := bm.m.sharedID(peerChainID, bm.thisChainID)
	db := bm.m.GetDatabase(sharedID)
	defer bm.m.ReleaseDatabase(sharedID)

	return nil
}

func (bm *sharedMemory) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	sharedID := bm.m.sharedID(peerChainID, bm.thisChainID)
	db := bm.m.GetDatabase(sharedID)
	defer bm.m.ReleaseDatabase(sharedID)

	return nil, nil
}

func (bm *sharedMemory) Indexed(
	peerChainID ids.ID,
	traits [][]byte,
	startTrait,
	startKey []byte,
	limit int,
) (
	values [][]byte,
	lastTrait,
	lastKey []byte,
	err error,
) {
	sharedID := bm.m.sharedID(peerChainID, bm.thisChainID)
	db := bm.m.GetDatabase(sharedID)
	defer bm.m.ReleaseDatabase(sharedID)

	return nil, nil, nil, nil
}

func (bm *sharedMemory) Remove(peerChainID ids.ID, keys [][]byte) error {
	sharedID := bm.m.sharedID(peerChainID, bm.thisChainID)
	db := bm.m.GetDatabase(sharedID)
	defer bm.m.ReleaseDatabase(sharedID)

	return nil
}
