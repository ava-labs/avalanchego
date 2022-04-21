// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	safeMath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/version"
	"github.com/stretchr/testify/assert"
)

func TestStateSyncingStartsOnlyIfEnoughStakeIsConnected(t *testing.T) {
	assert := assert.New(t)

	vdrs := buildTestPeers(t)
	alpha := vdrs.Weight()
	startupAlpha := alpha

	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Validators:    vdrs,
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         alpha,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, _, sender := buildTestsObjects(t, &commonCfg)

	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, u uint32) {}

	// attempt starting bootstrapper with no stake connected. Bootstrapper should stall.
	startReqID := uint32(0)
	assert.NoError(syncer.Start(startReqID))
	assert.False(syncer.started)

	// attempt starting bootstrapper with not enough stake connected. Bootstrapper should stall.
	vdr0 := ids.GenerateTestShortID()
	assert.NoError(vdrs.AddWeight(vdr0, startupAlpha/2))
	assert.NoError(syncer.Connected(vdr0, version.CurrentApp))

	assert.NoError(syncer.Start(startReqID))
	assert.False(syncer.started)

	// finally attempt starting bootstrapper with enough stake connected. Frontiers should be requested.
	vdr := ids.GenerateTestShortID()
	assert.NoError(vdrs.AddWeight(vdr, startupAlpha))
	assert.NoError(syncer.Connected(vdr, version.CurrentApp))

	assert.NoError(syncer.Start(startReqID))
	assert.True(syncer.started)
}

func TestStateSyncIsSkippedIfNotEnoughBeaconsAreConnected(t *testing.T) {
	assert := assert.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4
	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         (vdrs.Weight() + 1) / 2,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, _, _ := buildTestsObjects(t, &commonCfg)

	emptySummaryAccepted := false
	emptySummary.CantAccept = true
	emptySummary.AcceptF = func() error {
		emptySummaryAccepted = true
		return nil
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))

	// no summary is accepted if not enough beacons are available
	assert.False(emptySummaryAccepted)
}

func TestBeaconsAreReachedForFrontiersUponStartup(t *testing.T) {
	assert := assert.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4
	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         (vdrs.Weight() + 1) / 2,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, _, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := ids.NewShortSet(3)
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, u uint32) {
		contactedFrontiersProviders.Union(ss)
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}

	// check that vdrs are reached out for frontiers
	assert.True(len(contactedFrontiersProviders) == safeMath.Min(vdrs.Len(), maxOutstandingStateSyncRequests))
	for beaconID := range contactedFrontiersProviders {
		// check that beacon is duly marked as reached out
		assert.True(syncer.contactedSeeders.Contains(beaconID))
	}

	// check that, obviously, no summary is yet registered
	assert.True(len(syncer.weightedSummaries) == 0)
}

func TestUnRequestedStateSummaryFrontiersAreDropped(t *testing.T) {
	assert := assert.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4
	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         (vdrs.Weight() + 1) / 2,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
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
	assert.True(initiallyReachedOutBeaconsSize <= maxOutstandingStateSyncRequests)

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
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
	assert.True(syncer.contactedSeeders.Contains(responsiveBeaconID)) // responsiveBeacon still pending
	assert.True(len(syncer.weightedSummaries) == 0)

	// check a response from unsolicited node is dropped
	unsolicitedNodeID := ids.GenerateTestShortID()
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
	assert.False(syncer.contactedSeeders.Contains(responsiveBeaconID))

	// valid summary is recorded
	ws, ok := syncer.weightedSummaries[summaryID]
	assert.True(ok)
	assert.True(bytes.Equal(ws.Summary.Bytes(), summaryBytes))

	// other listed vdrs are reached for data
	assert.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == vdrs.Len())
}

