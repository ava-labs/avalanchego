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

var _ gsharedmemoryproto.SharedMemoryServer = &Server{}

// Server is shared memory that is managed over RPC.
type Server struct {
	sm atomic.SharedMemory
	db database.Database

	putsLock sync.Mutex
	puts     map[int64]*putRequest

	getsLock sync.Mutex
	gets     map[int64]*getRequest

	indexedLock sync.Mutex
	indexed     map[int64]*indexedRequest

	removesLock sync.Mutex
	removes     map[int64]*removeRequest
}

// NewServer returns shared memory connected to remote shared memory
func NewServer(sm atomic.SharedMemory, db database.Database) *Server {
	return &Server{
		sm:      sm,
		db:      db,
		puts:    make(map[int64]*putRequest),
		gets:    make(map[int64]*getRequest),
		indexed: make(map[int64]*indexedRequest),
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
			elems:       make([]*atomic.Element, 0, len(req.Elems)),
			batches:     make(map[int64]database.Batch),
		}
	}

	for _, elem := range req.Elems {
		put.elems = append(put.elems, &atomic.Element{
			Key:    elem.Key,
			Value:  elem.Value,
			Traits: elem.Traits,
		})
	}

	if err := s.parseBatches(put.batches, req.Batches); err != nil {
		delete(s.puts, req.Id)
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

type getRequest struct {
	peerChainID ids.ID
	keys        [][]byte

	executed        bool
	remainingValues [][]byte
}

func (s *Server) Get(
	_ context.Context,
	req *gsharedmemoryproto.GetRequest,
) (*gsharedmemoryproto.GetResponse, error) {
	s.getsLock.Lock()
	defer s.getsLock.Unlock()

	get, exists := s.gets[req.Id]
	if !exists {
		peerChainID, err := ids.ToID(req.PeerChainID)
		if err != nil {
			return nil, err
		}

		get = &getRequest{
			peerChainID: peerChainID,
			keys:        make([][]byte, 0, len(req.Keys)),
		}
	}

	get.keys = append(get.keys, req.Keys...)

	if req.Continues {
		s.gets[req.Id] = get
		return &gsharedmemoryproto.GetResponse{}, nil
	}

	if !get.executed {
		values, err := s.sm.Get(get.peerChainID, get.keys)
		if err != nil {
			delete(s.gets, req.Id)
			return nil, err
		}

		get.executed = true
		get.remainingValues = values
	}

	currentSize := 0
	resp := &gsharedmemoryproto.GetResponse{}
	for i, value := range get.remainingValues {
		sizeChange := baseElementSize + len(value)
		if newSize := currentSize + sizeChange; newSize > maxBatchSize && i > 0 {
			break
		}
		currentSize += sizeChange

		resp.Values = append(resp.Values, value)
	}

	get.remainingValues = get.remainingValues[len(resp.Values):]
	resp.Continues = len(get.remainingValues) > 0

	if resp.Continues {
		s.gets[req.Id] = get
	} else {
		delete(s.gets, req.Id)
	}
	return resp, nil
}

type indexedRequest struct {
	peerChainID ids.ID
	traits      [][]byte
	startTrait  []byte
	startKey    []byte
	limit       int

	executed        bool
	remainingValues [][]byte
	lastTrait       []byte
	lastKey         []byte
}

func (s *Server) Indexed(
	_ context.Context,
	req *gsharedmemoryproto.IndexedRequest,
) (*gsharedmemoryproto.IndexedResponse, error) {
	s.indexedLock.Lock()
	defer s.indexedLock.Unlock()

	indexed, exists := s.indexed[req.Id]
	if !exists {
		peerChainID, err := ids.ToID(req.PeerChainID)
		if err != nil {
			return nil, err
		}

		indexed = &indexedRequest{
			peerChainID: peerChainID,
			traits:      make([][]byte, 0, len(req.Traits)),
			startTrait:  req.StartTrait,
			startKey:    req.StartKey,
			limit:       int(req.Limit),
		}
	}

	indexed.traits = append(indexed.traits, req.Traits...)

	if req.Continues {
		s.indexed[req.Id] = indexed
		return &gsharedmemoryproto.IndexedResponse{}, nil
	}

	if !indexed.executed {
		values, lastTrait, lastKey, err := s.sm.Indexed(
			indexed.peerChainID,
			indexed.traits,
			indexed.startTrait,
			indexed.startKey,
			indexed.limit,
		)
		if err != nil {
			delete(s.indexed, req.Id)
			return nil, err
		}

		indexed.executed = true
		indexed.remainingValues = values
		indexed.lastTrait = lastTrait
		indexed.lastKey = lastKey
	}

	currentSize := 0
	resp := &gsharedmemoryproto.IndexedResponse{
		LastTrait: indexed.lastTrait,
		LastKey:   indexed.lastKey,
	}
	for i, value := range indexed.remainingValues {
		sizeChange := baseElementSize + len(value)
		if newSize := currentSize + sizeChange; newSize > maxBatchSize && i > 0 {
			break
		}
		currentSize += sizeChange

		resp.Values = append(resp.Values, value)
	}

	indexed.remainingValues = indexed.remainingValues[len(resp.Values):]
	resp.Continues = len(indexed.remainingValues) > 0

	if resp.Continues {
		indexed.lastTrait = nil
		indexed.lastKey = nil
		s.indexed[req.Id] = indexed
	} else {
		delete(s.indexed, req.Id)
	}
	return resp, nil
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
			batches:     make(map[int64]database.Batch),
		}
	}

	remove.keys = append(remove.keys, req.Keys...)
	if err := s.parseBatches(remove.batches, req.Batches); err != nil {
		delete(s.removes, req.Id)
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
