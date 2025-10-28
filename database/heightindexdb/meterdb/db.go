// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meterdb

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
)

const methodLabel = "method"

var (
	_ database.HeightIndex = (*Database)(nil)

	methodLabels = []string{methodLabel}
	putLabel     = prometheus.Labels{
		methodLabel: "put",
	}
	getLabel = prometheus.Labels{
		methodLabel: "get",
	}
	hasLabel = prometheus.Labels{
		methodLabel: "has",
	}
	closeLabel = prometheus.Labels{
		methodLabel: "close",
	}
)

// Database tracks the amount of time each operation takes and how many bytes
// are read/written to the underlying height index database.
type Database struct {
	heightDB database.HeightIndex

	calls    *prometheus.CounterVec
	duration *prometheus.GaugeVec
	size     *prometheus.CounterVec
}

func New(
	reg prometheus.Registerer,
	namespace string,
	db database.HeightIndex,
) (*Database, error) {
	meterDB := &Database{
		heightDB: db,
		calls: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "calls",
				Help:      "number of calls to the database",
			},
			methodLabels,
		),
		duration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "duration",
				Help:      "time spent in database calls (ns)",
			},
			methodLabels,
		),
		size: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "size",
				Help:      "size of data passed in database calls",
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

func (db *Database) Put(height uint64, block []byte) error {
	start := time.Now()
	err := db.heightDB.Put(height, block)
	duration := time.Since(start)

	db.calls.With(putLabel).Inc()
	db.duration.With(putLabel).Add(float64(duration.Nanoseconds()))
	db.size.With(putLabel).Add(float64(len(block)))
	return err
}

func (db *Database) Get(height uint64) ([]byte, error) {
	start := time.Now()
	block, err := db.heightDB.Get(height)
	duration := time.Since(start)

	db.calls.With(getLabel).Inc()
	db.duration.With(getLabel).Add(float64(duration.Nanoseconds()))
	db.size.With(getLabel).Add(float64(len(block)))
	return block, err
}

func (db *Database) Has(height uint64) (bool, error) {
	start := time.Now()
	has, err := db.heightDB.Has(height)
	duration := time.Since(start)

	db.calls.With(hasLabel).Inc()
	db.duration.With(hasLabel).Add(float64(duration.Nanoseconds()))
	return has, err
}

func (db *Database) Close() error {
	start := time.Now()
	err := db.heightDB.Close()
	duration := time.Since(start)

	db.calls.With(closeLabel).Inc()
	db.duration.With(closeLabel).Add(float64(duration.Nanoseconds()))
	return err
}
