// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errInvalidSummary = errors.New("invalid summary")
	errEmptySummary   = errors.New("empty summary")
	errUnknownSummary = errors.New("unknown summary")
)

func TestStateSyncerIsEnabledIfVMSupportsStateSyncing(t *testing.T) {
	require := require.New(t)

	// Build state syncer
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	sender := &enginetest.Sender{T: t}

	// Non state syncableVM case
	nonStateSyncableVM := &blocktest.VM{
		VM: enginetest.VM{T: t},
	}
	dummyGetter, err := getter.New(
		nonStateSyncableVM,
		sender,
		logging.NoLog{},
		time.Second,
		2000,
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	cfg, err := NewConfig(dummyGetter, ctx, nil, sender, nil, 0, 0, nil, nonStateSyncableVM)
	require.NoError(err)
	syncer := New(cfg, func(context.Context, uint32) error {
		return nil
	})

	enabled, err := syncer.IsEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	// State syncableVM case
	fullVM := &fullVM{
		VM: &blocktest.VM{
			VM: enginetest.VM{T: t},
		},
		StateSyncableVM: &blocktest.StateSyncableVM{
			T: t,
		},
	}
	dummyGetter, err = getter.New(
		fullVM,
		sender,
		logging.NoLog{},
		time.Second,
		2000,
		prometheus.NewRegistry())
	require.NoError(err)

	cfg, err = NewConfig(dummyGetter, ctx, nil, sender, nil, 0, 0, nil, fullVM)
	require.NoError(err)
	syncer = New(cfg, func(context.Context, uint32) error {
		return nil
	})

	// test: VM does not support state syncing
	fullVM.StateSyncEnabledF = func(context.Context) (bool, error) {
		return false, nil
	}
	enabled, err = syncer.IsEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	// test: VM does support state syncing
	fullVM.StateSyncEnabledF = func(context.Context) (bool, error) {
		return true, nil
	}
	enabled, err = syncer.IsEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)
}

func TestStateSyncingStartsOnlyIfEnoughStakeIsConnected(t *testing.T) {
	require := require.New(t)
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	alpha, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := alpha

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, _, sender := buildTestsObjects(t, ctx, startup, beacons, alpha)

	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(context.Context, set.Set[ids.NodeID], uint32) {}
	startReqID := uint32(0)

	// attempt starting bootstrapper with no stake connected. Bootstrapper should stall.
	require.False(startup.ShouldStart())
	require.NoError(syncer.Start(context.Background(), startReqID))
	require.False(syncer.started)

	// attempt starting bootstrapper with not enough stake connected. Bootstrapper should stall.
	vdr0 := ids.GenerateTestNodeID()
	require.NoError(beacons.AddStaker(ctx.SubnetID, vdr0, nil, ids.Empty, startupAlpha/2))
	require.NoError(syncer.Connected(context.Background(), vdr0, version.CurrentApp))

	require.False(startup.ShouldStart())
	require.NoError(syncer.Start(context.Background(), startReqID))
	require.False(syncer.started)

	// finally attempt starting bootstrapper with enough stake connected. Frontiers should be requested.
	vdr := ids.GenerateTestNodeID()
	require.NoError(beacons.AddStaker(ctx.SubnetID, vdr, nil, ids.Empty, startupAlpha))
	require.NoError(syncer.Connected(context.Background(), vdr, version.CurrentApp))

	require.True(startup.ShouldStart())
	require.NoError(syncer.Start(context.Background(), startReqID))
	require.True(syncer.started)
}

func TestStateSyncLocalSummaryIsIncludedAmongFrontiersIfAvailable(t *testing.T) {
	require := require.New(t)
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, _ := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// mock VM to simulate a valid summary is returned
	localSummary := &blocktest.StateSummary{
		HeightV: 2000,
		IDV:     summaryID,
		BytesV:  summaryBytes,
	}
	fullVM.CantStateSyncGetOngoingSummary = true
	fullVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return localSummary, nil
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}

	require.Equal(localSummary, syncer.locallyAvailableSummary)
	ws, ok := syncer.weightedSummaries[summaryID]
	require.True(ok)
	require.Equal(summaryBytes, ws.summary.Bytes())
	require.Zero(ws.weight)
}

