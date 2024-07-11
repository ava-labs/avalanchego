// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
)

var processingBlockIndexPrefix = []byte("processing_block")

type ProcessingBlockIndex interface {
	PutProcessingBlock(blkID ids.ID) error
	HasProcessingBlock(blkID ids.ID) (bool, error)
	DeleteProcessingBlock(blkID ids.ID) error
}

// newProcessingBlockIndex returns an instance of processingBlockIndex
func newProcessingBlockIndex(db database.Database) processingBlockIndex {
	return processingBlockIndex{
		db: prefixdb.New(processingBlockIndexPrefix, db),
	}
}

// processingBlockIndex contains an index of blocks that have not been decided
// yet by consensus.
type processingBlockIndex struct {
	db database.Database
}

func (p processingBlockIndex) PutProcessingBlock(blkID ids.ID) error {
	return p.db.Put(blkID[:], nil)
}

func (p processingBlockIndex) HasProcessingBlock(blkID ids.ID) (bool, error) {
	return p.db.Has(blkID[:])
}

func (p processingBlockIndex) DeleteProcessingBlock(blkID ids.ID) error {
	return p.db.Delete(blkID[:])
}