func TestMalformedStateSummaryFrontiersAreDropped(t *testing.T) {
	assert := assert.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4
	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         (vdrs.Weight() + 1) / 2,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
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
	assert.True(initiallyReachedOutBeaconsSize <= maxOutstandingStateSyncRequests)

	// mock VM to simulate an invalid summary is returned
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	isSummaryDecoded := false
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		isSummaryDecoded = true
		return nil, fmt.Errorf("invalid state summary")
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
	assert.False(syncer.contactedSeeders.Contains(responsiveBeaconID))

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
	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         (vdrs.Weight() + 1) / 2,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
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
	assert.True(initiallyReachedOutBeaconsSize <= maxOutstandingStateSyncRequests)

	// pick one of the vdrs that have been reached out
	unresponsiveBeaconID := pickRandomFrom(contactedFrontiersProviders)
	unresponsiveBeaconReqID := contactedFrontiersProviders[unresponsiveBeaconID]

	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		assert.True(len(summaryBytes) == 0)
		return nil, fmt.Errorf("empty summary")
	}

	// assume timeout is reached and vdrs is marked as unresponsive
	assert.NoError(syncer.GetStateSummaryFrontierFailed(
		unresponsiveBeaconID,
		unresponsiveBeaconReqID,
	))

	// unresponsiveBeacon not pending anymore
	assert.False(syncer.contactedSeeders.Contains(unresponsiveBeaconID))
	assert.True(syncer.failedSeeders.Contains(unresponsiveBeaconID))

	// even in case of timeouts, other listed vdrs
	// are reached for data
	assert.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == vdrs.Len())

	// mock VM to simulate a valid but late summary is returned

	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
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

