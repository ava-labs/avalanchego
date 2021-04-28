// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/chains/atomic/gsharedmemory/gsharedmemoryproto"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ gsharedmemoryproto.SharedMemoryServer = &Server{}
)

// Server is shared memory that is managed over RPC.
type Server struct {
	sm atomic.SharedMemory
	db database.Database

	putsLock sync.Mutex
	puts     map[int64]*putRequest

	removesLock sync.Mutex
	removes     map[int64]*removeRequest
}

// NewServer returns shared memory connected to remote shared memory
func NewServer(sm atomic.SharedMemory, db database.Database) *Server {
	return &Server{
		sm:      sm,
		db:      db,
		puts:    make(map[int64]*putRequest),
		removes: make(map[int64]*removeRequest),
	}
}

type putRequest struct {
	peerChainID ids.ID
	elems       []*atomic.Element
	batches     map[int64]database.Batch
}

func (s *Server) Put(
	_ context.Context,
	req *gsharedmemoryproto.PutRequest,
) (*gsharedmemoryproto.PutResponse, error) {
	s.putsLock.Lock()
	defer s.putsLock.Unlock()

	put, exists := s.puts[req.Id]
	if !exists {
		peerChainID, err := ids.ToID(req.PeerChainID)
		if err != nil {
			return nil, err
		}

		put = &putRequest{
			peerChainID: peerChainID,
			elems:       make([]*atomic.Element, len(req.Elems)),
			batches:     make(map[int64]database.Batch),
		}
		for i, elem := range req.Elems {
			put.elems[i] = &atomic.Element{
				Key:    elem.Key,
				Value:  elem.Value,
				Traits: elem.Traits,
			}
		}
	}

	if err := s.parseBatches(put.batches, req.Batches); err != nil {
		return nil, err
	}

	if req.Continues {
		s.puts[req.Id] = put
		return &gsharedmemoryproto.PutResponse{}, nil
	}

	delete(s.puts, req.Id)

	batches := make([]database.Batch, len(put.batches))
	i := 0
	for _, batch := range put.batches {
		batches[i] = batch
		i++
	}
	return &gsharedmemoryproto.PutResponse{}, s.sm.Put(put.peerChainID, put.elems, batches...)
}

func (s *Server) Get(
	_ context.Context,
	req *gsharedmemoryproto.GetRequest,
) (*gsharedmemoryproto.GetResponse, error) {
	peerChainID, err := ids.ToID(req.PeerChainID)
	if err != nil {
		return nil, err
	}

	values, err := s.sm.Get(peerChainID, req.Keys)
	return &gsharedmemoryproto.GetResponse{
		Values: values,
	}, err
}

func (s *Server) Indexed(
	_ context.Context,
	req *gsharedmemoryproto.IndexedRequest,
) (*gsharedmemoryproto.IndexedResponse, error) {
	peerChainID, err := ids.ToID(req.PeerChainID)
	if err != nil {
		return nil, err
	}

	values, lastTrait, lastKey, err := s.sm.Indexed(
		peerChainID,
		req.Traits,
		req.StartTrait,
		req.StartKey,
		int(req.Limit),
	)
	return &gsharedmemoryproto.IndexedResponse{
		Values:    values,
		LastTrait: lastTrait,
		LastKey:   lastKey,
	}, err
}

type removeRequest struct {
	peerChainID ids.ID
	keys        [][]byte
	batches     map[int64]database.Batch
}

func (s *Server) Remove(
	_ context.Context,
	req *gsharedmemoryproto.RemoveRequest,
) (*gsharedmemoryproto.RemoveResponse, error) {
	s.removesLock.Lock()
	defer s.removesLock.Unlock()

	remove, exists := s.removes[req.Id]
	if !exists {
		peerChainID, err := ids.ToID(req.PeerChainID)
		if err != nil {
			return nil, err
		}

		remove = &removeRequest{
			peerChainID: peerChainID,
			keys:        req.Keys,
			batches:     make(map[int64]database.Batch),
		}
	}

	if err := s.parseBatches(remove.batches, req.Batches); err != nil {
		return nil, err
	}

	if req.Continues {
		s.removes[req.Id] = remove
		return &gsharedmemoryproto.RemoveResponse{}, nil
	}

	delete(s.removes, req.Id)

	batches := make([]database.Batch, len(remove.batches))
	i := 0
	for _, batch := range remove.batches {
		batches[i] = batch
		i++
	}
	return &gsharedmemoryproto.RemoveResponse{}, s.sm.Remove(remove.peerChainID, remove.keys, batches...)
}

func (s *Server) parseBatches(
	batches map[int64]database.Batch,
	rawBatches []*gsharedmemoryproto.Batch,
) error {
	for _, reqBatch := range rawBatches {
		batch, exists := batches[reqBatch.Id]
		if !exists {
			batch = s.db.NewBatch()
			batches[reqBatch.Id] = batch
		}

		for _, putReq := range reqBatch.Puts {
			if err := batch.Put(putReq.Key, putReq.Value); err != nil {
				return err
			}
		}

		for _, deleteReq := range reqBatch.Deletes {
			if err := batch.Delete(deleteReq.Key); err != nil {
				return err
			}
		}
	}
	return nil
}