func TestStateSyncNotFoundOngoingSummaryIsNotIncludedAmongFrontiers(t *testing.T) {
	require := require.New(t)
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, _ := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// mock VM to simulate a no summary returned
	fullVM.CantStateSyncGetOngoingSummary = true
	fullVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}

	require.Nil(syncer.locallyAvailableSummary)
	require.Empty(syncer.weightedSummaries)
}

func TestBeaconsAreReachedForFrontiersUponStartup(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, _, sender := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// set sender to track nodes reached out
	contactedFrontiersProviders := set.NewSet[ids.NodeID](3)
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], _ uint32) {
		contactedFrontiersProviders.Union(ss)
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}

	// check that vdrs are reached out for frontiers
	require.Len(contactedFrontiersProviders, min(beacons.NumValidators(ctx.SubnetID), maxOutstandingBroadcastRequests))
	for beaconID := range contactedFrontiersProviders {
		// check that beacon is duly marked as reached out
		require.Contains(syncer.pendingSeeders, beaconID)
	}

	// check that, obviously, no summary is yet registered
	require.Empty(syncer.weightedSummaries)
}

func TestUnRequestedStateSummaryFrontiersAreDropped(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}

	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	require.Positive(initiallyReachedOutBeaconsSize)
	require.LessOrEqual(initiallyReachedOutBeaconsSize, maxOutstandingBroadcastRequests)

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		return &blocktest.StateSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	// pick one of the vdrs that have been reached out
	responsiveBeaconID := pickRandomFrom(contactedFrontiersProviders)
	responsiveBeaconReqID := contactedFrontiersProviders[responsiveBeaconID]

	// check a response with wrong request ID is dropped
	require.NoError(syncer.StateSummaryFrontier(
		context.Background(),
		responsiveBeaconID,
		math.MaxInt32,
		summaryBytes,
	))
	require.Contains(syncer.pendingSeeders, responsiveBeaconID) // responsiveBeacon still pending
	require.Empty(syncer.weightedSummaries)

	// check a response from unsolicited node is dropped
	unsolicitedNodeID := ids.GenerateTestNodeID()
	require.NoError(syncer.StateSummaryFrontier(
		context.Background(),
		unsolicitedNodeID,
		responsiveBeaconReqID,
		summaryBytes,
	))
	require.Empty(syncer.weightedSummaries)

	// check a valid response is duly recorded
	require.NoError(syncer.StateSummaryFrontier(
		context.Background(),
		responsiveBeaconID,
		responsiveBeaconReqID,
		summaryBytes,
	))

	// responsiveBeacon not pending anymore
	require.NotContains(syncer.pendingSeeders, responsiveBeaconID)

	// valid summary is recorded
	ws, ok := syncer.weightedSummaries[summaryID]
	require.True(ok)
	require.True(bytes.Equal(ws.summary.Bytes(), summaryBytes))

	// other listed vdrs are reached for data
	require.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == beacons.NumValidators(ctx.SubnetID))
}

func TestMalformedStateSummaryFrontiersAreDropped(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}

	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	require.Positive(initiallyReachedOutBeaconsSize)
	require.LessOrEqual(initiallyReachedOutBeaconsSize, maxOutstandingBroadcastRequests)

	// mock VM to simulate an invalid summary is returned
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	isSummaryDecoded := false
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(context.Context, []byte) (block.StateSummary, error) {
		isSummaryDecoded = true
		return nil, errInvalidSummary
	}

	// pick one of the vdrs that have been reached out
	responsiveBeaconID := pickRandomFrom(contactedFrontiersProviders)
	responsiveBeaconReqID := contactedFrontiersProviders[responsiveBeaconID]

	// response is valid, but invalid summary is not recorded
	require.NoError(syncer.StateSummaryFrontier(
		context.Background(),
		responsiveBeaconID,
		responsiveBeaconReqID,
		summary,
	))

	// responsiveBeacon not pending anymore
	require.NotContains(syncer.pendingSeeders, responsiveBeaconID)

	// invalid summary is not recorded
	require.True(isSummaryDecoded)
	require.Empty(syncer.weightedSummaries)

	// even in case of invalid summaries, other listed vdrs
	// are reached for data
	require.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == beacons.NumValidators(ctx.SubnetID))
}

