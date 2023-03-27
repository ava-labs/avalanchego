// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"context"
	"encoding/json"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

var (
	_ database.Database = (*DatabaseClient)(nil)
	_ database.Batch    = (*batch)(nil)
	_ database.Iterator = (*iterator)(nil)
)

// DatabaseClient is an implementation of database that talks over RPC.
type DatabaseClient struct {
	client rpcdbpb.DatabaseClient

	closed utils.Atomic[bool]
}

// NewClient returns a database instance connected to a remote database instance
func NewClient(client rpcdbpb.DatabaseClient) *DatabaseClient {
	return &DatabaseClient{client: client}
}

// Has attempts to return if the database has a key with the provided value.
func (db *DatabaseClient) Has(key []byte) (bool, error) {
	resp, err := db.client.Has(context.Background(), &rpcdbpb.HasRequest{
		Key: key,
	})
	if err != nil {
		return false, err
	}
	return resp.Has, errEnumToError[resp.Err]
}

// Get attempts to return the value that was mapped to the key that was provided
func (db *DatabaseClient) Get(key []byte) ([]byte, error) {
	resp, err := db.client.Get(context.Background(), &rpcdbpb.GetRequest{
		Key: key,
	})
	if err != nil {
		return nil, err
	}
	return resp.Value, errEnumToError[resp.Err]
}

// Put attempts to set the value this key maps to
func (db *DatabaseClient) Put(key, value []byte) error {
	resp, err := db.client.Put(context.Background(), &rpcdbpb.PutRequest{
		Key:   key,
		Value: value,
	})
	if err != nil {
		return err
	}
	return errEnumToError[resp.Err]
}

// Delete attempts to remove any mapping from the key
func (db *DatabaseClient) Delete(key []byte) error {
	resp, err := db.client.Delete(context.Background(), &rpcdbpb.DeleteRequest{
		Key: key,
	})
	if err != nil {
		return err
	}
	return errEnumToError[resp.Err]
}

// NewBatch returns a new batch
func (db *DatabaseClient) NewBatch() database.Batch {
	return &batch{db: db}
}

func (db *DatabaseClient) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

func (db *DatabaseClient) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

func (db *DatabaseClient) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

// NewIteratorWithStartAndPrefix returns a new empty iterator
func (db *DatabaseClient) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	resp, err := db.client.NewIteratorWithStartAndPrefix(context.Background(), &rpcdbpb.NewIteratorWithStartAndPrefixRequest{
		Start:  start,
		Prefix: prefix,
	})
	if err != nil {
		return &database.IteratorError{
			Err: err,
		}
	}
	return &iterator{
		db: db,
		id: resp.Id,
	}
}

// Compact attempts to optimize the space utilization in the provided range
func (db *DatabaseClient) Compact(start, limit []byte) error {
	resp, err := db.client.Compact(context.Background(), &rpcdbpb.CompactRequest{
		Start: start,
		Limit: limit,
	})
	if err != nil {
		return err
	}
	return errEnumToError[resp.Err]
}

// Close attempts to close the database
func (db *DatabaseClient) Close() error {
	db.closed.Set(true)
	resp, err := db.client.Close(context.Background(), &rpcdbpb.CloseRequest{})
	if err != nil {
		return err
	}
	return errEnumToError[resp.Err]
}

func (db *DatabaseClient) HealthCheck(ctx context.Context) (interface{}, error) {
	health, err := db.client.HealthCheck(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	return json.RawMessage(health.Details), nil
}

type batch struct {
	database.BatchOps

	db *DatabaseClient
}

func (b *batch) Write() error {
	request := &rpcdbpb.WriteBatchRequest{}
	keySet := set.NewSet[string](len(b.Ops))
	for i := len(b.Ops) - 1; i >= 0; i-- {
		op := b.Ops[i]
		key := string(op.Key)
		if keySet.Contains(key) {
			continue
		}
		keySet.Add(key)

		if op.Delete {
			request.Deletes = append(request.Deletes, &rpcdbpb.DeleteRequest{
				Key: op.Key,
			})
		} else {
			request.Puts = append(request.Puts, &rpcdbpb.PutRequest{
				Key:   op.Key,
				Value: op.Value,
			})
		}
	}

	resp, err := b.db.client.WriteBatch(context.Background(), request)
	if err != nil {
		return err
	}
	return errEnumToError[resp.Err]
}

func (b *batch) Inner() database.Batch {
	return b
}

type iterator struct {
	db *DatabaseClient
	id uint64

	data []*rpcdbpb.PutRequest
	errs wrappers.Errs
}

// Next attempts to move the iterator to the next element and returns if this
// succeeded
func (it *iterator) Next() bool {
	if it.db.closed.Get() {
		it.data = nil
		it.errs.Add(database.ErrClosed)
		return false
	}
	if len(it.data) > 1 {
		it.data[0] = nil
		it.data = it.data[1:]
		return true
	}

	resp, err := it.db.client.IteratorNext(context.Background(), &rpcdbpb.IteratorNextRequest{
		Id: it.id,
	})
	if err != nil {
		it.errs.Add(err)
		return false
	}
	it.data = resp.Data
	return len(it.data) > 0
}

// Error returns any that occurred while iterating
func (it *iterator) Error() error {
	if it.errs.Errored() {
		return it.errs.Err
	}

	resp, err := it.db.client.IteratorError(context.Background(), &rpcdbpb.IteratorErrorRequest{
		Id: it.id,
	})
	if err != nil {
		it.errs.Add(err)
	} else {
		it.errs.Add(errEnumToError[resp.Err])
	}
	return it.errs.Err
}

// Key returns the key of the current element
func (it *iterator) Key() []byte {
	if len(it.data) == 0 {
		return nil
	}
	return it.data[0].Key
}

// Value returns the value of the current element
func (it *iterator) Value() []byte {
	if len(it.data) == 0 {
		return nil
	}
	return it.data[0].Value
}

// Release frees any resources held by the iterator
func (it *iterator) Release() {
	resp, err := it.db.client.IteratorRelease(context.Background(), &rpcdbpb.IteratorReleaseRequest{
		Id: it.id,
	})
	if err != nil {
		it.errs.Add(err)
	} else {
		it.errs.Add(errEnumToError[resp.Err])
	}
}
