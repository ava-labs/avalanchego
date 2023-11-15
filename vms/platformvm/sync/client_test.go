// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
	xsync "github.com/ava-labs/avalanchego/x/sync"
)

func TestNewClient(t *testing.T) {
	var (
		require       = require.New(t)
		metadataDB    = memdb.New()
		managerConfig = xsync.ManagerConfig{
			SimultaneousWorkLimit: 1,
		}
		onDoneCalled bool
		onDone       = func(error) { onDoneCalled = true }
		config       = ClientConfig{
			ManagerConfig: managerConfig,
			Enabled:       true,
			OnDone:        onDone,
		}
		client = NewClient(config, metadataDB)
	)

	require.NotNil(client)
	require.NotNil(config, client.config)
	require.Equal(config.ManagerConfig, managerConfig)
	require.Equal(config.Enabled, client.config.Enabled)
	require.Equal(metadataDB, client.metadataDB)

	// Can't use reflect.Equal to test function equality
	// so do this instead.
	client.config.OnDone(nil)
	require.True(onDoneCalled)
}

func TestClientStateSyncEnabled(t *testing.T) {
	require := require.New(t)

	client := NewClient(ClientConfig{
		Enabled: true,
	}, memdb.New())

	enabled, err := client.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)

	client.config.Enabled = false
	enabled, err = client.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)
}

func TestClientParseStateSummary(t *testing.T) {
	var (
		require     = require.New(t)
		blockID     = ids.GenerateTestID()
		blockNumber = uint64(1337)
		rootID      = ids.GenerateTestID()
		client      = NewClient(ClientConfig{}, memdb.New())
	)
	summary, err := NewSummary(blockID, blockNumber, rootID)
	require.NoError(err)
	summaryBytes := summary.Bytes()

	parsedSummary, err := client.ParseStateSummary(context.Background(), summary.Bytes())
	require.NoError(err)

	require.Equal(blockNumber, parsedSummary.Height())
	require.Equal(summaryBytes, parsedSummary.Bytes())
}

func TestClientAcceptSyncSummary(t *testing.T) {
	var (
		require          = require.New(t)
		now              = time.Now().UnixNano()
		r                = rand.New(rand.NewSource(now)) // #nosec G404
		ctrl             = gomock.NewController(t)
		onDoneErrChan    = make(chan error)
		onGotSummaryChan = make(chan struct{})
		syncClient       = xsync.NewMockClient(ctrl)
	)

	newDBConfig := func() merkledb.Config {
		return merkledb.Config{
			BranchFactor:              merkledb.BranchFactor16,
			EvictionBatchSize:         1_000,
			HistoryLength:             1_000,
			ValueNodeCacheSize:        1_000,
			IntermediateNodeCacheSize: 1_000,
			// Need a new [Reg] for each database to avoid
			// duplicate metrics collector registration error.
			Reg:        prometheus.NewRegistry(),
			TraceLevel: merkledb.NoTrace,
			Tracer:     trace.Noop,
		}
	}

	// Make a database that will be synced.
	syncDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDBConfig(),
	)
	require.NoError(err)

	// Make a target database.
	targetDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDBConfig(),
	)
	require.NoError(err)

	// Populate the target database.
	for i := 0; i < 1_000; i++ {
		key := make([]byte, 32)
		_, _ = r.Read(key)
		value := make([]byte, 32)
		_, _ = r.Read(value)
		require.NoError(targetDB.Put(key, value))
	}
	targetRootID, err := targetDB.GetMerkleRoot(context.Background())
	require.NoError(err)

	syncClient.EXPECT().GetRangeProof(
		gomock.Any(),
		gomock.Any(),
	).DoAndReturn(func(_ context.Context, req *pb.SyncGetRangeProofRequest) (*merkledb.RangeProof, error) {
		// Don't finish syncing until we've checked that
		// GetOngoingSyncStateSummary works as expected.
		<-onGotSummaryChan

		rootID, err := ids.ToID(req.RootHash)
		require.NoError(err)
		require.Equal(targetRootID, rootID)

		start := maybe.Nothing[[]byte]()
		if req.StartKey != nil && !req.StartKey.IsNothing {
			start = maybe.Some(req.StartKey.Value)
		}

		end := maybe.Nothing[[]byte]()
		if req.EndKey != nil && !req.EndKey.IsNothing {
			end = maybe.Some(req.EndKey.Value)
		}

		return targetDB.GetRangeProof(
			context.Background(),
			start,
			end,
			int(req.KeyLimit),
		)
	}).AnyTimes()

	client := NewClient(ClientConfig{
		ManagerConfig: xsync.ManagerConfig{
			DB:                    syncDB,
			Client:                syncClient,
			SimultaneousWorkLimit: 1,
			Log:                   logging.NoLog{},
			BranchFactor:          merkledb.BranchFactor16,
		},
		Enabled: true,
		OnDone: func(err error) {
			onDoneErrChan <- err
		},
	}, memdb.New())

	// Make a new rawSummary whose root ID is [targetRootID].
	rawSummary, err := NewSummary(ids.GenerateTestID(), 1337, targetRootID)
	require.NoError(err)
	// Need to make the summary like this so that its onAccept callback is set.
	summary, err := client.ParseStateSummary(context.Background(), rawSummary.Bytes())
	require.NoError(err)

	// Start syncing.
	syncMode, err := summary.Accept(context.Background())
	require.Equal(block.StateSyncStatic, syncMode)
	require.NoError(err)

	// Make sure we recorded the ongoing sync.
	ongoingSummary, err := client.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.Equal(summary.ID(), ongoingSummary.ID())

	// Allow syncing to finish.
	close(onGotSummaryChan)

	// Wait for syncing to finish / assert that
	// the OnDone callback was called.
	require.NoError(<-onDoneErrChan)

	// Make sure syncing resulted in the correct root ID.
	rootID, err := syncDB.GetMerkleRoot(context.Background())
	require.NoError(err)
	require.Equal(targetRootID, rootID)

	// Make sure the ongoing sync was removed.
	_, err = client.GetOngoingSyncStateSummary(context.Background())
	require.ErrorIs(err, database.ErrNotFound)
}