func TestLateResponsesFromUnresponsiveFrontiersAreNotRecorded(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}

	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	require.Positive(initiallyReachedOutBeaconsSize)
	require.LessOrEqual(initiallyReachedOutBeaconsSize, maxOutstandingBroadcastRequests)

	// pick one of the vdrs that have been reached out
	unresponsiveBeaconID := pickRandomFrom(contactedFrontiersProviders)
	unresponsiveBeaconReqID := contactedFrontiersProviders[unresponsiveBeaconID]

	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		require.Empty(summaryBytes)
		return nil, errEmptySummary
	}

	// assume timeout is reached and vdrs is marked as unresponsive
	require.NoError(syncer.GetStateSummaryFrontierFailed(
		context.Background(),
		unresponsiveBeaconID,
		unresponsiveBeaconReqID,
	))

	// unresponsiveBeacon not pending anymore
	require.NotContains(syncer.pendingSeeders, unresponsiveBeaconID)
	require.Contains(syncer.failedSeeders, unresponsiveBeaconID)

	// even in case of timeouts, other listed vdrs
	// are reached for data
	require.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == beacons.NumValidators(ctx.SubnetID))

	// mock VM to simulate a valid but late summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		return &blocktest.StateSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	// check a valid but late response is not recorded
	require.NoError(syncer.StateSummaryFrontier(
		context.Background(),
		unresponsiveBeaconID,
		unresponsiveBeaconReqID,
		summaryBytes,
	))

	// late summary is not recorded
	require.Empty(syncer.weightedSummaries)
}

func TestStateSyncIsRestartedIfTooManyFrontierSeedersTimeout(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, b []byte) (block.StateSummary, error) {
		switch {
		case bytes.Equal(b, summaryBytes):
			return &blocktest.StateSummary{
				HeightV: key,
				IDV:     summaryID,
				BytesV:  summaryBytes,
			}, nil
		case bytes.Equal(b, nil):
			return nil, errEmptySummary
		default:
			return nil, errUnknownSummary
		}
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32, _ []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}
	require.NotEmpty(syncer.pendingSeeders)

	// let just one node respond and all others timeout
	maxResponses := 1
	reachedSeedersCount := syncer.Config.SampleK
	for reachedSeedersCount >= 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		require.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		if maxResponses > 0 {
			require.NoError(syncer.StateSummaryFrontier(
				context.Background(),
				beaconID,
				reqID,
				summaryBytes,
			))
		} else {
			require.NoError(syncer.GetStateSummaryFrontierFailed(
				context.Background(),
				beaconID,
				reqID,
			))
		}
		maxResponses--
		reachedSeedersCount--
	}

	// check that some frontier seeders are reached again for the frontier
	require.NotEmpty(syncer.pendingSeeders)

	// check that no vote requests are issued
	require.Empty(contactedVoters)
}

