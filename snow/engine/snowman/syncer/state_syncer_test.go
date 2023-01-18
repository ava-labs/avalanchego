// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	safeMath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	errInvalidSummary = errors.New("invalid summary")
	errEmptySummary   = errors.New("empty summary")
	errUnknownSummary = errors.New("unknown summary")
)

func TestStateSyncerIsEnabledIfVMSupportsStateSyncing(t *testing.T) {
	require := require.New(t)

	// Build state syncer
	sender := &common.SenderTest{T: t}
	commonCfg := &common.Config{
		Ctx:    snow.DefaultConsensusContextTest(),
		Sender: sender,
	}

	// Non state syncableVM case
	nonStateSyncableVM := &block.TestVM{
		TestVM: common.TestVM{T: t},
	}
	dummyGetter, err := getter.New(nonStateSyncableVM, *commonCfg)
	require.NoError(err)

	cfg, err := NewConfig(*commonCfg, nil, dummyGetter, nonStateSyncableVM)
	require.NoError(err)
	syncer := New(cfg, func(context.Context, uint32) error {
		return nil
	})

	enabled, err := syncer.IsEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	// State syncableVM case
	commonCfg.Ctx = snow.DefaultConsensusContextTest() // reset metrics

	fullVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{T: t},
		},
		TestStateSyncableVM: &block.TestStateSyncableVM{
			T: t,
		},
	}
	dummyGetter, err = getter.New(fullVM, *commonCfg)
	require.NoError(err)

	cfg, err = NewConfig(*commonCfg, nil, dummyGetter, fullVM)
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

	vdrs := buildTestPeers(t)
	alpha := vdrs.Weight()
	startupAlpha := alpha

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Validators:     vdrs,
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          alpha,
		StartupTracker: startup,
	}
	syncer, _, sender := buildTestsObjects(t, &commonCfg)

	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(context.Context, set.Set[ids.NodeID], uint32) {}
	startReqID := uint32(0)

	// attempt starting bootstrapper with no stake connected. Bootstrapper should stall.
	require.False(commonCfg.StartupTracker.ShouldStart())
	require.NoError(syncer.Start(context.Background(), startReqID))
	require.False(syncer.started)

	// attempt starting bootstrapper with not enough stake connected. Bootstrapper should stall.
	vdr0 := ids.GenerateTestNodeID()
	require.NoError(vdrs.Add(vdr0, nil, ids.Empty, startupAlpha/2))
	require.NoError(syncer.Connected(context.Background(), vdr0, version.CurrentApp))

	require.False(commonCfg.StartupTracker.ShouldStart())
	require.NoError(syncer.Start(context.Background(), startReqID))
	require.False(syncer.started)

	// finally attempt starting bootstrapper with enough stake connected. Frontiers should be requested.
	vdr := ids.GenerateTestNodeID()
	require.NoError(vdrs.Add(vdr, nil, ids.Empty, startupAlpha))
	require.NoError(syncer.Connected(context.Background(), vdr, version.CurrentApp))

	require.True(commonCfg.StartupTracker.ShouldStart())
	require.NoError(syncer.Start(context.Background(), startReqID))
	require.True(syncer.started)
}

func TestStateSyncLocalSummaryIsIncludedAmongFrontiersIfAvailable(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, _ := buildTestsObjects(t, &commonCfg)

	// mock VM to simulate a valid summary is returned
	localSummary := &block.TestStateSummary{
		HeightV: 2000,
		IDV:     summaryID,
		BytesV:  summaryBytes,
	}
	fullVM.CantStateSyncGetOngoingSummary = true
	fullVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return localSummary, nil
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}

	require.True(syncer.locallyAvailableSummary == localSummary)
	ws, ok := syncer.weightedSummaries[summaryID]
	require.True(ok)
	require.True(bytes.Equal(ws.summary.Bytes(), summaryBytes))
	require.Zero(ws.weight)
}

func TestStateSyncNotFoundOngoingSummaryIsNotIncludedAmongFrontiers(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, _ := buildTestsObjects(t, &commonCfg)

	// mock VM to simulate a no summary returned
	fullVM.CantStateSyncGetOngoingSummary = true
	fullVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}

	require.Nil(syncer.locallyAvailableSummary)
	require.Empty(syncer.weightedSummaries)
}

