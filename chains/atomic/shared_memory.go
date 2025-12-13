// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

var (
	_ SharedMemory = (*sharedMemory)(nil)
	_ SharedMemory = (*ReadOnly)(nil)
)

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
	// Get fetches the values corresponding to [keys] that have been sent from
	// [peerChainID]
	//
	// Invariant: Get guarantees that the resulting values array is the same
	//            length as keys.
	Get(peerChainID ids.ID, keys [][]byte) (values [][]byte, err error)
	// Indexed returns a paginated result of values that possess any of the
	// given traits and were sent from [peerChainID].
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
	// Apply performs the requested set of operations by atomically applying
	// [requests] to their respective chainID keys in the map along with the
	// batches on the underlying DB.
	//
	// Invariant: The underlying database of [batches] must be the same as the
	//            underlying database for SharedMemory.
	Apply(requests map[ids.ID]*Requests, batches ...database.Batch) error
}

// sharedMemory provides the API for a blockchain to interact with shared memory
// of another blockchain
type sharedMemory struct {
	m           *Memory
	thisChainID ids.ID
}

func (sm *sharedMemory) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	sharedID := sharedID(peerChainID, sm.thisChainID)
	db := sm.m.GetSharedDatabase(sm.m.db, sharedID)
	defer sm.m.ReleaseSharedDatabase(sharedID)

	s := state{
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
	sharedID := sharedID(peerChainID, sm.thisChainID)
	db := sm.m.GetSharedDatabase(sm.m.db, sharedID)
	defer sm.m.ReleaseSharedDatabase(sharedID)

	s := state{}
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
		sharedID := sharedID(sm.thisChainID, peerChainID)
		sharedIDs = append(sharedIDs, sharedID)

		request.peerChainID = peerChainID
		sharedOperations[sharedID] = request
	}
	utils.Sort(sharedIDs)

	// Make sure all operations are committed atomically
	vdb := versiondb.New(sm.m.db)

	for _, sharedID := range sharedIDs {
		req := sharedOperations[sharedID]

		db := sm.m.GetSharedDatabase(vdb, sharedID)
		defer sm.m.ReleaseSharedDatabase(sharedID)

		s := state{}

		// Perform any remove requests on the inbound database
		s.valueDB, s.indexDB = inbound.getValueAndIndexDB(sm.thisChainID, req.peerChainID, db)
		for _, removeRequest := range req.RemoveRequests {
			if err := s.RemoveValue(removeRequest); err != nil {
				return err
			}
		}

		// Add Put requests to the outbound database.
		s.valueDB, s.indexDB = outbound.getValueAndIndexDB(sm.thisChainID, req.peerChainID, db)
		for _, putRequest := range req.PutRequests {
			if err := s.SetValue(putRequest); err != nil {
				return err
			}
		}
	}

	// Commit the operations on shared memory atomically with the contents of
	// [batches].
	batch, err := vdb.CommitBatch()
	if err != nil {
		return err
	}

	return WriteAll(batch, batches...)
}

// ReadOnly drops writes to the underlying [SharedMemory] so that atomic
// operations can be replayed without duplicating writes.
type ReadOnly struct {
	sm SharedMemory
}

func NewReadOnly(sharedMemory SharedMemory) ReadOnly {
	return ReadOnly{sm: sharedMemory}
}

func (r ReadOnly) Get(
	peerChainID ids.ID,
	keys [][]byte,
) (
	[][]byte,
	error,
) {
	return r.sm.Get(peerChainID, keys)
}

func (r ReadOnly) Indexed(
	peerChainID ids.ID,
	traits [][]byte,
	startTrait,
	startKey []byte,
	limit int,
) ([][]byte, []byte, []byte, error) {
	return r.sm.Indexed(
		peerChainID,
		traits,
		startTrait,
		startKey,
		limit,
	)
}

// Apply drops atomic write requests.
func (ReadOnly) Apply(
	_ map[ids.ID]*Requests,
	bs ...database.Batch,
) error {
	// TODO hacky
	if len(bs) != 1 {
		return errors.New("expected only one batch")
	}

	return bs[0].Write()
}