func TestVoteRequestsAreSentAsAllFrontierBeaconsResponded(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, b []byte) (block.StateSummary, error) {
		require.Equal(summaryBytes, b)
		return &blocktest.StateSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32, _ []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}
	require.NotEmpty(syncer.pendingSeeders)

	// let all contacted vdrs respond
	for syncer.pendingSeeders.Len() != 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		require.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		require.NoError(syncer.StateSummaryFrontier(
			context.Background(),
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	require.Empty(syncer.pendingSeeders)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	require.Positive(initiallyContactedVotersSize)
	require.LessOrEqual(initiallyContactedVotersSize, maxOutstandingBroadcastRequests)
}

func TestUnRequestedVotesAreDropped(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		return &blocktest.StateSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32, _ []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}
	require.NotEmpty(syncer.pendingSeeders)

	// let all contacted vdrs respond
	for syncer.pendingSeeders.Len() != 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		require.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		require.NoError(syncer.StateSummaryFrontier(
			context.Background(),
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	require.Empty(syncer.pendingSeeders)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	require.Positive(initiallyContactedVotersSize)
	require.LessOrEqual(initiallyContactedVotersSize, maxOutstandingBroadcastRequests)

	_, found := syncer.weightedSummaries[summaryID]
	require.True(found)

	// pick one of the voters that have been reached out
	responsiveVoterID := pickRandomFrom(contactedVoters)
	responsiveVoterReqID := contactedVoters[responsiveVoterID]

	// check a response with wrong request ID is dropped
	require.NoError(syncer.AcceptedStateSummary(
		context.Background(),
		responsiveVoterID,
		math.MaxInt32,
		set.Of(summaryID),
	))

	// responsiveVoter still pending
	require.Contains(syncer.pendingVoters, responsiveVoterID)
	require.Zero(syncer.weightedSummaries[summaryID].weight)

	// check a response from unsolicited node is dropped
	unsolicitedVoterID := ids.GenerateTestNodeID()
	require.NoError(syncer.AcceptedStateSummary(
		context.Background(),
		unsolicitedVoterID,
		responsiveVoterReqID,
		set.Of(summaryID),
	))
	require.Zero(syncer.weightedSummaries[summaryID].weight)

	// check a valid response is duly recorded
	require.NoError(syncer.AcceptedStateSummary(
		context.Background(),
		responsiveVoterID,
		responsiveVoterReqID,
		set.Of(summaryID),
	))

	// responsiveBeacon not pending anymore
	require.NotContains(syncer.pendingSeeders, responsiveVoterID)
	voterWeight := beacons.GetWeight(ctx.SubnetID, responsiveVoterID)
	require.Equal(voterWeight, syncer.weightedSummaries[summaryID].weight)

	// other listed voters are reached out
	require.True(
		len(contactedVoters) > initiallyContactedVotersSize ||
			len(contactedVoters) == beacons.NumValidators(ctx.SubnetID))
}

func TestVotesForUnknownSummariesAreDropped(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		return &blocktest.StateSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32, _ []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}
	require.NotEmpty(syncer.pendingSeeders)

	// let all contacted vdrs respond
	for syncer.pendingSeeders.Len() != 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		require.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		require.NoError(syncer.StateSummaryFrontier(
			context.Background(),
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	require.Empty(syncer.pendingSeeders)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	require.Positive(initiallyContactedVotersSize)
	require.LessOrEqual(initiallyContactedVotersSize, maxOutstandingBroadcastRequests)

	_, found := syncer.weightedSummaries[summaryID]
	require.True(found)

	// pick one of the voters that have been reached out
	responsiveVoterID := pickRandomFrom(contactedVoters)
	responsiveVoterReqID := contactedVoters[responsiveVoterID]

	// check a response for unRequested summary is dropped
	require.NoError(syncer.AcceptedStateSummary(
		context.Background(),
		responsiveVoterID,
		responsiveVoterReqID,
		set.Of(unknownSummaryID),
	))
	_, found = syncer.weightedSummaries[unknownSummaryID]
	require.False(found)

	// check that responsiveVoter cannot cast another vote
	require.NotContains(syncer.pendingSeeders, responsiveVoterID)
	require.NoError(syncer.AcceptedStateSummary(
		context.Background(),
		responsiveVoterID,
		responsiveVoterReqID,
		set.Of(summaryID),
	))
	require.Zero(syncer.weightedSummaries[summaryID].weight)

	// other listed voters are reached out, even in the face of vote
	// on unknown summary
	require.True(
		len(contactedVoters) > initiallyContactedVotersSize ||
			len(contactedVoters) == beacons.NumValidators(ctx.SubnetID))
}

func TestStateSummaryIsPassedToVMAsMajorityOfVotesIsCastedForIt(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4
	alpha := (totalWeight + 1) / 2

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, alpha)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	summary := &blocktest.StateSummary{
		HeightV: key,
		IDV:     summaryID,
		BytesV:  summaryBytes,
		T:       t,
	}
	minoritySummary := &blocktest.StateSummary{
		HeightV: minorityKey,
		IDV:     minoritySummaryID,
		BytesV:  minoritySummaryBytes,
		T:       t,
	}

	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, b []byte) (block.StateSummary, error) {
		switch {
		case bytes.Equal(b, summaryBytes):
			return summary, nil
		case bytes.Equal(b, minoritySummaryBytes):
			return minoritySummary, nil
		default:
			return nil, errUnknownSummary
		}
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32, _ []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}
	require.NotEmpty(syncer.pendingSeeders)

	// let all contacted vdrs respond with majority or minority summaries
	for {
		reachedSeeders := syncer.pendingSeeders.Len()
		if reachedSeeders == 0 {
			break
		}
		beaconID, found := syncer.pendingSeeders.Peek()
		require.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		if reachedSeeders%2 == 0 {
			require.NoError(syncer.StateSummaryFrontier(
				context.Background(),
				beaconID,
				reqID,
				summaryBytes,
			))
		} else {
			require.NoError(syncer.StateSummaryFrontier(
				context.Background(),
				beaconID,
				reqID,
				minoritySummaryBytes,
			))
		}
	}
	require.Empty(syncer.pendingSeeders)

	majoritySummaryCalled := false
	minoritySummaryCalled := false
	summary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		majoritySummaryCalled = true
		return block.StateSyncStatic, nil
	}
	minoritySummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		minoritySummaryCalled = true
		return block.StateSyncStatic, nil
	}

	// let a majority of voters return summaryID, and a minority return minoritySummaryID. The rest timeout.
	cumulatedWeight := uint64(0)
	for syncer.pendingVoters.Len() != 0 {
		voterID, found := syncer.pendingVoters.Peek()
		require.True(found)
		reqID := contactedVoters[voterID]

		switch {
		case cumulatedWeight < alpha/2:
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				set.Of(summaryID, minoritySummaryID),
			))
			cumulatedWeight += beacons.GetWeight(ctx.SubnetID, voterID)

		case cumulatedWeight < alpha:
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				set.Of(summaryID),
			))
			cumulatedWeight += beacons.GetWeight(ctx.SubnetID, voterID)

		default:
			require.NoError(syncer.GetAcceptedStateSummaryFailed(
				context.Background(),
				voterID,
				reqID,
			))
		}
	}

	// check that finally summary is passed to VM
	require.True(majoritySummaryCalled)
	require.False(minoritySummaryCalled)
}

