// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reexecute

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
)

// ExportBlockRange copies blocks from a source LevelDB directory to a
// destination LevelDB directory for the specified block range [startBlock, endBlock].
func ExportBlockRange(tb testing.TB, blockDirSrc string, blockDirDst string, startBlock, endBlock uint64, chanSize int) {
	r := require.New(tb)
	blockChan, err := createBlockChanFromLevelDB(tb, blockDirSrc, startBlock, endBlock, chanSize)
	r.NoError(err)

	db, err := leveldb.New(blockDirDst, nil, logging.NoLog{}, prometheus.NewRegistry())
	r.NoError(err)
	tb.Cleanup(func() {
		r.NoError(db.Close())
	})

	batch := db.NewBatch()
	for blkResult := range blockChan {
		r.NoError(batch.Put(blockKey(blkResult.height), blkResult.blockBytes))

		if batch.Size() > 10*units.MiB {
			r.NoError(batch.Write())
			batch = db.NewBatch()
		}
	}

	r.NoError(batch.Write())
}
