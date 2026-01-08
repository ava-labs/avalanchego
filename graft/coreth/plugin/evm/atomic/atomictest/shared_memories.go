// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomictest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

var removeValue = []byte{0x1}

type SharedMemories struct {
	ThisChain   atomic.SharedMemory
	PeerChain   atomic.SharedMemory
	thisChainID ids.ID
	peerChainID ids.ID
}

func (s *SharedMemories) AddItemsToBeRemovedToPeerChain(ops map[ids.ID]*atomic.Requests) error {
	for _, reqs := range ops {
		puts := make(map[ids.ID]*atomic.Requests)
		puts[s.thisChainID] = &atomic.Requests{}
		for _, key := range reqs.RemoveRequests {
			puts[s.thisChainID].PutRequests = append(puts[s.thisChainID].PutRequests, &atomic.Element{Key: key, Value: removeValue})
		}
		if err := s.PeerChain.Apply(puts); err != nil {
			return err
		}
	}
	return nil
}

func (s *SharedMemories) AssertOpsApplied(t *testing.T, ops map[ids.ID]*atomic.Requests) {
	t.Helper()
	for _, reqs := range ops {
		// should be able to get put requests
		for _, elem := range reqs.PutRequests {
			val, err := s.PeerChain.Get(s.thisChainID, [][]byte{elem.Key})
			require.NoError(t, err)
			require.Equal(t, [][]byte{elem.Value}, val)
		}

		// should not be able to get remove requests
		for _, key := range reqs.RemoveRequests {
			_, err := s.ThisChain.Get(s.peerChainID, [][]byte{key})
			require.ErrorIs(t, err, database.ErrNotFound)
		}
	}
}

func (s *SharedMemories) AssertOpsNotApplied(t *testing.T, ops map[ids.ID]*atomic.Requests) {
	t.Helper()
	for _, reqs := range ops {
		// should not be able to get put requests
		for _, elem := range reqs.PutRequests {
			_, err := s.PeerChain.Get(s.thisChainID, [][]byte{elem.Key})
			require.ErrorIs(t, err, database.ErrNotFound)
		}

		// should be able to get remove requests (these were previously added as puts on peerChain)
		for _, key := range reqs.RemoveRequests {
			val, err := s.ThisChain.Get(s.peerChainID, [][]byte{key})
			require.NoError(t, err)
			require.Equal(t, [][]byte{removeValue}, val)
		}
	}
}

func NewSharedMemories(atomicMemory *atomic.Memory, thisChainID, peerChainID ids.ID) *SharedMemories {
	return &SharedMemories{
		ThisChain:   atomicMemory.NewSharedMemory(thisChainID),
		PeerChain:   atomicMemory.NewSharedMemory(peerChainID),
		thisChainID: thisChainID,
		peerChainID: peerChainID,
	}
}

func TestSharedMemory() atomic.SharedMemory {
	m := atomic.NewMemory(memdb.New())
	return m.NewSharedMemory(snowtest.CChainID)
}
