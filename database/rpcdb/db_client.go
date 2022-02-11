// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"sync/atomic"

	"golang.org/x/net/context"

	"github.com/ava-labs/avalanchego/api/proto/rpcdbproto"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/nodb"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	maxBatchSize = 128 * units.KiB

	// baseElementSize is an approximation of the protobuf encoding overhead per
	// element
	baseElementSize = 8 // bytes
)

var (
	_ database.Database = &DatabaseClient{}
	_ database.Batch    = &batch{}
	_ database.Iterator = &iterator{}
)

// DatabaseClient is an implementation of database that talks over RPC.
type DatabaseClient struct {
	client rpcdbproto.DatabaseClient

	closed     utils.AtomicBool
	batchIndex int64
}

// NewClient returns a database instance connected to a remote database instance
func NewClient(client rpcdbproto.DatabaseClient) *DatabaseClient {
	return &DatabaseClient{client: client}
}

// Has attempts to return if the database has a key with the provided value.
func (db *DatabaseClient) Has(key []byte) (bool, error) {
	resp, err := db.client.Has(context.Background(), &rpcdbproto.HasRequest{
		Key: key,
	})
	if err != nil {
		return false, err
	}
	return resp.Has, errCodeToError[resp.Err]
}

// Get attempts to return the value that was mapped to the key that was provided
func (db *DatabaseClient) Get(key []byte) ([]byte, error) {
	resp, err := db.client.Get(context.Background(), &rpcdbproto.GetRequest{
		Key: key,
	})
	if err != nil {
		return nil, err
	}
	return resp.Value, errCodeToError[resp.Err]
}

// Put attempts to set the value this key maps to
func (db *DatabaseClient) Put(key, value []byte) error {
	resp, err := db.client.Put(context.Background(), &rpcdbproto.PutRequest{
		Key:   key,
		Value: value,
	})
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}

// Delete attempts to remove any mapping from the key
func (db *DatabaseClient) Delete(key []byte) error {
	resp, err := db.client.Delete(context.Background(), &rpcdbproto.DeleteRequest{
		Key: key,
	})
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}

// NewBatch returns a new batch
func (db *DatabaseClient) NewBatch() database.Batch { return &batch{db: db} }

// NewIterator implements the Database interface
func (db *DatabaseClient) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

// NewIteratorWithStart implements the Database interface
func (db *DatabaseClient) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

// NewIteratorWithPrefix implements the Database interface
func (db *DatabaseClient) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

// NewIteratorWithStartAndPrefix returns a new empty iterator
func (db *DatabaseClient) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	resp, err := db.client.NewIteratorWithStartAndPrefix(context.Background(), &rpcdbproto.NewIteratorWithStartAndPrefixRequest{
		Start:  start,
		Prefix: prefix,
	})
	if err != nil {
		return &nodb.Iterator{Err: err}
	}
	return &iterator{
		db: db,
		id: resp.Id,
	}
}

// Stat attempts to return the statistic of this database
func (db *DatabaseClient) Stat(property string) (string, error) {
	resp, err := db.client.Stat(context.Background(), &rpcdbproto.StatRequest{
		Property: property,
	})
	if err != nil {
		return "", err
	}
	return resp.Stat, errCodeToError[resp.Err]
}

// Compact attempts to optimize the space utilization in the provided range
func (db *DatabaseClient) Compact(start, limit []byte) error {
	resp, err := db.client.Compact(context.Background(), &rpcdbproto.CompactRequest{
		Start: start,
		Limit: limit,
	})
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}

// Close attempts to close the database
func (db *DatabaseClient) Close() error {
	db.closed.SetValue(true)
	resp, err := db.client.Close(context.Background(), &rpcdbproto.CloseRequest{})
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}

type keyValue struct {
	key    []byte
	value  []byte
	delete bool
}

type batch struct {
	db     *DatabaseClient
	writes []keyValue
	size   int
}

func (b *batch) Put(key, value []byte) error {
	b.writes = append(b.writes, keyValue{utils.CopyBytes(key), utils.CopyBytes(value), false})
	b.size += len(key) + len(value)
	return nil
}

func (b *batch) Delete(key []byte) error {
	b.writes = append(b.writes, keyValue{utils.CopyBytes(key), nil, true})
	b.size += len(key)
	return nil
}

func (b *batch) Size() int { return b.size }

func (b *batch) Write() error {
	request := &rpcdbproto.WriteBatchRequest{
		Id:        atomic.AddInt64(&b.db.batchIndex, 1),
		Continues: true,
	}
	currentSize := 0
	keySet := make(map[string]struct{}, len(b.writes))
	for i := len(b.writes) - 1; i >= 0; i-- {
		kv := b.writes[i]
		key := string(kv.key)
		if _, overwritten := keySet[key]; overwritten {
			continue
		}
		keySet[key] = struct{}{}

		sizeChange := baseElementSize + len(kv.key) + len(kv.value)
		if newSize := currentSize + sizeChange; newSize > maxBatchSize {
			resp, err := b.db.client.WriteBatch(context.Background(), request)
			if err != nil {
				return err
			}
			if err := errCodeToError[resp.Err]; err != nil {
				return err
			}
			currentSize = 0
			request.Deletes = request.Deletes[:0]
			request.Puts = request.Puts[:0]
		}
		currentSize += sizeChange

		if kv.delete {
			request.Deletes = append(request.Deletes, &rpcdbproto.DeleteRequest{
				Key: kv.key,
			})
		} else {
			request.Puts = append(request.Puts, &rpcdbproto.PutRequest{
				Key:   kv.key,
				Value: kv.value,
			})
		}
	}

	request.Continues = false
	resp, err := b.db.client.WriteBatch(context.Background(), request)
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}

func (b *batch) Reset() {
	if cap(b.writes) > len(b.writes)*database.MaxExcessCapacityFactor {
		b.writes = make([]keyValue, 0, cap(b.writes)/database.CapacityReductionFactor)
	} else {
		b.writes = b.writes[:0]
	}
	b.size = 0
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, keyvalue := range b.writes {
		if keyvalue.delete {
			if err := w.Delete(keyvalue.key); err != nil {
				return err
			}
		} else if err := w.Put(keyvalue.key, keyvalue.value); err != nil {
			return err
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch { return b }

type iterator struct {
	db *DatabaseClient
	id uint64

	data []*rpcdbproto.PutRequest
	errs wrappers.Errs
}

// Next attempts to move the iterator to the next element and returns if this
// succeeded
func (it *iterator) Next() bool {
	if it.db.closed.GetValue() {
		it.data = nil
		it.errs.Add(database.ErrClosed)
		return false
	}
	if len(it.data) > 1 {
		it.data = it.data[1:]
		return true
	}

	resp, err := it.db.client.IteratorNext(context.Background(), &rpcdbproto.IteratorNextRequest{
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

	resp, err := it.db.client.IteratorError(context.Background(), &rpcdbproto.IteratorErrorRequest{
		Id: it.id,
	})
	if err != nil {
		it.errs.Add(err)
	} else {
		it.errs.Add(errCodeToError[resp.Err])
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
	resp, err := it.db.client.IteratorRelease(context.Background(), &rpcdbproto.IteratorReleaseRequest{
		Id: it.id,
	})
	if err != nil {
		it.errs.Add(err)
	} else {
		it.errs.Add(errCodeToError[resp.Err])
	}
}
