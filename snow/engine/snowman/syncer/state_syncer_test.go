// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/version"

	safeMath "github.com/ava-labs/avalanchego/utils/math"
)

func TestStateSyncerIsEnabledIfVMSupportsStateSyncing(t *testing.T) {
	assert := assert.New(t)

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
	assert.NoError(err)

	cfg, err := NewConfig(*commonCfg, nil, dummyGetter, nonStateSyncableVM)
	assert.NoError(err)
	syncer := New(cfg, func(lastReqID uint32) error { return nil })

	enabled, err := syncer.IsEnabled()
	assert.NoError(err)
	assert.False(enabled)

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
	assert.NoError(err)

	cfg, err = NewConfig(*commonCfg, nil, dummyGetter, fullVM)
	assert.NoError(err)
	syncer = New(cfg, func(lastReqID uint32) error { return nil })

	// test: VM does not support state syncing
	fullVM.StateSyncEnabledF = func() (bool, error) { return false, nil }
	enabled, err = syncer.IsEnabled()
	assert.NoError(err)
	assert.False(enabled)

	// test: VM does support state syncing
	fullVM.StateSyncEnabledF = func() (bool, error) { return true, nil }
	enabled, err = syncer.IsEnabled()
	assert.NoError(err)
	assert.True(enabled)
}

func TestStateSyncingStartsOnlyIfEnoughStakeIsConnected(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, u uint32) {}
	startReqID := uint32(0)

	// attempt starting bootstrapper with no stake connected. Bootstrapper should stall.
	assert.False(commonCfg.StartupTracker.ShouldStart())
	assert.NoError(syncer.Start(startReqID))
	assert.False(syncer.started)

	// attempt starting bootstrapper with not enough stake connected. Bootstrapper should stall.
	vdr0 := ids.GenerateTestNodeID()
	assert.NoError(vdrs.AddWeight(vdr0, startupAlpha/2))
	assert.NoError(syncer.Connected(vdr0, version.CurrentApp))

	assert.False(commonCfg.StartupTracker.ShouldStart())
	assert.NoError(syncer.Start(startReqID))
	assert.False(syncer.started)

	// finally attempt starting bootstrapper with enough stake connected. Frontiers should be requested.
	vdr := ids.GenerateTestNodeID()
	assert.NoError(vdrs.AddWeight(vdr, startupAlpha))
	assert.NoError(syncer.Connected(vdr, version.CurrentApp))

	assert.True(commonCfg.StartupTracker.ShouldStart())
	assert.NoError(syncer.Start(startReqID))
	assert.True(syncer.started)
}

func TestStateSyncLocalSummaryIsIncludedAmongFrontiersIfAvailable(t *testing.T) {
	assert := assert.New(t)

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
	fullVM.GetOngoingSyncStateSummaryF = func() (block.StateSummary, error) {
		return localSummary, nil
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}

	assert.True(syncer.locallyAvailableSummary == localSummary)
	ws, ok := syncer.weightedSummaries[summaryID]
	assert.True(ok)
	assert.True(bytes.Equal(ws.summary.Bytes(), summaryBytes))
	assert.Zero(ws.weight)
}

func TestStateSyncNotFoundOngoingSummaryIsNotIncludedAmongFrontiers(t *testing.T) {
	assert := assert.New(t)

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
	fullVM.GetOngoingSyncStateSummaryF = func() (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}

	assert.Nil(syncer.locallyAvailableSummary)
	assert.Empty(syncer.weightedSummaries)
}

func TestBeaconsAreReachedForFrontiersUponStartup(t *testing.T) {
	assert := assert.New(t)

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
	contactedFrontiersProviders := ids.NewNodeIDSet(3)
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, u uint32) {
		contactedFrontiersProviders.Union(ss)
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}

	// check that vdrs are reached out for frontiers
	assert.True(len(contactedFrontiersProviders) == safeMath.Min(vdrs.Len(), common.MaxOutstandingBroadcastRequests))
	for beaconID := range contactedFrontiersProviders {
		// check that beacon is duly marked as reached out
		assert.True(syncer.pendingSeeders.Contains(beaconID))
	}

	// check that, obviously, no summary is yet registered
	assert.True(len(syncer.weightedSummaries) == 0)
}

