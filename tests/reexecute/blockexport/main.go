// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/reexecute"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
)

var (
	blockDirSrcArg string
	blockDirDstArg string
	startBlockArg  uint64
	endBlockArg    uint64
	chanSizeArg    int
)

func init() {
	flag.StringVar(&blockDirSrcArg, "block-dir-src", blockDirSrcArg, "Source block directory to copy from when running TestExportBlockRange.")
	flag.StringVar(&blockDirDstArg, "block-dir-dst", blockDirDstArg, "Destination block directory to write blocks into when executing TestExportBlockRange.")
	flag.Uint64Var(&startBlockArg, "start-block", 101, "Start block to begin execution (exclusive).")
	flag.Uint64Var(&endBlockArg, "end-block", 200, "End block to end execution (inclusive).")
	flag.IntVar(&chanSizeArg, "chan-size", 100, "Size of the channel to use for block processing.")

	flag.Parse()
}

func main() {
	tc := tests.NewTestContext(tests.NewDefaultLogger(""))
	defer tc.RecoverAndExit()

	r := require.New(tc)
	blockChan, err := reexecute.CreateBlockChanFromLevelDB(
		tc,
		blockDirSrcArg,
		startBlockArg,
		endBlockArg,
		chanSizeArg,
	)
	r.NoError(err)

	db, err := leveldb.New(blockDirDstArg, nil, logging.NoLog{}, prometheus.NewRegistry())
	r.NoError(err)
	tc.DeferCleanup(func() {
		r.NoError(db.Close())
	})

	batch := db.NewBatch()
	for blkResult := range blockChan {
		r.NoError(batch.Put(reexecute.BlockKey(blkResult.Height), blkResult.BlockBytes))

		if batch.Size() > 10*units.MiB {
			r.NoError(batch.Write())
			batch = db.NewBatch()
		}
	}

	r.NoError(batch.Write())
}
