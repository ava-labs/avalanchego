// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"errors"
	"sync"

	"golang.org/x/net/context"

	"github.com/ava-labs/avalanchego/database"

	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

var errUnknownIterator = errors.New("unknown iterator")

// DatabaseServer is a database that is managed over RPC.
type DatabaseServer struct {
	rpcdbpb.UnimplementedDatabaseServer
	lock    sync.Mutex
	db      database.Database
	batches map[int64]database.Batch

	nextIteratorID uint64
	iterators      map[uint64]database.Iterator
}

// NewServer returns a database instance that is managed remotely
func NewServer(db database.Database) *DatabaseServer {
	return &DatabaseServer{
		db:        db,
		batches:   make(map[int64]database.Batch),
		iterators: make(map[uint64]database.Iterator),
	}
}

// Has delegates the Has call to the managed database and returns the result
func (db *DatabaseServer) Has(_ context.Context, req *rpcdbpb.HasRequest) (*rpcdbpb.HasResponse, error) {
	has, err := db.db.Has(req.Key)
	return &rpcdbpb.HasResponse{
		Has: has,
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

// Get delegates the Get call to the managed database and returns the result
func (db *DatabaseServer) Get(_ context.Context, req *rpcdbpb.GetRequest) (*rpcdbpb.GetResponse, error) {
	value, err := db.db.Get(req.Key)
	return &rpcdbpb.GetResponse{
		Value: value,
		Err:   errorToErrCode[err],
	}, errorToRPCError(err)
}

// Put delegates the Put call to the managed database and returns the result
func (db *DatabaseServer) Put(_ context.Context, req *rpcdbpb.PutRequest) (*rpcdbpb.PutResponse, error) {
	err := db.db.Put(req.Key, req.Value)
	return &rpcdbpb.PutResponse{Err: errorToErrCode[err]}, errorToRPCError(err)
}

// Delete delegates the Delete call to the managed database and returns the
// result
func (db *DatabaseServer) Delete(_ context.Context, req *rpcdbpb.DeleteRequest) (*rpcdbpb.DeleteResponse, error) {
	err := db.db.Delete(req.Key)
	return &rpcdbpb.DeleteResponse{Err: errorToErrCode[err]}, errorToRPCError(err)
}

// Stat delegates the Stat call to the managed database and returns the result
func (db *DatabaseServer) Stat(_ context.Context, req *rpcdbpb.StatRequest) (*rpcdbpb.StatResponse, error) {
	stat, err := db.db.Stat(req.Property)
	return &rpcdbpb.StatResponse{
		Stat: stat,
		Err:  errorToErrCode[err],
	}, errorToRPCError(err)
}

// Compact delegates the Compact call to the managed database and returns the
// result
func (db *DatabaseServer) Compact(_ context.Context, req *rpcdbpb.CompactRequest) (*rpcdbpb.CompactResponse, error) {
	err := db.db.Compact(req.Start, req.Limit)
	return &rpcdbpb.CompactResponse{Err: errorToErrCode[err]}, errorToRPCError(err)
}

// Close delegates the Close call to the managed database and returns the result
func (db *DatabaseServer) Close(context.Context, *rpcdbpb.CloseRequest) (*rpcdbpb.CloseResponse, error) {
	err := db.db.Close()
	return &rpcdbpb.CloseResponse{Err: errorToErrCode[err]}, errorToRPCError(err)
}

// WriteBatch takes in a set of key-value pairs and atomically writes them to
// the internal database
func (db *DatabaseServer) WriteBatch(_ context.Context, req *rpcdbpb.WriteBatchRequest) (*rpcdbpb.WriteBatchResponse, error) {
	db.lock.Lock()
	defer db.lock.Unlock()

	batch, exists := db.batches[req.Id]
	if !exists {
		batch = db.db.NewBatch()
	}

	for _, put := range req.Puts {
		if err := batch.Put(put.Key, put.Value); err != nil {
			// Because we are reporting an error, we free the allocated batch.
			delete(db.batches, req.Id)

			return &rpcdbpb.WriteBatchResponse{Err: errorToErrCode[err]}, errorToRPCError(err)
		}
	}

	for _, del := range req.Deletes {
		if err := batch.Delete(del.Key); err != nil {
			// Because we are reporting an error, we free the allocated batch.
			delete(db.batches, req.Id)

			return &rpcdbpb.WriteBatchResponse{Err: errorToErrCode[err]}, errorToRPCError(err)
		}
	}

	if req.Continues {
		db.batches[req.Id] = batch
		return &rpcdbpb.WriteBatchResponse{}, nil
	}

	delete(db.batches, req.Id)
	err := batch.Write()
	return &rpcdbpb.WriteBatchResponse{Err: errorToErrCode[err]}, errorToRPCError(err)
}

// NewIteratorWithStartAndPrefix allocates an iterator and returns the iterator
// ID
func (db *DatabaseServer) NewIteratorWithStartAndPrefix(_ context.Context, req *rpcdbpb.NewIteratorWithStartAndPrefixRequest) (*rpcdbpb.NewIteratorWithStartAndPrefixResponse, error) {
	db.lock.Lock()
	defer db.lock.Unlock()

	id := db.nextIteratorID
	it := db.db.NewIteratorWithStartAndPrefix(req.Start, req.Prefix)
	db.iterators[id] = it

	db.nextIteratorID++
	return &rpcdbpb.NewIteratorWithStartAndPrefixResponse{Id: id}, nil
}

// IteratorNext attempts to call next on the requested iterator
func (db *DatabaseServer) IteratorNext(_ context.Context, req *rpcdbpb.IteratorNextRequest) (*rpcdbpb.IteratorNextResponse, error) {
	db.lock.Lock()
	defer db.lock.Unlock()

	it, exists := db.iterators[req.Id]
	if !exists {
		return nil, errUnknownIterator
	}

	size := 0
	data := []*rpcdbpb.PutRequest(nil)
	for size < maxBatchSize && it.Next() {
		key := it.Key()
		value := it.Value()
		size += len(key) + len(value)

		data = append(data, &rpcdbpb.PutRequest{
			Key:   key,
			Value: value,
		})
	}

	return &rpcdbpb.IteratorNextResponse{Data: data}, nil
}

// IteratorError attempts to report any errors that occurred during iteration
func (db *DatabaseServer) IteratorError(_ context.Context, req *rpcdbpb.IteratorErrorRequest) (*rpcdbpb.IteratorErrorResponse, error) {
	db.lock.Lock()
	defer db.lock.Unlock()

	it, exists := db.iterators[req.Id]
	if !exists {
		return nil, errUnknownIterator
	}
	err := it.Error()
	return &rpcdbpb.IteratorErrorResponse{Err: errorToErrCode[err]}, errorToRPCError(err)
}

// IteratorRelease attempts to release the resources allocated to an iterator
func (db *DatabaseServer) IteratorRelease(_ context.Context, req *rpcdbpb.IteratorReleaseRequest) (*rpcdbpb.IteratorReleaseResponse, error) {
	db.lock.Lock()
	defer db.lock.Unlock()

	it, exists := db.iterators[req.Id]
	if !exists {
		return &rpcdbpb.IteratorReleaseResponse{Err: 0}, nil
	}

	delete(db.iterators, req.Id)
	err := it.Error()
	it.Release()
	return &rpcdbpb.IteratorReleaseResponse{Err: errorToErrCode[err]}, errorToRPCError(err)
}