func TestUnRequestedStateSummaryFrontiersAreDropped(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}

	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	assert.True(initiallyReachedOutBeaconsSize > 0)
	assert.True(initiallyReachedOutBeaconsSize <= common.MaxOutstandingBroadcastRequests)

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (block.StateSummary, error) {
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
	assert.NoError(syncer.StateSummaryFrontier(
		responsiveBeaconID,
		math.MaxInt32,
		summaryBytes,
	))
	assert.True(syncer.pendingSeeders.Contains(responsiveBeaconID)) // responsiveBeacon still pending
	assert.True(len(syncer.weightedSummaries) == 0)

	// check a response from unsolicited node is dropped
	unsolicitedNodeID := ids.GenerateTestNodeID()
	assert.NoError(syncer.StateSummaryFrontier(
		unsolicitedNodeID,
		responsiveBeaconReqID,
		summaryBytes,
	))
	assert.True(len(syncer.weightedSummaries) == 0)

	// check a valid response is duly recorded
	assert.NoError(syncer.StateSummaryFrontier(
		responsiveBeaconID,
		responsiveBeaconReqID,
		summaryBytes,
	))

	// responsiveBeacon not pending anymore
	assert.False(syncer.pendingSeeders.Contains(responsiveBeaconID))

	// valid summary is recorded
	ws, ok := syncer.weightedSummaries[summaryID]
	assert.True(ok)
	assert.True(bytes.Equal(ws.summary.Bytes(), summaryBytes))

	// other listed vdrs are reached for data
	assert.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == vdrs.Len())
}

func TestMalformedStateSummaryFrontiersAreDropped(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}

	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	assert.True(initiallyReachedOutBeaconsSize > 0)
	assert.True(initiallyReachedOutBeaconsSize <= common.MaxOutstandingBroadcastRequests)

	// mock VM to simulate an invalid summary is returned
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	isSummaryDecoded := false
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (block.StateSummary, error) {
		isSummaryDecoded = true
		return nil, errors.New("invalid state summary")
	}

	// pick one of the vdrs that have been reached out
	responsiveBeaconID := pickRandomFrom(contactedFrontiersProviders)
	responsiveBeaconReqID := contactedFrontiersProviders[responsiveBeaconID]

	// response is valid, but invalid summary is not recorded
	assert.NoError(syncer.StateSummaryFrontier(
		responsiveBeaconID,
		responsiveBeaconReqID,
		summary,
	))

	// responsiveBeacon not pending anymore
	assert.False(syncer.pendingSeeders.Contains(responsiveBeaconID))

	// invalid summary is not recorded
	assert.True(isSummaryDecoded)
	assert.True(len(syncer.weightedSummaries) == 0)

	// even in case of invalid summaries, other listed vdrs
	// are reached for data
	assert.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == vdrs.Len())
}

func TestLateResponsesFromUnresponsiveFrontiersAreNotRecorded(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}

	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	assert.True(initiallyReachedOutBeaconsSize > 0)
	assert.True(initiallyReachedOutBeaconsSize <= common.MaxOutstandingBroadcastRequests)

	// pick one of the vdrs that have been reached out
	unresponsiveBeaconID := pickRandomFrom(contactedFrontiersProviders)
	unresponsiveBeaconReqID := contactedFrontiersProviders[unresponsiveBeaconID]

	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (block.StateSummary, error) {
		assert.True(len(summaryBytes) == 0)
		return nil, errors.New("empty summary")
	}

	// assume timeout is reached and vdrs is marked as unresponsive
	assert.NoError(syncer.GetStateSummaryFrontierFailed(
		unresponsiveBeaconID,
		unresponsiveBeaconReqID,
	))

	// unresponsiveBeacon not pending anymore
	assert.False(syncer.pendingSeeders.Contains(unresponsiveBeaconID))
	assert.True(syncer.failedSeeders.Contains(unresponsiveBeaconID))

	// even in case of timeouts, other listed vdrs
	// are reached for data
	assert.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == vdrs.Len())

	// mock VM to simulate a valid but late summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (block.StateSummary, error) {
		return &block.TestStateSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	// check a valid but late response is not recorded
	assert.NoError(syncer.StateSummaryFrontier(
		unresponsiveBeaconID,
		unresponsiveBeaconReqID,
		summaryBytes,
	))

	// late summary is not recorded
	assert.True(len(syncer.weightedSummaries) == 0)
}