func TestVotingIsRestartedIfMajorityIsNotReachedDueToTimeouts(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4
	alpha := (totalWeight + 1) / 2

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, alpha)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	minoritySummary := &blocktest.StateSummary{
		HeightV: minorityKey,
		IDV:     minoritySummaryID,
		BytesV:  minoritySummaryBytes,
		T:       t,
	}
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(context.Context, []byte) (block.StateSummary, error) {
		return minoritySummary, nil
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32, _ []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}
	require.NotEmpty(syncer.pendingSeeders)

	// let all contacted vdrs respond
	for syncer.pendingSeeders.Len() != 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		require.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		require.NoError(syncer.StateSummaryFrontier(
			context.Background(),
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	require.Empty(syncer.pendingSeeders)

	minoritySummaryCalled := false
	minoritySummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		minoritySummaryCalled = true
		return block.StateSyncStatic, nil
	}

	// Let a majority of voters timeout.
	timedOutWeight := uint64(0)
	for syncer.pendingVoters.Len() != 0 {
		voterID, found := syncer.pendingVoters.Peek()
		require.True(found)
		reqID := contactedVoters[voterID]

		// vdr carries the largest weight by far. Make sure it fails
		if timedOutWeight <= alpha {
			require.NoError(syncer.GetAcceptedStateSummaryFailed(
				context.Background(),
				voterID,
				reqID,
			))
			timedOutWeight += beacons.GetWeight(ctx.SubnetID, voterID)
		} else {
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				set.Of(summaryID),
			))
		}
	}

	// No state summary is passed to VM
	require.False(minoritySummaryCalled)

	// instead the whole process is restared
	require.Empty(syncer.pendingVoters)     // no voters reached
	require.NotEmpty(syncer.pendingSeeders) // frontiers providers reached again
}

