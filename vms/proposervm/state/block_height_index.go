// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	cacheSize = 8192 // max cache entries

	deleteBatchSize = 8192

	// Sleep [sleepDurationMultiplier]x (5x) the amount of time we spend processing the block
	// to ensure the async indexing does not bottleneck the node.
	sleepDurationMultiplier = 5
)

var (
	_ HeightIndex = &heightIndex{}

	heightPrefix   = []byte("height")
	metadataPrefix = []byte("metadata")

	forkKey          = []byte("fork")
	checkpointKey    = []byte("checkpoint")
	resetOccurredKey = []byte("resetOccurred")
)

type HeightIndexGetter interface {
	GetBlockIDAtHeight(height uint64) (ids.ID, error)

	// Fork height is stored when the first post-fork block/option is accepted.
	// Before that, fork height won't be found.
	GetForkHeight() (uint64, error)
	IsIndexEmpty() (bool, error)
	HasIndexReset() (bool, error)
}

type HeightIndexWriter interface {
	SetBlockIDAtHeight(height uint64, blkID ids.ID) error
	SetForkHeight(height uint64) error
	SetIndexHasReset() error
}

// A checkpoint is the blockID of the next block to be considered
// for height indexing. We store checkpoints to be able to duly resume
// long-running re-indexing ops.
type HeightIndexBatchSupport interface {
	versiondb.Commitable

	GetCheckpoint() (ids.ID, error)
	SetCheckpoint(blkID ids.ID) error
	DeleteCheckpoint() error
}

// HeightIndex contains mapping of blockHeights to accepted proposer block IDs
// along with some metadata (fork height and checkpoint).
type HeightIndex interface {
	HeightIndexWriter
	HeightIndexGetter
	HeightIndexBatchSupport

	// ResetHeightIndex deletes all index DB entries
	ResetHeightIndex(logging.Logger, versiondb.Commitable) error
}

type heightIndex struct {
	versiondb.Commitable

	// Caches block height -> proposerVMBlockID.
	heightsCache cache.Cacher

	heightDB   database.Database
	metadataDB database.Database
}

func NewHeightIndex(db database.Database, commitable versiondb.Commitable) HeightIndex {
	return &heightIndex{
		Commitable: commitable,

		heightsCache: &cache.LRU{Size: cacheSize},
		heightDB:     prefixdb.New(heightPrefix, db),
		metadataDB:   prefixdb.New(metadataPrefix, db),
	}
}

func (hi *heightIndex) IsIndexEmpty() (bool, error) {
	heightsIsEmpty, err := database.IsEmpty(hi.heightDB)
	if err != nil {
		return false, err
	}
	if !heightsIsEmpty {
		return false, nil
	}
	return database.IsEmpty(hi.metadataDB)
}

func (hi *heightIndex) HasIndexReset() (bool, error) {
	return hi.metadataDB.Has(resetOccurredKey)
}

func (hi *heightIndex) SetIndexHasReset() error {
	return hi.metadataDB.Put(resetOccurredKey, nil)
}

func (hi *heightIndex) ResetHeightIndex(log logging.Logger, baseDB versiondb.Commitable) error {
	var (
		itHeight   = hi.heightDB.NewIterator()
		itMetadata = hi.metadataDB.NewIterator()
	)
	defer func() {
		itHeight.Release()
		itMetadata.Release()
	}()

	// clear height cache
	hi.heightsCache.Flush()

	// clear heightDB
	deleteCount := 0
	processingStart := time.Now()
	for itHeight.Next() {
		if err := hi.heightDB.Delete(itHeight.Key()); err != nil {
			return err
		}

		deleteCount++
		if deleteCount%deleteBatchSize == 0 {
			if err := hi.Commit(); err != nil {
				return err
			}
			if err := baseDB.Commit(); err != nil {
				return err
			}

			log.Info("deleted height index entries",
				zap.Int("numDeleted", deleteCount),
			)

			// every deleteBatchSize ops, sleep to avoid clogging the node on this
			processingDuration := time.Since(processingStart)
			// Sleep [sleepDurationMultiplier]x (5x) the amount of time we spend processing the block
			// to ensure the indexing does not bottleneck the node.
			time.Sleep(processingDuration * sleepDurationMultiplier)
			processingStart = time.Now()

			if err := itHeight.Error(); err != nil {
				return err
			}

			// release iterator so underlying db does not hold on to the previous state
			itHeight.Release()
			itHeight = hi.heightDB.NewIterator()
		}
	}

	// clear metadataDB
	for itMetadata.Next() {
		if err := hi.metadataDB.Delete(itMetadata.Key()); err != nil {
			return err
		}
	}

	errs := wrappers.Errs{}
	errs.Add(
		itHeight.Error(),
		itMetadata.Error(),
	)
	if errs.Errored() {
		return errs.Err
	}

	if err := hi.SetIndexHasReset(); err != nil {
		return err
	}

	if err := hi.Commit(); err != nil {
		return err
	}
	return baseDB.Commit()
}

func (hi *heightIndex) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if blkIDIntf, found := hi.heightsCache.Get(height); found {
		res, _ := blkIDIntf.(ids.ID)
		return res, nil
	}

	key := database.PackUInt64(height)
	blkID, err := database.GetID(hi.heightDB, key)
	if err != nil {
		return ids.Empty, err
	}
	hi.heightsCache.Put(height, blkID)
	return blkID, err
}

func (hi *heightIndex) SetBlockIDAtHeight(height uint64, blkID ids.ID) error {
	hi.heightsCache.Put(height, blkID)
	key := database.PackUInt64(height)
	return database.PutID(hi.heightDB, key, blkID)
}

func (hi *heightIndex) GetForkHeight() (uint64, error) {
	return database.GetUInt64(hi.metadataDB, forkKey)
}

func (hi *heightIndex) SetForkHeight(height uint64) error {
	return database.PutUInt64(hi.metadataDB, forkKey, height)
}

func (hi *heightIndex) GetCheckpoint() (ids.ID, error) {
	return database.GetID(hi.metadataDB, checkpointKey)
}

func (hi *heightIndex) SetCheckpoint(blkID ids.ID) error {
	return database.PutID(hi.metadataDB, checkpointKey, blkID)
}

func (hi *heightIndex) DeleteCheckpoint() error {
	return hi.metadataDB.Delete(checkpointKey)
}
