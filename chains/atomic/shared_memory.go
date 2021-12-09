// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
)

var _ SharedMemory = &sharedMemory{}

type Requests struct {
	RemoveRequests [][]byte   `serialize:"true"`
	PutRequests    []*Element `serialize:"true"`

	peerChainID ids.ID
}

type Element struct {
	Key    []byte   `serialize:"true"`
	Value  []byte   `serialize:"true"`
	Traits [][]byte `serialize:"true"`
}

type SharedMemory interface {
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
	Apply(requests map[ids.ID]*Requests, batches ...database.Batch) error
}

// sharedMemory provides the API for a blockchain to interact with shared memory
// of another blockchain
type sharedMemory struct {
	m           *Memory
	thisChainID ids.ID
}

func (sm *sharedMemory) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	sharedID := sm.m.sharedID(peerChainID, sm.thisChainID)
	db := sm.m.GetSharedDatabase(sm.m.db, sharedID)
	defer sm.m.ReleaseSharedDatabase(sharedID)

	s := state{
		c:       sm.m.codec,
		valueDB: inbound.getValueDB(sm.thisChainID, peerChainID, db),
	}

	values := make([][]byte, len(keys))
	for i, key := range keys {
		elem, err := s.Value(key)
		if err != nil {
			return nil, err
		}
		values[i] = elem.Value
	}
	return values, nil
}

func (sm *sharedMemory) Indexed(
	peerChainID ids.ID,
	traits [][]byte,
	startTrait,
	startKey []byte,
	limit int,
) ([][]byte, []byte, []byte, error) {
	sharedID := sm.m.sharedID(peerChainID, sm.thisChainID)
	db := sm.m.GetSharedDatabase(sm.m.db, sharedID)
	defer sm.m.ReleaseSharedDatabase(sharedID)

	s := state{
		c: sm.m.codec,
	}
	s.valueDB, s.indexDB = inbound.getValueAndIndexDB(sm.thisChainID, peerChainID, db)

	keys, lastTrait, lastKey, err := s.getKeys(traits, startTrait, startKey, limit)
	if err != nil {
		return nil, nil, nil, err
	}

	values := make([][]byte, len(keys))
	for i, key := range keys {
		elem, err := s.Value(key)
		if err != nil {
			return nil, nil, nil, err
		}
		values[i] = elem.Value
	}
	return values, lastTrait, lastKey, nil
}

func (sm *sharedMemory) Apply(requests map[ids.ID]*Requests, batches ...database.Batch) error {
	// Sorting here introduces an ordering over the locks to prevent any
	// deadlocks
	sharedIDs := make([]ids.ID, 0, len(requests))
	sharedOperations := make(map[ids.ID]*Requests, len(requests))
	for peerChainID, request := range requests {
		sharedID := sm.m.sharedID(sm.thisChainID, peerChainID)
		sharedIDs = append(sharedIDs, sharedID)

		request.peerChainID = peerChainID
		sharedOperations[sharedID] = request
	}
	ids.SortIDs(sharedIDs)

	// Make sure all operations are committed atomically
	vdb := versiondb.New(sm.m.db)

	for _, sharedID := range sharedIDs {
		req := sharedOperations[sharedID]

		db := sm.m.GetSharedDatabase(vdb, sharedID)
		defer sm.m.ReleaseSharedDatabase(sharedID)

		s := state{
			c: sm.m.codec,
		}

		s.valueDB, s.indexDB = inbound.getValueAndIndexDB(sm.thisChainID, req.peerChainID, db)
		for _, removeRequest := range req.RemoveRequests {
			if err := s.RemoveValue(removeRequest); err != nil {
				return err
			}
		}

		s.valueDB, s.indexDB = outbound.getValueAndIndexDB(sm.thisChainID, req.peerChainID, db)
		for _, putRequest := range req.PutRequests {
			if err := s.SetValue(putRequest); err != nil {
				return err
			}
		}
	}

	batch, err := vdb.CommitBatch()
	if err != nil {
		return err
	}

	return WriteAll(batch, batches...)
}