func TestStateSyncIsStoppedIfEnoughVotesAreCastedWithNoClearMajority(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4
	alpha := (totalWeight + 1) / 2

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, fullVM, sender := buildTestsObjects(t, ctx, startup, beacons, alpha)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid minoritySummary1 is returned
	minoritySummary1 := &blocktest.StateSummary{
		HeightV: key,
		IDV:     summaryID,
		BytesV:  summaryBytes,
		T:       t,
	}
	minoritySummary2 := &blocktest.StateSummary{
		HeightV: minorityKey,
		IDV:     minoritySummaryID,
		BytesV:  minoritySummaryBytes,
		T:       t,
	}

	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, b []byte) (block.StateSummary, error) {
		switch {
		case bytes.Equal(b, summaryBytes):
			return minoritySummary1, nil
		case bytes.Equal(b, minoritySummaryBytes):
			return minoritySummary2, nil
		default:
			return nil, errUnknownSummary
		}
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32, _ []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, nodeID := range beacons.GetValidatorIDs(ctx.SubnetID) {
		require.NoError(syncer.Connected(context.Background(), nodeID, version.CurrentApp))
	}
	require.NotEmpty(syncer.pendingSeeders)

	// let all contacted vdrs respond with majority or minority summaries
	for {
		reachedSeeders := syncer.pendingSeeders.Len()
		if reachedSeeders == 0 {
			break
		}
		beaconID, found := syncer.pendingSeeders.Peek()
		require.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		if reachedSeeders%2 == 0 {
			require.NoError(syncer.StateSummaryFrontier(
				context.Background(),
				beaconID,
				reqID,
				summaryBytes,
			))
		} else {
			require.NoError(syncer.StateSummaryFrontier(
				context.Background(),
				beaconID,
				reqID,
				minoritySummaryBytes,
			))
		}
	}
	require.Empty(syncer.pendingSeeders)

	majoritySummaryCalled := false
	minoritySummaryCalled := false
	minoritySummary1.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		majoritySummaryCalled = true
		return block.StateSyncStatic, nil
	}
	minoritySummary2.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		minoritySummaryCalled = true
		return block.StateSyncStatic, nil
	}

	stateSyncFullyDone := false
	syncer.onDoneStateSyncing = func(context.Context, uint32) error {
		stateSyncFullyDone = true
		return nil
	}

	// let all votes respond in time without any summary reaching a majority.
	// We achieve it by making most nodes voting for an invalid summaryID.
	votingWeightStake := uint64(0)
	for syncer.pendingVoters.Len() != 0 {
		voterID, found := syncer.pendingVoters.Peek()
		require.True(found)
		reqID := contactedVoters[voterID]

		switch {
		case votingWeightStake < alpha/2:
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				set.Of(minoritySummary1.ID(), minoritySummary2.ID()),
			))
			votingWeightStake += beacons.GetWeight(ctx.SubnetID, voterID)

		default:
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				set.Of(ids.ID{'u', 'n', 'k', 'n', 'o', 'w', 'n', 'I', 'D'}),
			))
			votingWeightStake += beacons.GetWeight(ctx.SubnetID, voterID)
		}
	}

	// check that finally summary is passed to VM
	require.False(majoritySummaryCalled)
	require.False(minoritySummaryCalled)
	require.True(stateSyncFullyDone) // no restart, just move to boostrapping
}

func TestStateSyncIsDoneOnceVMNotifies(t *testing.T) {
	require := require.New(t)

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	beacons := buildTestPeers(t, ctx.SubnetID)
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startupAlpha := (3*totalWeight + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	syncer, _, _ := buildTestsObjects(t, ctx, startup, beacons, (totalWeight+1)/2)

	stateSyncFullyDone := false
	syncer.onDoneStateSyncing = func(context.Context, uint32) error {
		stateSyncFullyDone = true
		return nil
	}

	// Any Put response before StateSyncDone is received from VM is dropped
	require.NoError(syncer.Notify(context.Background(), common.StateSyncDone))
	require.True(stateSyncFullyDone)
}
