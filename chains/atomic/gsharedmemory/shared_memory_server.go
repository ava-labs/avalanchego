// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"context"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"

	sharedmemorypb "github.com/ava-labs/avalanchego/proto/pb/sharedmemory"
)

var _ sharedmemorypb.SharedMemoryServer = (*Server)(nil)

// Server is shared memory that is managed over RPC.
type Server struct {
	sharedmemorypb.UnsafeSharedMemoryServer
	sm atomic.SharedMemory
	db database.Database
}

// NewServer returns shared memory connected to remote shared memory
func NewServer(sm atomic.SharedMemory, db database.Database) *Server {
	return &Server{
		sm: sm,
		db: db,
	}
}

func (s *Server) Get(
	_ context.Context,
	req *sharedmemorypb.GetRequest,
) (*sharedmemorypb.GetResponse, error) {
	peerChainID, err := ids.ToID(req.PeerChainId)
	if err != nil {
		return nil, err
	}

	values, err := s.sm.Get(peerChainID, req.Keys)
	return &sharedmemorypb.GetResponse{
		Values: values,
	}, err
}

func (s *Server) Indexed(
	_ context.Context,
	req *sharedmemorypb.IndexedRequest,
) (*sharedmemorypb.IndexedResponse, error) {
	peerChainID, err := ids.ToID(req.PeerChainId)
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
	return &sharedmemorypb.IndexedResponse{
		Values:    values,
		LastTrait: lastTrait,
		LastKey:   lastKey,
	}, err
}

func (s *Server) Apply(
	_ context.Context,
	req *sharedmemorypb.ApplyRequest,
) (*sharedmemorypb.ApplyResponse, error) {
	requests := make(map[ids.ID]*atomic.Requests, len(req.Requests))
	for _, request := range req.Requests {
		peerChainID, err := ids.ToID(request.PeerChainId)
		if err != nil {
			return nil, err
		}

		r := &atomic.Requests{
			RemoveRequests: request.RemoveRequests,
			PutRequests:    make([]*atomic.Element, len(request.PutRequests)),
		}
		for i, put := range request.PutRequests {
			r.PutRequests[i] = &atomic.Element{
				Key:    put.Key,
				Value:  put.Value,
				Traits: put.Traits,
			}
		}
		requests[peerChainID] = r
	}

	batches := make([]database.Batch, len(req.Batches))
	for i, reqBatch := range req.Batches {
		batch := s.db.NewBatch()
		for _, putReq := range reqBatch.Puts {
			if err := batch.Put(putReq.Key, putReq.Value); err != nil {
				return nil, err
			}
		}
		for _, deleteReq := range reqBatch.Deletes {
			if err := batch.Delete(deleteReq.Key); err != nil {
				return nil, err
			}
		}
		batches[i] = batch
	}
	return &sharedmemorypb.ApplyResponse{}, s.sm.Apply(requests, batches...)
}