func TestClientShutdown(t *testing.T) {
	var (
		require        = require.New(t)
		now            = time.Now().UnixNano()
		r              = rand.New(rand.NewSource(now)) // #nosec G404
		ctrl           = gomock.NewController(t)
		onDoneErrChan  = make(chan error)
		syncClient     = xsync.NewMockClient(ctrl)
		onShutdownChan = make(chan struct{})
	)

	newDBConfig := func() merkledb.Config {
		return merkledb.Config{
			BranchFactor:              merkledb.BranchFactor16,
			EvictionBatchSize:         1_000,
			HistoryLength:             1_000,
			ValueNodeCacheSize:        1_000,
			IntermediateNodeCacheSize: 1_000,
			// Need a new [Reg] for each database to avoid
			// duplicate metrics collector registration error.
			Reg:        prometheus.NewRegistry(),
			TraceLevel: merkledb.NoTrace,
			Tracer:     trace.Noop,
		}
	}

	// Make a database that will be synced.
	syncDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDBConfig(),
	)
	require.NoError(err)

	// Make a target database.
	targetDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDBConfig(),
	)
	require.NoError(err)

	// Populate the target database.
	for i := 0; i < 1_000; i++ {
		key := make([]byte, 32)
		_, _ = r.Read(key)
		value := make([]byte, 32)
		_, _ = r.Read(value)
		require.NoError(targetDB.Put(key, value))
	}
	targetRootID, err := targetDB.GetMerkleRoot(context.Background())
	require.NoError(err)

	syncClient.EXPECT().GetRangeProof(
		gomock.Any(),
		gomock.Any(),
	).DoAndReturn(func(_ context.Context, req *pb.SyncGetRangeProofRequest) (*merkledb.RangeProof, error) {
		// Don't serve sync requests until we've called Shutdown,
		// to ensure we don't finish syncing before Shutdown is called.
		<-onShutdownChan

		rootID, err := ids.ToID(req.RootHash)
		require.NoError(err)
		require.Equal(targetRootID, rootID)

		start := maybe.Nothing[[]byte]()
		if req.StartKey != nil && !req.StartKey.IsNothing {
			start = maybe.Some(req.StartKey.Value)
		}

		end := maybe.Nothing[[]byte]()
		if req.EndKey != nil && !req.EndKey.IsNothing {
			end = maybe.Some(req.EndKey.Value)
		}

		return targetDB.GetRangeProof(
			context.Background(),
			start,
			end,
			int(req.KeyLimit),
		)
	}).AnyTimes()

	client := NewClient(ClientConfig{
		ManagerConfig: xsync.ManagerConfig{
			DB:                    syncDB,
			Client:                syncClient,
			SimultaneousWorkLimit: 1,
			Log:                   logging.NoLog{},
			BranchFactor:          merkledb.BranchFactor16,
		},
		Enabled: true,
		OnDone: func(err error) {
			onDoneErrChan <- err
		},
	}, memdb.New())

	// Make a new rawSummary whose root ID is [targetRootID].
	rawSummary, err := NewSummary(ids.GenerateTestID(), 1337, targetRootID)
	require.NoError(err)
	// Need to make the summary like this so that its onAccept callback is set.
	summary, err := client.ParseStateSummary(context.Background(), rawSummary.Bytes())
	require.NoError(err)

	// Start syncing.
	syncMode, err := summary.Accept(context.Background())
	require.Equal(block.StateSyncStatic, syncMode)
	require.NoError(err)

	// Interrupt syncing with a shutdown.
	client.Shutdown()

	// Allow syncing to finish.
	close(onShutdownChan)

	// Wait for syncing to finish / assert that
	// the OnDone callback was called.
	require.ErrorIs(<-onDoneErrChan, context.Canceled)

	// Make sure the ongoing sync remains.
	ongoingSummary, err := client.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.Equal(summary.ID(), ongoingSummary.ID())
}
