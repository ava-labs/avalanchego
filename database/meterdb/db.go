// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meterdb

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
)

const methodLabel = "method"

var (
	_ database.Database = (*Database)(nil)
	_ database.Batch    = (*batch)(nil)
	_ database.Iterator = (*iterator)(nil)

	methodLabels = []string{methodLabel}
	hasLabel     = prometheus.Labels{
		methodLabel: "has",
	}
	getLabel = prometheus.Labels{
		methodLabel: "get",
	}
	putLabel = prometheus.Labels{
		methodLabel: "put",
	}
	deleteLabel = prometheus.Labels{
		methodLabel: "delete",
	}
	newBatchLabel = prometheus.Labels{
		methodLabel: "new_batch",
	}
	newIteratorLabel = prometheus.Labels{
		methodLabel: "new_iterator",
	}
	compactLabel = prometheus.Labels{
		methodLabel: "compact",
	}
	closeLabel = prometheus.Labels{
		methodLabel: "close",
	}
	healthCheckLabel = prometheus.Labels{
		methodLabel: "health_check",
	}
	batchPutLabel = prometheus.Labels{
		methodLabel: "batch_put",
	}
	batchDeleteLabel = prometheus.Labels{
		methodLabel: "batch_delete",
	}
	batchSizeLabel = prometheus.Labels{
		methodLabel: "batch_size",
	}
	batchWriteLabel = prometheus.Labels{
		methodLabel: "batch_write",
	}
	batchResetLabel = prometheus.Labels{
		methodLabel: "batch_reset",
	}
	batchReplayLabel = prometheus.Labels{
		methodLabel: "batch_replay",
	}
	batchInnerLabel = prometheus.Labels{
		methodLabel: "batch_inner",
	}
	iteratorNextLabel = prometheus.Labels{
		methodLabel: "iterator_next",
	}
	iteratorErrorLabel = prometheus.Labels{
		methodLabel: "iterator_error",
	}
	iteratorKeyLabel = prometheus.Labels{
		methodLabel: "iterator_key",
	}
	iteratorValueLabel = prometheus.Labels{
		methodLabel: "iterator_value",
	}
	iteratorReleaseLabel = prometheus.Labels{
		methodLabel: "iterator_release",
	}
)

// Database tracks the amount of time each operation takes and how many bytes
// are read/written to the underlying database instance.
type Database struct {
	db database.Database

	calls    *prometheus.CounterVec
	duration *prometheus.GaugeVec
	size     *prometheus.CounterVec
}

// New returns a new database with added metrics
func New(
	reg prometheus.Registerer,
	db database.Database,
) (*Database, error) {
	meterDB := &Database{
		db: db,
		calls: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "calls",
				Help: "number of calls to the database",
			},
			methodLabels,
		),
		duration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "duration",
				Help: "time spent in database calls (ns)",
			},
			methodLabels,
		),
		size: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "size",
				Help: "size of data passed in database calls",
			},
			methodLabels,
		),
	}
	return meterDB, errors.Join(
		reg.Register(meterDB.calls),
		reg.Register(meterDB.duration),
		reg.Register(meterDB.size),
	)
}

func (db *Database) Has(key []byte) (bool, error) {
	start := time.Now()
	has, err := db.db.Has(key)
	duration := time.Since(start)

	db.calls.With(hasLabel).Inc()
	db.duration.With(hasLabel).Add(float64(duration))
	db.size.With(hasLabel).Add(float64(len(key)))
	return has, err
}

func (db *Database) Get(key []byte) ([]byte, error) {
	start := time.Now()
	value, err := db.db.Get(key)
	duration := time.Since(start)

	db.calls.With(getLabel).Inc()
	db.duration.With(getLabel).Add(float64(duration))
	db.size.With(getLabel).Add(float64(len(key) + len(value)))
	return value, err
}

func (db *Database) Put(key, value []byte) error {
	start := time.Now()
	err := db.db.Put(key, value)
	duration := time.Since(start)

	db.calls.With(putLabel).Inc()
	db.duration.With(putLabel).Add(float64(duration))
	db.size.With(putLabel).Add(float64(len(key) + len(value)))
	return err
}

func (db *Database) Delete(key []byte) error {
	start := time.Now()
	err := db.db.Delete(key)
	duration := time.Since(start)

	db.calls.With(deleteLabel).Inc()
	db.duration.With(deleteLabel).Add(float64(duration))
	db.size.With(deleteLabel).Add(float64(len(key)))
	return err
}

func (db *Database) NewBatch() database.Batch {
	start := time.Now()
	b := &batch{
		batch: db.db.NewBatch(),
		db:    db,
	}
	duration := time.Since(start)

	db.calls.With(newBatchLabel).Inc()
	db.duration.With(newBatchLabel).Add(float64(duration))
	return b
}

