// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"fmt"

	"golang.org/x/net/context"

	"github.com/ava-labs/avalanche-go/database"
	"github.com/ava-labs/avalanche-go/database/nodb"
	"github.com/ava-labs/avalanche-go/database/rpcdb/rpcdbproto"
	"github.com/ava-labs/avalanche-go/utils"
	"github.com/ava-labs/avalanche-go/utils/wrappers"
)

var (
	errClosed   = fmt.Sprintf("rpc error: code = Unknown desc = %s", database.ErrClosed)
	errNotFound = fmt.Sprintf("rpc error: code = Unknown desc = %s", database.ErrNotFound)
)

// DatabaseClient is an implementation of database that talks over RPC.
type DatabaseClient struct{ client rpcdbproto.DatabaseClient }

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
		return false, updateError(err)
	}
	return resp.Has, nil
}

// Get attempts to return the value that was mapped to the key that was provided
func (db *DatabaseClient) Get(key []byte) ([]byte, error) {
	resp, err := db.client.Get(context.Background(), &rpcdbproto.GetRequest{
		Key: key,
	})
	if err != nil {
		return nil, updateError(err)
	}
	return resp.Value, nil
}

// Put attempts to set the value this key maps to
func (db *DatabaseClient) Put(key, value []byte) error {
	_, err := db.client.Put(context.Background(), &rpcdbproto.PutRequest{
		Key:   key,
		Value: value,
	})
	return updateError(err)
}

// Delete attempts to remove any mapping from the key
func (db *DatabaseClient) Delete(key []byte) error {
	_, err := db.client.Delete(context.Background(), &rpcdbproto.DeleteRequest{
		Key: key,
	})
	return updateError(err)
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
		return &nodb.Iterator{Err: updateError(err)}
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
		return "", updateError(err)
	}
	return resp.Stat, nil
}

// Compact attempts to optimize the space utilization in the provided range
func (db *DatabaseClient) Compact(start, limit []byte) error {
	_, err := db.client.Compact(context.Background(), &rpcdbproto.CompactRequest{
		Start: start,
		Limit: limit,
	})
	return updateError(err)
}

// Close attempts to close the database
func (db *DatabaseClient) Close() error {
	_, err := db.client.Close(context.Background(), &rpcdbproto.CloseRequest{})
	return updateError(err)
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
	b.size += len(value)
	return nil
}

func (b *batch) Delete(key []byte) error {
	b.writes = append(b.writes, keyValue{utils.CopyBytes(key), nil, true})
	b.size++
	return nil
}

func (b *batch) ValueSize() int { return b.size }

func (b *batch) Write() error {
	request := &rpcdbproto.WriteBatchRequest{}

	keySet := make(map[string]struct{}, len(b.writes))
	for i := len(b.writes) - 1; i >= 0; i-- {
		kv := b.writes[i]
		key := string(kv.key)
		if _, overwritten := keySet[key]; overwritten {
			continue
		}
		keySet[key] = struct{}{}

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

	_, err := b.db.client.WriteBatch(context.Background(), request)
	return updateError(err)
}

func (b *batch) Reset() {
	if cap(b.writes) > len(b.writes)*database.MaxExcessCapacityFactor {
		b.writes = make([]keyValue, 0, cap(b.writes)/database.CapacityReductionFactor)
	} else {
		b.writes = b.writes[:0]
	}
	b.size = 0
}

func (b *batch) Replay(w database.KeyValueWriter) error {
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
	db    *DatabaseClient
	id    uint64
	key   []byte
	value []byte
	errs  wrappers.Errs
}

// Next attempts to move the iterator to the next element and returns if this
// succeeded
func (it *iterator) Next() bool {
	resp, err := it.db.client.IteratorNext(context.Background(), &rpcdbproto.IteratorNextRequest{
		Id: it.id,
	})
	it.errs.Add(updateError(err))

	if err != nil {
		return false
	}
	it.key = resp.Key
	it.value = resp.Value
	return resp.FoundNext
}

// Error returns any that occurred while iterating
func (it *iterator) Error() error {
	if it.errs.Errored() {
		return it.errs.Err
	}

	_, err := it.db.client.IteratorError(context.Background(), &rpcdbproto.IteratorErrorRequest{
		Id: it.id,
	})
	it.errs.Add(updateError(err))
	return it.errs.Err
}

// Key returns the key of the current element
func (it *iterator) Key() []byte { return it.key }

// Value returns the value of the current element
func (it *iterator) Value() []byte { return it.value }

// Release frees any resources held by the iterator
func (it *iterator) Release() {
	_, err := it.db.client.IteratorRelease(context.Background(), &rpcdbproto.IteratorReleaseRequest{
		Id: it.id,
	})
	it.errs.Add(updateError(err))
}

// updateError sets the error value to the errors required by the Database
// interface
func updateError(err error) error {
	if err == nil {
		return nil
	}

	switch err.Error() {
	case errClosed:
		return database.ErrClosed
	case errNotFound:
		return database.ErrNotFound
	default:
		return err
	}
}
