// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"flag"
	"math/big"
	"net/http/httptest"
	"path/filepath"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/reexecute"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	currentStateDirArg string
	startBlockArg      uint64
	endBlockArg        uint64
)

func init() {
	evm.RegisterAllLibEVMExtras()

	flag.StringVar(&currentStateDirArg, "current-state-dir", currentStateDirArg, "Current state directory including VM DB and Chain Data Directory.")
	flag.Uint64Var(&startBlockArg, "start-block", 101, "Start block to begin execution (inclusive).")
	flag.Uint64Var(&endBlockArg, "end-block", 200, "End block to end execution (inclusive).")
	flag.Parse()
}

// main verifies that historical state is accessible for archival node by
// iterating over a range of blocks and querying the nonce of the zero address
// at each block height. This confirms the node can serve state queries for
// arbitrary historical blocks.
func main() {
	tc := tests.NewTestContext(tests.NewDefaultLogger("state-access"))
	defer tc.RecoverAndExit()

	r := require.New(tc)
	ctx := context.Background()

	var (
		vmDBDir      = filepath.Join(currentStateDirArg, "db")
		chainDataDir = filepath.Join(currentStateDirArg, "chain-data-dir")
	)

	db, err := leveldb.New(vmDBDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	r.NoError(err)

	firewoodArchiveConfig := `{
		"state-scheme": "firewood",
		"snapshot-cache": 0,
		"pruning-enabled": false,
		"state-sync-enabled": false
	}`

	vm, err := reexecute.NewMainnetCChainVM(
		ctx,
		db,
		chainDataDir,
		[]byte(firewoodArchiveConfig),
		metrics.NewPrefixGatherer(),
		prometheus.NewRegistry(),
	)
	r.NoError(err)

	handlers, err := vm.CreateHandlers(ctx)
	r.NoError(err)

	ethRPCEndpoint := "/rpc"
	server := httptest.NewServer(handlers[ethRPCEndpoint])
	tc.DeferCleanup(server.Close)

	client, err := ethclient.Dial(server.URL)
	r.NoError(err)

	for i := startBlockArg; i <= endBlockArg; i++ {
		nonce, err := client.NonceAt(ctx, common.Address{}, big.NewInt(int64(i)))
		r.NoErrorf(err, "failed to get nonce at block %d", i)
		r.Zero(nonce)
	}
}