func (db *Database) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (db *Database) NewIteratorWithStartAndPrefix(
	start,
	prefix []byte,
) database.Iterator {
	startTime := time.Now()
	it := &iterator{
		iterator: db.db.NewIteratorWithStartAndPrefix(start, prefix),
		db:       db,
	}
	duration := time.Since(startTime)

	db.calls.With(newIteratorLabel).Inc()
	db.duration.With(newIteratorLabel).Add(float64(duration))
	return it
}

func (db *Database) Compact(start, limit []byte) error {
	startTime := time.Now()
	err := db.db.Compact(start, limit)
	duration := time.Since(startTime)

	db.calls.With(compactLabel).Inc()
	db.duration.With(compactLabel).Add(float64(duration))
	return err
}

func (db *Database) Close() error {
	start := time.Now()
	err := db.db.Close()
	duration := time.Since(start)

	db.calls.With(closeLabel).Inc()
	db.duration.With(closeLabel).Add(float64(duration))
	return err
}

func (db *Database) HealthCheck(ctx context.Context) (interface{}, error) {
	start := time.Now()
	result, err := db.db.HealthCheck(ctx)
	duration := time.Since(start)

	db.calls.With(healthCheckLabel).Inc()
	db.duration.With(healthCheckLabel).Add(float64(duration))
	return result, err
}

type batch struct {
	batch database.Batch
	db    *Database
}

func (b *batch) Put(key, value []byte) error {
	start := time.Now()
	err := b.batch.Put(key, value)
	duration := time.Since(start)

	b.db.calls.With(batchPutLabel).Inc()
	b.db.duration.With(batchPutLabel).Add(float64(duration))
	b.db.size.With(batchPutLabel).Add(float64(len(key) + len(value)))
	return err
}

func (b *batch) Delete(key []byte) error {
	start := time.Now()
	err := b.batch.Delete(key)
	duration := time.Since(start)

	b.db.calls.With(batchDeleteLabel).Inc()
	b.db.duration.With(batchDeleteLabel).Add(float64(duration))
	b.db.size.With(batchDeleteLabel).Add(float64(len(key)))
	return err
}

func (b *batch) Size() int {
	start := time.Now()
	size := b.batch.Size()
	duration := time.Since(start)

	b.db.calls.With(batchSizeLabel).Inc()
	b.db.duration.With(batchSizeLabel).Add(float64(duration))
	return size
}

func (b *batch) Write() error {
	start := time.Now()
	err := b.batch.Write()
	duration := time.Since(start)
	size := b.batch.Size()

	b.db.calls.With(batchWriteLabel).Inc()
	b.db.duration.With(batchWriteLabel).Add(float64(duration))
	b.db.size.With(batchWriteLabel).Add(float64(size))
	return err
}

func (b *batch) Reset() {
	start := time.Now()
	b.batch.Reset()
	duration := time.Since(start)

	b.db.calls.With(batchResetLabel).Inc()
	b.db.duration.With(batchResetLabel).Add(float64(duration))
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	start := time.Now()
	err := b.batch.Replay(w)
	duration := time.Since(start)

	b.db.calls.With(batchReplayLabel).Inc()
	b.db.duration.With(batchReplayLabel).Add(float64(duration))
	return err
}

func (b *batch) Inner() database.Batch {
	start := time.Now()
	inner := b.batch.Inner()
	duration := time.Since(start)

	b.db.calls.With(batchInnerLabel).Inc()
	b.db.duration.With(batchInnerLabel).Add(float64(duration))
	return inner
}

type iterator struct {
	iterator database.Iterator
	db       *Database
}

func (it *iterator) Next() bool {
	start := time.Now()
	next := it.iterator.Next()
	duration := time.Since(start)
	size := len(it.iterator.Key()) + len(it.iterator.Value())

	it.db.calls.With(iteratorNextLabel).Inc()
	it.db.duration.With(iteratorNextLabel).Add(float64(duration))
	it.db.size.With(iteratorNextLabel).Add(float64(size))
	return next
}

func (it *iterator) Error() error {
	start := time.Now()
	err := it.iterator.Error()
	duration := time.Since(start)

	it.db.calls.With(iteratorErrorLabel).Inc()
	it.db.duration.With(iteratorErrorLabel).Add(float64(duration))
	return err
}

func (it *iterator) Key() []byte {
	start := time.Now()
	key := it.iterator.Key()
	duration := time.Since(start)

	it.db.calls.With(iteratorKeyLabel).Inc()
	it.db.duration.With(iteratorKeyLabel).Add(float64(duration))
	return key
}

func (it *iterator) Value() []byte {
	start := time.Now()
	value := it.iterator.Value()
	duration := time.Since(start)

	it.db.calls.With(iteratorValueLabel).Inc()
	it.db.duration.With(iteratorValueLabel).Add(float64(duration))
	return value
}

func (it *iterator) Release() {
	start := time.Now()
	it.iterator.Release()
	duration := time.Since(start)

	it.db.calls.With(iteratorReleaseLabel).Inc()
	it.db.duration.With(iteratorReleaseLabel).Add(float64(duration))
}
