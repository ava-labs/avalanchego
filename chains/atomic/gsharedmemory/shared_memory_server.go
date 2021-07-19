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
	gsharedmemoryproto.UnimplementedSharedMemoryServer
	sm atomic.SharedMemory
	db database.Database

	getsLock sync.Mutex
	gets     map[int64]*getRequest

	indexedLock sync.Mutex
	indexed     map[int64]*indexedRequest

	removeAndPutMultipleLock sync.Mutex
	removeAndPutMultiples    map[int64]*removeAndPutMultipleRequest
}

// NewServer returns shared memory connected to remote shared memory
func NewServer(sm atomic.SharedMemory, db database.Database) *Server {
	return &Server{
		sm:                    sm,
		db:                    db,
		gets:                  make(map[int64]*getRequest),
		indexed:               make(map[int64]*indexedRequest),
		removeAndPutMultiples: make(map[int64]*removeAndPutMultipleRequest),
	}
}

type removeAndPutMultipleRequest struct {
	batchChainsAndInputs map[ids.ID]*atomic.Requests
	batch                map[int64]database.Batch
}

func (s *Server) RemoveAndPutMultiple(
	_ context.Context,
	req *gsharedmemoryproto.RemoveAndPutMultipleRequest,
) (*gsharedmemoryproto.RemoveAndPutMultipleResponse, error) {
	s.removeAndPutMultipleLock.Lock()
	defer s.removeAndPutMultipleLock.Unlock()

	removeAndPut, exists := s.removeAndPutMultiples[req.Id]
	if !exists {
		removeAndPut = &removeAndPutMultipleRequest{
			batchChainsAndInputs: make(map[ids.ID]*atomic.Requests),
			batch:                make(map[int64]database.Batch),
		}

	}

	for _, value := range req.BatchChainsAndInputs {
		formattedElements := make([]*atomic.Element, 0, len(value.PutRequests))
		chainIdentifier, err := ids.ToID(value.PeerChainID)
		if err != nil {
			return nil, err
		}

		for _, v := range value.PutRequests {
			formattedElements = append(formattedElements, &atomic.Element{
				Key:    v.Key,
				Value:  v.Value,
				Traits: v.Traits,
			})
		}

		formattedValues := &atomic.Requests{
			RemoveRequests: value.RemoveRequests,
			PutRequests:    formattedElements,
		}

		if val, ok := removeAndPut.batchChainsAndInputs[chainIdentifier]; ok {
			val.PutRequests = append(val.PutRequests, formattedValues.PutRequests...)
			val.RemoveRequests = append(val.RemoveRequests, formattedValues.RemoveRequests...)
		} else {
			removeAndPut.batchChainsAndInputs[chainIdentifier] = formattedValues

		}

	}

	if err := s.parseBatches(removeAndPut.batch, req.Batches); err != nil {
		delete(s.removeAndPutMultiples, req.Id)
		return nil, err
	}

	if req.Continues {
		s.removeAndPutMultiples[req.Id] = removeAndPut
		return &gsharedmemoryproto.RemoveAndPutMultipleResponse{}, nil
	}

	delete(s.removeAndPutMultiples, req.Id)

	batches := make([]database.Batch, len(removeAndPut.batch))
	i := 0
	for _, batch := range removeAndPut.batch {
		batches[i] = batch
		i++
	}

	return &gsharedmemoryproto.RemoveAndPutMultipleResponse{}, s.sm.RemoveAndPutMultiple(removeAndPut.batchChainsAndInputs, batches...)
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
