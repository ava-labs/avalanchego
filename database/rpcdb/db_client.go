// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"context"
	"encoding/json"
	"sync"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"

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
	return resp.Has, ErrEnumToError[resp.Err]
}

// Get attempts to return the value that was mapped to the key that was provided
func (db *DatabaseClient) Get(key []byte) ([]byte, error) {
	resp, err := db.client.Get(context.Background(), &rpcdbpb.GetRequest{
		Key: key,
	})
	if err != nil {
		return nil, err
	}
	return resp.Value, ErrEnumToError[resp.Err]
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
	return ErrEnumToError[resp.Err]
}

// Delete attempts to remove any mapping from the key
func (db *DatabaseClient) Delete(key []byte) error {
	resp, err := db.client.Delete(context.Background(), &rpcdbpb.DeleteRequest{
		Key: key,
	})
	if err != nil {
		return err
	}
	return ErrEnumToError[resp.Err]
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
	return newIterator(db, resp.Id)
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
	return ErrEnumToError[resp.Err]
}

// Close attempts to close the database
func (db *DatabaseClient) Close() error {
	db.closed.Set(true)
	resp, err := db.client.Close(context.Background(), &rpcdbpb.CloseRequest{})
	if err != nil {
		return err
	}
	return ErrEnumToError[resp.Err]
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
	return ErrEnumToError[resp.Err]
}

func (b *batch) Inner() database.Batch {
	return b
}

type iterator struct {
	db *DatabaseClient
	id uint64

	data        []*rpcdbpb.PutRequest
	fetchedData chan []*rpcdbpb.PutRequest

	errLock sync.RWMutex
	err     error

	reqUpdateError chan chan struct{}

	once     sync.Once
	onClose  chan struct{}
	onClosed chan struct{}
}

func newIterator(db *DatabaseClient, id uint64) *iterator {
	it := &iterator{
		db:             db,
		id:             id,
		fetchedData:    make(chan []*rpcdbpb.PutRequest),
		reqUpdateError: make(chan chan struct{}),
		onClose:        make(chan struct{}),
		onClosed:       make(chan struct{}),
	}
	go it.fetch()
	return it
}

// Invariant: fetch is the only thread with access to send requests to the
// server's iterator. This is needed because iterators are not thread safe and
// the server expects the client (us) to only ever issue one request at a time
// for a given iterator id.
func (it *iterator) fetch() {
	defer func() {
		resp, err := it.db.client.IteratorRelease(context.Background(), &rpcdbpb.IteratorReleaseRequest{
			Id: it.id,
		})
		if err != nil {
			it.setError(err)
		} else {
			it.setError(ErrEnumToError[resp.Err])
		}

		close(it.fetchedData)
		close(it.onClosed)
	}()

	for {
		resp, err := it.db.client.IteratorNext(context.Background(), &rpcdbpb.IteratorNextRequest{
			Id: it.id,
		})
		if err != nil {
			it.setError(err)
			return
		}

		if len(resp.Data) == 0 {
			return
		}

		for {
			select {
			case it.fetchedData <- resp.Data:
			case onUpdated := <-it.reqUpdateError:
				it.updateError()
				close(onUpdated)
				continue
			case <-it.onClose:
				return
			}
			break
		}
	}
}

// Next attempts to move the iterator to the next element and returns if this
// succeeded
func (it *iterator) Next() bool {
	if it.db.closed.Get() {
		it.data = nil
		it.setError(database.ErrClosed)
		return false
	}
	if len(it.data) > 1 {
		it.data[0] = nil
		it.data = it.data[1:]
		return true
	}

	it.data = <-it.fetchedData
	return len(it.data) > 0
}

// Error returns any that occurred while iterating
func (it *iterator) Error() error {
	if err := it.getError(); err != nil {
		return err
	}

	onUpdated := make(chan struct{})
	select {
	case it.reqUpdateError <- onUpdated:
		<-onUpdated
	case <-it.onClosed:
	}

	return it.getError()
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
	it.once.Do(func() {
		close(it.onClose)
		<-it.onClosed
	})
}

func (it *iterator) updateError() {
	resp, err := it.db.client.IteratorError(context.Background(), &rpcdbpb.IteratorErrorRequest{
		Id: it.id,
	})
	if err != nil {
		it.setError(err)
	} else {
		it.setError(ErrEnumToError[resp.Err])
	}
}

func (it *iterator) setError(err error) {
	if err == nil {
		return
	}

	it.errLock.Lock()
	defer it.errLock.Unlock()

	if it.err == nil {
		it.err = err
	}
}

func (it *iterator) getError() error {
	it.errLock.RLock()
	defer it.errLock.RUnlock()

	return it.err
}