func TestVoteRequestsAreSentAsAllFrontierBeaconsResponded(t *testing.T) {
	assert := assert.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4
	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         (vdrs.Weight() + 1) / 2,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(b []byte) (common.Summary, error) {
		assert.True(bytes.Equal(b, summaryBytes))
		return &block.TestSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted vdrs respond
	for syncer.contactedSeeders.Len() != 0 {
		beaconID, found := syncer.contactedSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		assert.NoError(syncer.StateSummaryFrontier(
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	assert.False(syncer.contactedSeeders.Len() != 0)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	assert.True(initiallyContactedVotersSize > 0)
	assert.True(initiallyContactedVotersSize <= maxOutstandingStateSyncRequests)
}

func TestUnRequestedVotesAreDropped(t *testing.T) {
	assert := assert.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4
	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         (vdrs.Weight() + 1) / 2,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted vdrs respond
	for syncer.contactedSeeders.Len() != 0 {
		beaconID, found := syncer.contactedSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		assert.NoError(syncer.StateSummaryFrontier(
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	assert.False(syncer.contactedSeeders.Len() != 0)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	assert.True(initiallyContactedVotersSize > 0)
	assert.True(initiallyContactedVotersSize <= maxOutstandingStateSyncRequests)

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
	assert.True(syncer.contactedVoters.Contains(responsiveVoterID))
	assert.True(syncer.weightedSummaries[summaryID].weight == 0)

	// check a response from unsolicited node is dropped
	unsolicitedVoterID := ids.GenerateTestShortID()
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
	assert.False(syncer.contactedSeeders.Contains(responsiveVoterID))
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
	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         (vdrs.Weight() + 1) / 2,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
			HeightV: key,
			IDV:     summaryID,
			BytesV:  summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted vdrs respond
	for syncer.contactedSeeders.Len() != 0 {
		beaconID, found := syncer.contactedSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		assert.NoError(syncer.StateSummaryFrontier(
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	assert.False(syncer.contactedSeeders.Len() != 0)

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	assert.True(initiallyContactedVotersSize > 0)
	assert.True(initiallyContactedVotersSize <= maxOutstandingStateSyncRequests)

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
	assert.False(syncer.contactedSeeders.Contains(responsiveVoterID))
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

func TestSummaryIsPassedToVMAsMajorityOfVotesIsCastedForIt(t *testing.T) {
	assert := assert.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4
	commonCfg := common.Config{
		Ctx:           snow.DefaultConsensusContextTest(),
		Beacons:       vdrs,
		SampleK:       vdrs.Len(),
		Alpha:         (vdrs.Weight() + 1) / 2,
		WeightTracker: tracker.NewWeightTracker(vdrs, startupAlpha),
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	summary := &block.TestSummary{
		HeightV: key,
		IDV:     summaryID,
		BytesV:  summaryBytes,
		T:       t,
	}
	minoritySummary := &block.TestSummary{
		HeightV: minorityKey,
		IDV:     minoritySummaryID,
		BytesV:  minoritySummaryBytes,
		T:       t,
	}

	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(b []byte) (common.Summary, error) {
		switch {
		case bytes.Equal(b, summaryBytes):
			return summary, nil
		case bytes.Equal(b, minoritySummaryBytes):
			return minoritySummary, nil
		default:
			return nil, fmt.Errorf("unknown state summary")
		}
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted vdrs respond with majority or minority summaries
	for {
		reachedSeeders := syncer.contactedSeeders.Len()
		if reachedSeeders == 0 {
			break
		}
		beaconID, found := syncer.contactedSeeders.Peek()
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
	assert.False(syncer.contactedSeeders.Len() != 0)

	majoritySummaryCalled := false
	minoritySummaryCalled := false
	summary.AcceptF = func() error {
		majoritySummaryCalled = true
		return nil
	}
	minoritySummary.AcceptF = func() error {
		minoritySummaryCalled = true
		return nil
	}

	// let a majority of voters return summaryID, and a minority return minoritySummaryID. The rest timeout.
	cumulatedWeight := uint64(0)
	for syncer.contactedVoters.Len() != 0 {
		voterID, found := syncer.contactedVoters.Peek()
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

func TestVotingIsRestartedIfMajorityIsNotReached(t *testing.T) {
	assert := assert.New(t)

	vdrs := buildTestPeers(t)
	startupAlpha := (3*vdrs.Weight() + 3) / 4
	commonCfg := common.Config{
		Ctx:                         snow.DefaultConsensusContextTest(),
		Beacons:                     vdrs,
		SampleK:                     vdrs.Len(),
		Alpha:                       (vdrs.Weight() + 1) / 2,
		WeightTracker:               tracker.NewWeightTracker(vdrs, startupAlpha),
		RetryBootstrap:              true, // this sets RetryStateSyncing too
		RetryBootstrapWarnFrequency: 1,    // this sets RetrySyncingWarnFrequency too
	}
	syncer, fullVM, sender := buildTestsObjects(t, &commonCfg)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	minoritySummary := &block.TestSummary{
		HeightV: minorityKey,
		IDV:     minoritySummaryID,
		BytesV:  minoritySummaryBytes,
		T:       t,
	}
	fullVM.CantParseStateSummary = true
	fullVM.ParseStateSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return minoritySummary, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Connect enough stake to start syncer
	for _, vdr := range vdrs.List() {
		assert.NoError(syncer.Connected(vdr.ID(), version.CurrentApp))
	}
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted vdrs respond
	for syncer.contactedSeeders.Len() != 0 {
		beaconID, found := syncer.contactedSeeders.Peek()
		assert.True(found)
		reqID := contactedFrontiersProviders[beaconID]

		assert.NoError(syncer.StateSummaryFrontier(
			beaconID,
			reqID,
			summaryBytes,
		))
	}
	assert.False(syncer.contactedSeeders.Len() != 0)

	minoritySummaryCalled := false
	minoritySummary.AcceptF = func() error {
		minoritySummaryCalled = true
		return nil
	}

	// Let a majority of voters timeout.
	timedOutWeight := uint64(0)
	for syncer.contactedVoters.Len() != 0 {
		voterID, found := syncer.contactedVoters.Peek()
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
	assert.False(syncer.contactedVoters.Len() != 0) // no voters reached
	assert.True(syncer.contactedSeeders.Len() != 0) // frontiers providers reached again
}
