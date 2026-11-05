// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package long_running

import (
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

type LongRunningNode struct {
	*testutil.BasicNode
	bb      *LongRunningBlockBuilder
	wal     *testutil.TestWAL
	Storage *testutil.InMemStorage
	logger  *testutil.TestLogger
}

type longRunningNodeConfig struct {
	bb      *LongRunningBlockBuilder
	wal     *testutil.TestWAL
	storage *testutil.InMemStorage
	logger  *testutil.TestLogger
}

func defaultLongRunningNodeConfig(t *testing.T, nodeID common.NodeID, basicNetwork *testutil.BasicInMemoryNetwork, options longRunningNodeConfig) (simplex.EpochConfig, *LongRunningBlockBuilder, *testutil.TestWAL, *testutil.InMemStorage) {
	comm := testutil.NewTestComm(nodeID, basicNetwork, testutil.AllowAllMessages)
	bb := NewNetworkBlockBuilder(t)
	if options.bb != nil {
		bb = options.bb
	}

	epochConfig, wal, storage := testutil.DefaultTestNodeEpochConfig(t, nodeID, comm, bb)
	epochConfig.ReplicationEnabled = true
	epochConfig.MaxRoundWindow = longRunningMaxRoundWindow

	if options.wal != nil {
		epochConfig.WAL = options.wal
		wal = options.wal
	}
	if options.storage != nil {
		epochConfig.Storage = options.storage
		storage = options.storage
	}
	if options.logger != nil {
		epochConfig.Logger = options.logger
	} else {
		logger := epochConfig.Logger.(*testutil.TestLogger)
		logger.SetPanicOnError(true)
		logger.SetPanicOnWarn(true)
	}

	return epochConfig, bb, wal, storage
}

func NewLongRunningNode(t *testing.T, nodeID common.NodeID, basicNetwork *testutil.BasicInMemoryNetwork, options longRunningNodeConfig) *LongRunningNode {
	epochConfig, bb, wal, storage := defaultLongRunningNodeConfig(t, nodeID, basicNetwork, options)
	e, err := simplex.NewEpoch(epochConfig)
	require.NoError(t, err)

	logger := epochConfig.Logger.(*testutil.TestLogger)
	logger.SetPanicOnError(true)
	logger.SetPanicOnWarn(true)

	ti := &LongRunningNode{
		BasicNode: testutil.NewBasicNode(t, e, logger),
		bb:        bb,
		wal:       wal,
		Storage:   storage,
		logger:    logger,
	}

	return ti
}