func TestStateSyncIsRestartedIfTooManyFrontierSeedersTimeout(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(b []byte) (block.StateSummary, error) {
		switch {
		case bytes.Equal(b, summaryBytes):
			return &block.TestStateSummary{
				HeightV: key,
				IDV:     summaryID,
				BytesV:  summaryBytes,
			}, nil
		case bytes.Equal(b, nil):
			return nil, errors.New("Empty Summary")
		default:
			return nil, errors.New("unexpected Summary")
		}
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.NodeIDSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.pendingSeeders.Len() != 0)

	// let just one node respond and all others timeout
	maxResponses := 1
	reachedSeedersCount := syncer.Config.SampleK
	for reachedSeedersCount >= 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		if maxResponses > 0 {
			assert.NoError(syncer.StateSummaryFrontier(
				beaconID,
				reqID,
				summaryBytes,
			))
		} else {
			assert.NoError(syncer.GetStateSummaryFrontierFailed(
				beaconID,
				reqID,
			))
		}
		maxResponses--
		reachedSeedersCount--
	}

	// check that some frontier seeders are reached again for the frontier
	assert.True(syncer.pendingSeeders.Len() > 0)

	// check that no vote requests are issued
	assert.True(len(contactedVoters) == 0)
}

func TestVoteRequestsAreSentAsAllFrontierBeaconsResponded(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(b []byte) (block.StateSummary, error) {
		assert.True(bytes.Equal(b, summaryBytes))
		return &block.TestStateSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.NodeIDSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.pendingSeeders.Len() != 0)

	// let all contacted vdrs respond
	for syncer.pendingSeeders.Len() != 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		assert.NoError(syncer.StateSummaryFrontier(
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	assert.False(syncer.pendingSeeders.Len() != 0)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	assert.True(initiallyContactedVotersSize > 0)
	assert.True(initiallyContactedVotersSize <= common.MaxOutstandingBroadcastRequests)
}

func TestUnRequestedVotesAreDropped(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (block.StateSummary, error) {
		return &block.TestStateSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.NodeIDSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.pendingSeeders.Len() != 0)

	// let all contacted vdrs respond
	for syncer.pendingSeeders.Len() != 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		assert.NoError(syncer.StateSummaryFrontier(
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	assert.False(syncer.pendingSeeders.Len() != 0)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	assert.True(initiallyContactedVotersSize > 0)
	assert.True(initiallyContactedVotersSize <= common.MaxOutstandingBroadcastRequests)

	_, found := syncer.weightedSummaries[summaryID]
	assert.True(found)

	// pick one of the voters that have been reached out
	responsiveVoterID := pickRandomFrom(contactedVoters)
	responsiveVoterReqID := contactedVoters[responsiveVoterID]

	// check a response with wrong request ID is dropped
	assert.NoError(syncer.AcceptedStateSummary(
		responsiveVoterID,
		math.MaxInt32,
		[]ids.ID{summaryID},
	))

	// responsiveVoter still pending
	assert.True(syncer.pendingVoters.Contains(responsiveVoterID))
	assert.True(syncer.weightedSummaries[summaryID].weight == 0)

	// check a response from unsolicited node is dropped
	unsolicitedVoterID := ids.GenerateTestNodeID()
	assert.NoError(syncer.AcceptedStateSummary(
		unsolicitedVoterID,
		responsiveVoterReqID,
		[]ids.ID{summaryID},
	))
	assert.True(syncer.weightedSummaries[summaryID].weight == 0)

	// check a valid response is duly recorded
	assert.NoError(syncer.AcceptedStateSummary(
		responsiveVoterID,
		responsiveVoterReqID,
		[]ids.ID{summaryID},
	))

	// responsiveBeacon not pending anymore
	assert.False(syncer.pendingSeeders.Contains(responsiveVoterID))
	voterWeight, found := vdrs.GetWeight(responsiveVoterID)
	assert.True(found)
	assert.True(syncer.weightedSummaries[summaryID].weight == voterWeight)

	// other listed voters are reached out
	assert.True(
		len(contactedVoters) > initiallyContactedVotersSize ||
			len(contactedVoters) == vdrs.Len())
}

func TestVotesForUnknownSummariesAreDropped(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (block.StateSummary, error) {
		return &block.TestStateSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.NodeIDSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.pendingSeeders.Len() != 0)

	// let all contacted vdrs respond
	for syncer.pendingSeeders.Len() != 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		assert.NoError(syncer.StateSummaryFrontier(
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	assert.False(syncer.pendingSeeders.Len() != 0)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	assert.True(initiallyContactedVotersSize > 0)
	assert.True(initiallyContactedVotersSize <= common.MaxOutstandingBroadcastRequests)

	_, found := syncer.weightedSummaries[summaryID]
	assert.True(found)

	// pick one of the voters that have been reached out
	responsiveVoterID := pickRandomFrom(contactedVoters)
	responsiveVoterReqID := contactedVoters[responsiveVoterID]

	// check a response for unRequested summary is dropped
	assert.NoError(syncer.AcceptedStateSummary(
		responsiveVoterID,
		responsiveVoterReqID,
		[]ids.ID{unknownSummaryID},
	))
	_, found = syncer.weightedSummaries[unknownSummaryID]
	assert.False(found)

	// check that responsiveVoter cannot cast another vote
	assert.False(syncer.pendingSeeders.Contains(responsiveVoterID))
	assert.NoError(syncer.AcceptedStateSummary(
		responsiveVoterID,
		responsiveVoterReqID,
		[]ids.ID{summaryID},
	))
	assert.True(syncer.weightedSummaries[summaryID].weight == 0)

	// other listed voters are reached out, even in the face of vote
	// on unknown summary
	assert.True(
		len(contactedVoters) > initiallyContactedVotersSize ||
			len(contactedVoters) == vdrs.Len())
}

func TestStateSummaryIsPassedToVMAsMajorityOfVotesIsCastedForIt(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
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
	fullVM.ParseStateSummaryF = func(b []byte) (block.StateSummary, error) {
		switch {
		case bytes.Equal(b, summaryBytes):
			return summary, nil
		case bytes.Equal(b, minoritySummaryBytes):
			return minoritySummary, nil
		default:
			return nil, errors.New("unknown state summary")
		}
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.NodeIDSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.pendingSeeders.Len() != 0)

	// let all contacted vdrs respond with majority or minority summaries
	for {
		reachedSeeders := syncer.pendingSeeders.Len()
		if reachedSeeders == 0 {
			break
		}
		beaconID, found := syncer.pendingSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		if reachedSeeders%2 == 0 {
			assert.NoError(syncer.StateSummaryFrontier(
				beaconID,
				reqID,
				summaryBytes,
			))
		} else {
			assert.NoError(syncer.StateSummaryFrontier(
				beaconID,
				reqID,
				minoritySummaryBytes,
			))
		}
	}
	assert.False(syncer.pendingSeeders.Len() != 0)

	majoritySummaryCalled := false
	minoritySummaryCalled := false
	summary.AcceptF = func() (bool, error) {
		majoritySummaryCalled = true
		return true, nil
	}
	minoritySummary.AcceptF = func() (bool, error) {
		minoritySummaryCalled = true
		return true, nil
	}

	// let a majority of voters return summaryID, and a minority return minoritySummaryID. The rest timeout.
	cumulatedWeight := uint64(0)
	for syncer.pendingVoters.Len() != 0 {
		voterID, found := syncer.pendingVoters.Peek()
		assert.True(found)
		reqID := contactedVoters[voterID]

		switch {
		case cumulatedWeight < commonCfg.Alpha/2:
			assert.NoError(syncer.AcceptedStateSummary(
				voterID,
				reqID,
				[]ids.ID{summaryID, minoritySummaryID},
			))
			bw, _ := vdrs.GetWeight(voterID)
			cumulatedWeight += bw

		case cumulatedWeight < commonCfg.Alpha:
			assert.NoError(syncer.AcceptedStateSummary(
				voterID,
				reqID,
				[]ids.ID{summaryID},
			))
			bw, _ := vdrs.GetWeight(voterID)
			cumulatedWeight += bw

		default:
			assert.NoError(syncer.GetAcceptedStateSummaryFailed(
				voterID,
				reqID,
			))
		}
	}

	// check that finally summary is passed to VM
	assert.True(majoritySummaryCalled)
	assert.False(minoritySummaryCalled)
}

func TestVotingIsRestartedIfMajorityIsNotReachedDueToTimeouts(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
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
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (block.StateSummary, error) {
		return minoritySummary, nil
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.NodeIDSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.pendingSeeders.Len() != 0)

	// let all contacted vdrs respond
	for syncer.pendingSeeders.Len() != 0 {
		beaconID, found := syncer.pendingSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		assert.NoError(syncer.StateSummaryFrontier(
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	assert.False(syncer.pendingSeeders.Len() != 0)

	minoritySummaryCalled := false
	minoritySummary.AcceptF = func() (bool, error) {
		minoritySummaryCalled = true
		return true, nil
	}

	// Let a majority of voters timeout.
	timedOutWeight := uint64(0)
	for syncer.pendingVoters.Len() != 0 {
		voterID, found := syncer.pendingVoters.Peek()
		assert.True(found)
		reqID := contactedVoters[voterID]

		// vdr carries the largest weight by far. Make sure it fails
		if timedOutWeight <= commonCfg.Alpha {
			assert.NoError(syncer.GetAcceptedStateSummaryFailed(
				voterID,
				reqID,
			))
			bw, _ := vdrs.GetWeight(voterID)
			timedOutWeight += bw
		} else {
			assert.NoError(syncer.AcceptedStateSummary(
				voterID,
				reqID,
				[]ids.ID{summaryID},
			))
		}
	}

	// No state summary is passed to VM
	assert.False(minoritySummaryCalled)

	// instead the whole process is restared
	assert.False(syncer.pendingVoters.Len() != 0) // no voters reached
	assert.True(syncer.pendingSeeders.Len() != 0) // frontiers providers reached again
}

func TestStateSyncIsStoppedIfEnoughVotesAreCastedWithNoClearMajority(t *testing.T) {
	assert := assert.New(t)

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
	sender.SendGetStateSummaryFrontierF = func(ss ids.NodeIDSet, reqID uint32) {
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
	fullVM.ParseStateSummaryF = func(b []byte) (block.StateSummary, error) {
		switch {
		case bytes.Equal(b, summaryBytes):
			return minoritySummary1, nil
		case bytes.Equal(b, minoritySummaryBytes):
			return minoritySummary2, nil
		default:
			return nil, errors.New("unknown state summary")
		}
	}

	contactedVoters := make(map[ids.NodeID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.NodeIDSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.pendingSeeders.Len() != 0)

	// let all contacted vdrs respond with majority or minority summaries
	for {
		reachedSeeders := syncer.pendingSeeders.Len()
		if reachedSeeders == 0 {
			break
		}
		beaconID, found := syncer.pendingSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		if reachedSeeders%2 == 0 {
			assert.NoError(syncer.StateSummaryFrontier(
				beaconID,
				reqID,
				summaryBytes,
			))
		} else {
			assert.NoError(syncer.StateSummaryFrontier(
				beaconID,
				reqID,
				minoritySummaryBytes,
			))
		}
	}
	assert.False(syncer.pendingSeeders.Len() != 0)

	majoritySummaryCalled := false
	minoritySummaryCalled := false
	minoritySummary1.AcceptF = func() (bool, error) {
		majoritySummaryCalled = true
		return true, nil
	}
	minoritySummary2.AcceptF = func() (bool, error) {
		minoritySummaryCalled = true
		return true, nil
	}

	stateSyncFullyDone := false
	syncer.onDoneStateSyncing = func(lastReqID uint32) error {
		stateSyncFullyDone = true
		return nil
	}

	// let all votes respond in time without any summary reaching a majority.
	// We achieve it by making most nodes voting for an invalid summaryID.
	votingWeightStake := uint64(0)
	for syncer.pendingVoters.Len() != 0 {
		voterID, found := syncer.pendingVoters.Peek()
		assert.True(found)
		reqID := contactedVoters[voterID]

		switch {
		case votingWeightStake < commonCfg.Alpha/2:
			assert.NoError(syncer.AcceptedStateSummary(
				voterID,
				reqID,
				[]ids.ID{minoritySummary1.ID(), minoritySummary2.ID()},
			))
			bw, _ := vdrs.GetWeight(voterID)
			votingWeightStake += bw

		default:
			assert.NoError(syncer.AcceptedStateSummary(
				voterID,
				reqID,
				[]ids.ID{{'u', 'n', 'k', 'n', 'o', 'w', 'n', 'I', 'D'}},
			))
			bw, _ := vdrs.GetWeight(voterID)
			votingWeightStake += bw
		}
	}

	// check that finally summary is passed to VM
	assert.False(majoritySummaryCalled)
	assert.False(minoritySummaryCalled)
	assert.True(stateSyncFullyDone) // no restart, just move to boostrapping
}

func TestStateSyncIsDoneOnceVMNotifies(t *testing.T) {
	assert := assert.New(t)

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
	syncer.onDoneStateSyncing = func(lastReqID uint32) error {
		stateSyncFullyDone = true
		return nil
	}

	// Any Put response before StateSyncDone is received from VM is dropped
	assert.NoError(syncer.Notify(common.StateSyncDone))
	assert.True(stateSyncFullyDone)
}
