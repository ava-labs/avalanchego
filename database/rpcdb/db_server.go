// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"errors"

	"golang.org/x/net/context"

	"github.com/ava-labs/avalanche-go/database"
	"github.com/ava-labs/avalanche-go/database/rpcdb/rpcdbproto"
)

var (
	errUnknownIterator = errors.New("unknown iterator")
)

// DatabaseServer is a database that is managed over RPC.
type DatabaseServer struct {
	db    database.Database
	batch database.Batch

	nextIteratorID uint64
	iterators      map[uint64]database.Iterator
}

// NewServer returns a database instance that is managed remotely
func NewServer(db database.Database) *DatabaseServer {
	return &DatabaseServer{
		db:        db,
		batch:     db.NewBatch(),
		iterators: make(map[uint64]database.Iterator),
	}
}

// Has delegates the Has call to the managed database and returns the result
func (db *DatabaseServer) Has(_ context.Context, req *rpcdbproto.HasRequest) (*rpcdbproto.HasResponse, error) {
	has, err := db.db.Has(req.Key)
	if err != nil {
		return nil, err
	}
	return &rpcdbproto.HasResponse{Has: has}, err
}

// Get delegates the Get call to the managed database and returns the result
func (db *DatabaseServer) Get(_ context.Context, req *rpcdbproto.GetRequest) (*rpcdbproto.GetResponse, error) {
	value, err := db.db.Get(req.Key)
	if err != nil {
		return nil, err
	}
	return &rpcdbproto.GetResponse{Value: value}, nil
}

// Put delegates the Put call to the managed database and returns the result
func (db *DatabaseServer) Put(_ context.Context, req *rpcdbproto.PutRequest) (*rpcdbproto.PutResponse, error) {
	return &rpcdbproto.PutResponse{}, db.db.Put(req.Key, req.Value)
}

// Delete delegates the Delete call to the managed database and returns the
// result
func (db *DatabaseServer) Delete(_ context.Context, req *rpcdbproto.DeleteRequest) (*rpcdbproto.DeleteResponse, error) {
	return &rpcdbproto.DeleteResponse{}, db.db.Delete(req.Key)
}

// Stat delegates the Stat call to the managed database and returns the result
func (db *DatabaseServer) Stat(_ context.Context, req *rpcdbproto.StatRequest) (*rpcdbproto.StatResponse, error) {
	stat, err := db.db.Stat(req.Property)
	if err != nil {
		return nil, err
	}
	return &rpcdbproto.StatResponse{Stat: stat}, nil
}

// Compact delegates the Compact call to the managed database and returns the
// result
func (db *DatabaseServer) Compact(_ context.Context, req *rpcdbproto.CompactRequest) (*rpcdbproto.CompactResponse, error) {
	return &rpcdbproto.CompactResponse{}, db.db.Compact(req.Start, req.Limit)
}

// Close delegates the Close call to the managed database and returns the result
func (db *DatabaseServer) Close(context.Context, *rpcdbproto.CloseRequest) (*rpcdbproto.CloseResponse, error) {
	return &rpcdbproto.CloseResponse{}, db.db.Close()
}

// WriteBatch takes in a set of key-value pairs and atomically writes them to
// the internal database
func (db *DatabaseServer) WriteBatch(_ context.Context, req *rpcdbproto.WriteBatchRequest) (*rpcdbproto.WriteBatchResponse, error) {
	db.batch.Reset()

	for _, put := range req.Puts {
		if err := db.batch.Put(put.Key, put.Value); err != nil {
			return nil, err
		}
	}

	for _, del := range req.Deletes {
		if err := db.batch.Delete(del.Key); err != nil {
			return nil, err
		}
	}

	return &rpcdbproto.WriteBatchResponse{}, db.batch.Write()
}

// NewIteratorWithStartAndPrefix allocates an iterator and returns the iterator
// ID
func (db *DatabaseServer) NewIteratorWithStartAndPrefix(_ context.Context, req *rpcdbproto.NewIteratorWithStartAndPrefixRequest) (*rpcdbproto.NewIteratorWithStartAndPrefixResponse, error) {
	id := db.nextIteratorID
	it := db.db.NewIteratorWithStartAndPrefix(req.Start, req.Prefix)
	db.iterators[id] = it

	db.nextIteratorID++
	return &rpcdbproto.NewIteratorWithStartAndPrefixResponse{Id: id}, nil
}

// IteratorNext attempts to call next on the requested iterator
func (db *DatabaseServer) IteratorNext(_ context.Context, req *rpcdbproto.IteratorNextRequest) (*rpcdbproto.IteratorNextResponse, error) {
	it, exists := db.iterators[req.Id]
	if !exists {
		return nil, errUnknownIterator
	}
	return &rpcdbproto.IteratorNextResponse{
		FoundNext: it.Next(),
		Key:       it.Key(),
		Value:     it.Value(),
	}, nil
}

// IteratorError attempts to report any errors that occurred during iteration
func (db *DatabaseServer) IteratorError(_ context.Context, req *rpcdbproto.IteratorErrorRequest) (*rpcdbproto.IteratorErrorResponse, error) {
	it, exists := db.iterators[req.Id]
	if !exists {
		return nil, errUnknownIterator
	}
	return &rpcdbproto.IteratorErrorResponse{}, it.Error()
}

// IteratorRelease attempts to release the resources allocated to an iterator
func (db *DatabaseServer) IteratorRelease(_ context.Context, req *rpcdbproto.IteratorReleaseRequest) (*rpcdbproto.IteratorReleaseResponse, error) {
	it, exists := db.iterators[req.Id]
	if exists {
		delete(db.iterators, req.Id)
		it.Release()
	}
	return &rpcdbproto.IteratorReleaseResponse{}, nil
}
