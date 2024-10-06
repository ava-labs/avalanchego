// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var _ Disk = (*dbDisk)(nil)

type Disk interface {
	getShutdownType() ([]byte, error)
	setShutdownType(shutdownType []byte) error
	clearIntermediateNodes() error
	Compact(start, limit []byte) error
	HealthCheck(ctx context.Context) (interface{}, error)
	closeWithRoot(root maybe.Maybe[*node]) error
	getRootKey() ([]byte, error)
	writeChanges(ctx context.Context, changes *changeSummary) error
	Clear() error
	database.Iteratee
	getNode(key Key, hasValue bool) (*node, error)
	cacheSize() int
}

type dbDisk struct {
	tracer trace.Tracer
	db     database.Database

	valueNodeDB        *valueNodeDB
	intermediateNodeDB *intermediateNodeDB
}

func newDBDisk(db database.Database, hasher Hasher, config Config, metrics metrics) *dbDisk {
	// Share a bytes pool between the intermediateNodeDB and valueNodeDB to
	// reduce memory allocations.
	bufferPool := utils.NewBytesPool()

	intermediateNodeDB := newIntermediateNodeDB(
		db,
		bufferPool,
		metrics,
		int(config.IntermediateNodeCacheSize),
		int(config.IntermediateWriteBufferSize),
		int(config.IntermediateWriteBatchSize),
		BranchFactorToTokenSize[config.BranchFactor],
		hasher,
	)
	valueNodeDB := newValueNodeDB(
		db,
		bufferPool,
		metrics,
		int(config.ValueNodeCacheSize),
		hasher,
	)
	return newDBDiskFromComponents(db, intermediateNodeDB, valueNodeDB, config.Tracer)
}

func newDBDiskFromComponents(db database.Database, intermediateNodeDB *intermediateNodeDB, valueNodeDB *valueNodeDB, tracer trace.Tracer) *dbDisk {
	return &dbDisk{
		tracer:             tracer,
		db:                 db,
		valueNodeDB:        valueNodeDB,
		intermediateNodeDB: intermediateNodeDB,
	}
}

func (d *dbDisk) getShutdownType() ([]byte, error) {
	shutdownType, err := d.db.Get(cleanShutdownKey)
	switch err {
	case nil:
	case database.ErrNotFound:
		// If the marker wasn't found then the DB is being created for the first
		// time and there is nothing to do.
		shutdownType = hadCleanShutdown
	default:
		return nil, err
	}
	return shutdownType, nil
}

func (d *dbDisk) setShutdownType(shutdownType []byte) error {
	return d.db.Put(cleanShutdownKey, shutdownType)
}

func (d *dbDisk) clearIntermediateNodes() error {
	return database.ClearPrefix(d.db, intermediateNodePrefix, rebuildIntermediateDeletionWriteSize)
}

func (d *dbDisk) Compact(start, limit []byte) error {
	return d.db.Compact(start, limit)
}

func (d *dbDisk) HealthCheck(ctx context.Context) (interface{}, error) {
	return d.db.HealthCheck(ctx)
}

func (d *dbDisk) closeWithRoot(root maybe.Maybe[*node]) error {
	d.valueNodeDB.Close()
	// Flush intermediary nodes to disk.
	if err := d.intermediateNodeDB.Flush(); err != nil {
		return err
	}

	var (
		batch = d.db.NewBatch()
		err   error
	)
	// Write the root key
	if root.IsNothing() {
		err = batch.Delete(rootDBKey)
	} else {
		rootKey := encodeKey(root.Value().key)
		err = batch.Put(rootDBKey, rootKey)
	}
	if err != nil {
		return err
	}

	// Write the clean shutdown marker
	if err := batch.Put(cleanShutdownKey, hadCleanShutdown); err != nil {
		return err
	}
	return batch.Write()
}

func (d *dbDisk) getRootKey() ([]byte, error) {
	rootKeyBytes, err := d.db.Get(rootDBKey)
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil // Root isn't on disk.
	}
	if err != nil {
		return nil, err
	}

	// Root is on disk.
	return rootKeyBytes, nil
}

func (d *dbDisk) writeChanges(ctx context.Context, changes *changeSummary) error {
	valueNodeBatch := d.db.NewBatch()
	if err := d.applyChanges(ctx, valueNodeBatch, changes); err != nil {
		return err
	}

	if err := d.commitValueChanges(ctx, valueNodeBatch); err != nil {
		return err
	}
	return nil
}

// applyChanges takes the [changes] and applies them to [db.intermediateNodeDB]
// and [valueNodeBatch].
//
// assumes [db.lock] is held
func (d *dbDisk) applyChanges(ctx context.Context, valueNodeBatch database.KeyValueWriterDeleter, changes *changeSummary) error {
	_, span := d.tracer.Start(ctx, "Disk.applyChanges")
	defer span.End()

	for key, nodeChange := range changes.nodes {
		shouldAddIntermediate := nodeChange.after != nil && !nodeChange.after.hasValue()
		shouldDeleteIntermediate := !shouldAddIntermediate && nodeChange.before != nil && !nodeChange.before.hasValue()

		shouldAddValue := nodeChange.after != nil && nodeChange.after.hasValue()
		shouldDeleteValue := !shouldAddValue && nodeChange.before != nil && nodeChange.before.hasValue()

		if shouldAddIntermediate {
			if err := d.intermediateNodeDB.Put(key, nodeChange.after); err != nil {
				return err
			}
		} else if shouldDeleteIntermediate {
			if err := d.intermediateNodeDB.Delete(key); err != nil {
				return err
			}
		}

		if shouldAddValue {
			if err := d.valueNodeDB.Write(valueNodeBatch, key, nodeChange.after); err != nil {
				return err
			}
		} else if shouldDeleteValue {
			if err := d.valueNodeDB.Write(valueNodeBatch, key, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// commitValueChanges is a thin wrapper around [valueNodeBatch.Write()] to
// provide tracing.
func (d *dbDisk) commitValueChanges(ctx context.Context, valueNodeBatch database.Batch) error {
	_, span := d.tracer.Start(ctx, "Disk.commitValueChanges")
	defer span.End()

	return valueNodeBatch.Write()
}

func (d *dbDisk) NewIterator() database.Iterator {
	return d.NewIteratorWithStartAndPrefix(nil, nil)
}

func (d *dbDisk) NewIteratorWithStart(start []byte) database.Iterator {
	return d.NewIteratorWithStartAndPrefix(start, nil)
}

func (d *dbDisk) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return d.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (d *dbDisk) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	return d.valueNodeDB.newIteratorWithStartAndPrefix(start, prefix)
}

func (d *dbDisk) Clear() error {
	if err := d.valueNodeDB.Clear(); err != nil {
		return err
	}
	return d.intermediateNodeDB.Clear()
}

func (d *dbDisk) getNode(key Key, hasValue bool) (*node, error) {
	if hasValue {
		return d.valueNodeDB.Get(key)
	}
	return d.intermediateNodeDB.Get(key)
}

func (d *dbDisk) cacheSize() int {
	return d.valueNodeDB.nodeCache.Len() + d.intermediateNodeDB.writeBuffer.currentSize
}