func TestBeaconsAreReachedForFrontiersUponStartup(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, _, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := set.NewSet[ids.NodeID](3)
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], _ uint32) {
		contactedFrontiersProviders.Union(ss)
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}

	// check that vdrs are reached out for frontiers
	require.True(len(contactedFrontiersProviders) == safeMath.Min(vdrs.Len(), common.MaxOutstandingBroadcastRequests))
	for beaconID := range contactedFrontiersProviders {
		// check that beacon is duly marked as reached out
		require.True(syncer.pendingSeeders.Contains(beaconID))
	}

	// check that, obviously, no summary is yet registered
	require.True(len(syncer.weightedSummaries) == 0)
}

func TestUnRequestedStateSummaryFrontiersAreDropped(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}

	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	require.True(initiallyReachedOutBeaconsSize > 0)
	require.True(initiallyReachedOutBeaconsSize <= common.MaxOutstandingBroadcastRequests)

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		return &block.TestStateSummary{
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
	require.True(syncer.pendingSeeders.Contains(responsiveBeaconID)) // responsiveBeacon still pending
	require.True(len(syncer.weightedSummaries) == 0)

	// check a response from unsolicited node is dropped
	unsolicitedNodeID := ids.GenerateTestNodeID()
	require.NoError(syncer.StateSummaryFrontier(
		context.Background(),
		unsolicitedNodeID,
		responsiveBeaconReqID,
		summaryBytes,
	))
	require.True(len(syncer.weightedSummaries) == 0)

	// check a valid response is duly recorded
	require.NoError(syncer.StateSummaryFrontier(
		context.Background(),
		responsiveBeaconID,
		responsiveBeaconReqID,
		summaryBytes,
	))

	// responsiveBeacon not pending anymore
	require.False(syncer.pendingSeeders.Contains(responsiveBeaconID))

	// valid summary is recorded
	ws, ok := syncer.weightedSummaries[summaryID]
	require.True(ok)
	require.True(bytes.Equal(ws.summary.Bytes(), summaryBytes))

	// other listed vdrs are reached for data
	require.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == vdrs.Len())
}

func TestMalformedStateSummaryFrontiersAreDropped(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}

	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	require.True(initiallyReachedOutBeaconsSize > 0)
	require.True(initiallyReachedOutBeaconsSize <= common.MaxOutstandingBroadcastRequests)

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
	require.False(syncer.pendingSeeders.Contains(responsiveBeaconID))

	// invalid summary is not recorded
	require.True(isSummaryDecoded)
	require.True(len(syncer.weightedSummaries) == 0)

	// even in case of invalid summaries, other listed vdrs
	// are reached for data
	require.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == vdrs.Len())
}

func TestLateResponsesFromUnresponsiveFrontiersAreNotRecorded(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}

	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	require.True(initiallyReachedOutBeaconsSize > 0)
	require.True(initiallyReachedOutBeaconsSize <= common.MaxOutstandingBroadcastRequests)

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
	require.False(syncer.pendingSeeders.Contains(unresponsiveBeaconID))
	require.True(syncer.failedSeeders.Contains(unresponsiveBeaconID))

	// even in case of timeouts, other listed vdrs
	// are reached for data
	require.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == vdrs.Len())

	// mock VM to simulate a valid but late summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		return &block.TestStateSummary{
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
	require.True(len(syncer.weightedSummaries) == 0)
}

func TestStateSyncIsRestartedIfTooManyFrontierSeedersTimeout(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:                         snow.DefaultConsensusContextTest(),
		Beacons:                     vdrs,
		SampleK:                     vdrs.Len(),
		Alpha:                       (vdrs.Weight() + 1) / 2,
		StartupTracker:              startup,
		RetryBootstrap:              true,
		RetryBootstrapWarnFrequency: 1,
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

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
			return &block.TestStateSummary{
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
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}
	require.True(syncer.pendingSeeders.Len() != 0)

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
	require.True(syncer.pendingSeeders.Len() > 0)

	// check that no vote requests are issued
	require.True(len(contactedVoters) == 0)
}

