// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"errors"

	"golang.org/x/net/context"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/rpcdb/proto"
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

// Has ...
func (db *DatabaseServer) Has(_ context.Context, req *proto.HasRequest) (*proto.HasResponse, error) {
	has, err := db.db.Has(req.Key)
	if err != nil {
		return nil, err
	}
	return &proto.HasResponse{Has: has}, nil
}

// Get ...
func (db *DatabaseServer) Get(_ context.Context, req *proto.GetRequest) (*proto.GetResponse, error) {
	value, err := db.db.Get(req.Key)
	if err != nil {
		return nil, err
	}
	return &proto.GetResponse{Value: value}, nil
}

// Put ...
func (db *DatabaseServer) Put(_ context.Context, req *proto.PutRequest) (*proto.PutResponse, error) {
	return &proto.PutResponse{}, db.db.Put(req.Key, req.Value)
}

// Delete ...
func (db *DatabaseServer) Delete(_ context.Context, req *proto.DeleteRequest) (*proto.DeleteResponse, error) {
	return &proto.DeleteResponse{}, db.db.Delete(req.Key)
}

// Stat ...
func (db *DatabaseServer) Stat(_ context.Context, req *proto.StatRequest) (*proto.StatResponse, error) {
	stat, err := db.db.Stat(req.Property)
	if err != nil {
		return nil, err
	}
	return &proto.StatResponse{Stat: stat}, nil
}

// Compact ...
func (db *DatabaseServer) Compact(_ context.Context, req *proto.CompactRequest) (*proto.CompactResponse, error) {
	return &proto.CompactResponse{}, db.db.Compact(req.Start, req.Limit)
}

// Close ...
func (db *DatabaseServer) Close(_ context.Context, _ *proto.CloseRequest) (*proto.CloseResponse, error) {
	return &proto.CloseResponse{}, db.db.Close()
}

// WriteBatch ...
func (db *DatabaseServer) WriteBatch(_ context.Context, req *proto.WriteBatchRequest) (*proto.WriteBatchResponse, error) {
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

	return &proto.WriteBatchResponse{}, db.batch.Write()
}

// NewIteratorWithStartAndPrefix ...
func (db *DatabaseServer) NewIteratorWithStartAndPrefix(_ context.Context, req *proto.NewIteratorWithStartAndPrefixRequest) (*proto.NewIteratorWithStartAndPrefixResponse, error) {
	id := db.nextIteratorID
	it := db.db.NewIteratorWithStartAndPrefix(req.Start, req.Prefix)
	db.iterators[id] = it

	db.nextIteratorID++
	return &proto.NewIteratorWithStartAndPrefixResponse{Id: id}, nil
}

// IteratorNext ...
func (db *DatabaseServer) IteratorNext(_ context.Context, req *proto.IteratorNextRequest) (*proto.IteratorNextResponse, error) {
	it, exists := db.iterators[req.Id]
	if !exists {
		return nil, errUnknownIterator
	}
	return &proto.IteratorNextResponse{
		FoundNext: it.Next(),
		Key:       it.Key(),
		Value:     it.Value(),
	}, nil
}

// IteratorError ...
func (db *DatabaseServer) IteratorError(_ context.Context, req *proto.IteratorErrorRequest) (*proto.IteratorErrorResponse, error) {
	it, exists := db.iterators[req.Id]
	if !exists {
		return nil, errUnknownIterator
	}
	return &proto.IteratorErrorResponse{}, it.Error()
}

// IteratorRelease ...
func (db *DatabaseServer) IteratorRelease(_ context.Context, req *proto.IteratorReleaseRequest) (*proto.IteratorReleaseResponse, error) {
	it, exists := db.iterators[req.Id]
	if exists {
		delete(db.iterators, req.Id)
		it.Release()
	}
	return &proto.IteratorReleaseResponse{}, nil
}