func TestVoteRequestsAreSentAsAllFrontierBeaconsResponded(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

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
		require.True(bytes.Equal(b, summaryBytes))
		return &block.TestStateSummary{
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
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}
	require.True(syncer.pendingSeeders.Len() != 0)

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
	require.False(syncer.pendingSeeders.Len() != 0)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	require.True(initiallyContactedVotersSize > 0)
	require.True(initiallyContactedVotersSize <= common.MaxOutstandingBroadcastRequests)
}

func TestUnRequestedVotesAreDropped(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

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
		return &block.TestStateSummary{
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
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}
	require.True(syncer.pendingSeeders.Len() != 0)

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
	require.False(syncer.pendingSeeders.Len() != 0)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	require.True(initiallyContactedVotersSize > 0)
	require.True(initiallyContactedVotersSize <= common.MaxOutstandingBroadcastRequests)

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
		[]ids.ID{summaryID},
	))

	// responsiveVoter still pending
	require.True(syncer.pendingVoters.Contains(responsiveVoterID))
	require.True(syncer.weightedSummaries[summaryID].weight == 0)

	// check a response from unsolicited node is dropped
	unsolicitedVoterID := ids.GenerateTestNodeID()
	require.NoError(syncer.AcceptedStateSummary(
		context.Background(),
		unsolicitedVoterID,
		responsiveVoterReqID,
		[]ids.ID{summaryID},
	))
	require.True(syncer.weightedSummaries[summaryID].weight == 0)

	// check a valid response is duly recorded
	require.NoError(syncer.AcceptedStateSummary(
		context.Background(),
		responsiveVoterID,
		responsiveVoterReqID,
		[]ids.ID{summaryID},
	))

	// responsiveBeacon not pending anymore
	require.False(syncer.pendingSeeders.Contains(responsiveVoterID))
	voterWeight := vdrs.GetWeight(responsiveVoterID)
	require.Equal(voterWeight, syncer.weightedSummaries[summaryID].weight)

	// other listed voters are reached out
	require.True(
		len(contactedVoters) > initiallyContactedVotersSize ||
			len(contactedVoters) == vdrs.Len())
}

func TestVotesForUnknownSummariesAreDropped(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

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
		return &block.TestStateSummary{
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
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}
	require.True(syncer.pendingSeeders.Len() != 0)

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
	require.False(syncer.pendingSeeders.Len() != 0)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	require.True(initiallyContactedVotersSize > 0)
	require.True(initiallyContactedVotersSize <= common.MaxOutstandingBroadcastRequests)

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
		[]ids.ID{unknownSummaryID},
	))
	_, found = syncer.weightedSummaries[unknownSummaryID]
	require.False(found)

	// check that responsiveVoter cannot cast another vote
	require.False(syncer.pendingSeeders.Contains(responsiveVoterID))
	require.NoError(syncer.AcceptedStateSummary(
		context.Background(),
		responsiveVoterID,
		responsiveVoterReqID,
		[]ids.ID{summaryID},
	))
	require.True(syncer.weightedSummaries[summaryID].weight == 0)

	// other listed voters are reached out, even in the face of vote
	// on unknown summary
	require.True(
		len(contactedVoters) > initiallyContactedVotersSize ||
			len(contactedVoters) == vdrs.Len())
}

func TestStateSummaryIsPassedToVMAsMajorityOfVotesIsCastedForIt(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	summary := &block.TestStateSummary{
		HeightV: key,
		IDV:     summaryID,
		BytesV:  summaryBytes,
		T:       t,
	}
	minoritySummary := &block.TestStateSummary{
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
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}
	require.True(syncer.pendingSeeders.Len() != 0)

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
	require.False(syncer.pendingSeeders.Len() != 0)

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
		case cumulatedWeight < commonCfg.Alpha/2:
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				[]ids.ID{summaryID, minoritySummaryID},
			))
			cumulatedWeight += vdrs.GetWeight(voterID)

		case cumulatedWeight < commonCfg.Alpha:
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				[]ids.ID{summaryID},
			))
			cumulatedWeight += vdrs.GetWeight(voterID)

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

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:                         snow.DefaultConsensusContextTest(),
		Beacons:                     vdrs,
		SampleK:                     vdrs.Len(),
		Alpha:                       (vdrs.Weight() + 1) / 2,
		StartupTracker:              startup,
		RetryBootstrap:              true, // this sets RetryStateSyncing too
		RetryBootstrapWarnFrequency: 1,    // this sets RetrySyncingWarnFrequency too
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	minoritySummary := &block.TestStateSummary{
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
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}
	require.True(syncer.pendingSeeders.Len() != 0)

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
	require.False(syncer.pendingSeeders.Len() != 0)

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
		if timedOutWeight <= commonCfg.Alpha {
			require.NoError(syncer.GetAcceptedStateSummaryFailed(
				context.Background(),
				voterID,
				reqID,
			))
			timedOutWeight += vdrs.GetWeight(voterID)
		} else {
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				[]ids.ID{summaryID},
			))
		}
	}

	// No state summary is passed to VM
	require.False(minoritySummaryCalled)

	// instead the whole process is restared
	require.False(syncer.pendingVoters.Len() != 0) // no voters reached
	require.True(syncer.pendingSeeders.Len() != 0) // frontiers providers reached again
}

func TestStateSyncIsStoppedIfEnoughVotesAreCastedWithNoClearMajority(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:            snow.DefaultConsensusContextTest(),
		Beacons:        vdrs,
		SampleK:        vdrs.Len(),
		Alpha:          (vdrs.Weight() + 1) / 2,
		StartupTracker: startup,
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(_ context.Context, ss set.Set[ids.NodeID], reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid minoritySummary1 is returned
	minoritySummary1 := &block.TestStateSummary{
		HeightV: key,
		IDV:     summaryID,
		BytesV:  summaryBytes,
		T:       t,
	}
	minoritySummary2 := &block.TestStateSummary{
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
	for _, vdr := range vdrs.List() {
		require.NoError(syncer.Connected(context.Background(), vdr.NodeID, version.CurrentApp))
	}
	require.True(syncer.pendingSeeders.Len() != 0)

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
	require.False(syncer.pendingSeeders.Len() != 0)

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
		case votingWeightStake < commonCfg.Alpha/2:
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				[]ids.ID{minoritySummary1.ID(), minoritySummary2.ID()},
			))
			votingWeightStake += vdrs.GetWeight(voterID)

		default:
			require.NoError(syncer.AcceptedStateSummary(
				context.Background(),
				voterID,
				reqID,
				[]ids.ID{{'u', 'n', 'k', 'n', 'o', 'w', 'n', 'I', 'D'}},
			))
			votingWeightStake += vdrs.GetWeight(voterID)
		}
	}

	// check that finally summary is passed to VM
	require.False(majoritySummaryCalled)
	require.False(minoritySummaryCalled)
	require.True(stateSyncFullyDone) // no restart, just move to boostrapping
}

func TestStateSyncIsDoneOnceVMNotifies(t *testing.T) {
	require := require.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, startupAlpha)
	vdrs.RegisterCallbackListener(startup)

	commonCfg := common.Config{
		Ctx:                         snow.DefaultConsensusContextTest(),
		Beacons:                     vdrs,
		SampleK:                     vdrs.Len(),
		Alpha:                       (vdrs.Weight() + 1) / 2,
		StartupTracker:              startup,
		RetryBootstrap:              true, // this sets RetryStateSyncing too
		RetryBootstrapWarnFrequency: 1,    // this sets RetrySyncingWarnFrequency too
	}
	syncer, fullVM, _ := buildTestsObjects(t, &commonCfg)
	_ = fullVM

	stateSyncFullyDone := false
	syncer.onDoneStateSyncing = func(context.Context, uint32) error {
		stateSyncFullyDone = true
		return nil
	}

	// Any Put response before StateSyncDone is received from VM is dropped
	require.NoError(syncer.Notify(context.Background(), common.StateSyncDone))
	require.True(stateSyncFullyDone)
}
