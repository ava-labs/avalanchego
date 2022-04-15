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
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	safeMath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/stretchr/testify/assert"
)

func TestStateSyncIsSkippedIfNoBeaconIsProvided(t *testing.T) {
	assert := assert.New(t)

	noBeacons := validators.NewSet()
	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      noBeacons,
		SampleK:      int(noBeacons.Weight()),
		Alpha:        (noBeacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
	}
	syncer, fullVM, _ := buildTestsObjects(&commonCfg, t)

	// set VM to check for StateSync call
	stateSyncEmpty := false
	fullVM.CantStateSync = true
	fullVM.StateSyncF = func(s []common.Summary) error {
		if len(s) == 0 {
			stateSyncEmpty = true
		}
		return nil
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))

	// check that StateSync is called immediately with no frontiers
	assert.True(stateSyncEmpty)
}

func TestBeaconsAreReachedForFrontiersUponStartup(t *testing.T) {
	assert := assert.New(t)

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      int(beacons.Weight()),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
	}
	syncer, _, sender := buildTestsObjects(&commonCfg, t)

	// set sender to track nodes reached out
	contactedFrontiersProviders := ids.NewShortSet(3)
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, u uint32) {
		contactedFrontiersProviders.Union(ss)
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))

	// check that beacons are reached out for frontiers
	assert.True(len(contactedFrontiersProviders) == safeMath.Min(beacons.Len(), maxOutstandingStateSyncRequests))
	for beaconID := range contactedFrontiersProviders {
		// check that beacon is duly marked as reached out
		assert.True(syncer.contactedSeeders.Contains(beaconID))
	}

	// check that, obviously, no summary is yet registered
	assert.True(len(syncer.weightedSummaries) == 0)
}

func TestUnRequestedStateSummaryFrontiersAreDropped(t *testing.T) {
	assert := assert.New(t)

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      int(beacons.Weight()),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
	}
	syncer, fullVM, sender := buildTestsObjects(&commonCfg, t)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))
	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	assert.True(initiallyReachedOutBeaconsSize > 0)
	assert.True(initiallyReachedOutBeaconsSize <= maxOutstandingStateSyncRequests)

	// mock VM to simulate a valid summary is returned
	fullVM.CantStateSyncParseSummary = true
	fullVM.StateSyncParseSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
			SummaryKey:   key,
			SummaryID:    summaryID,
			ContentBytes: summaryBytes,
		}, nil
	}

	// pick one of the beacons that have been reached out
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

	// other listed beacons are reached for data
	assert.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == beacons.Len())
}

func TestMalformedStateSummaryFrontiersAreDropped(t *testing.T) {
	assert := assert.New(t)

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      int(beacons.Weight()),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
	}
	syncer, fullVM, sender := buildTestsObjects(&commonCfg, t)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))
	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	assert.True(initiallyReachedOutBeaconsSize > 0)
	assert.True(initiallyReachedOutBeaconsSize <= maxOutstandingStateSyncRequests)

	// mock VM to simulate an invalid summary is returned
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	isSummaryDecoded := false
	fullVM.CantStateSyncParseSummary = true
	fullVM.StateSyncParseSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		isSummaryDecoded = true
		return nil, fmt.Errorf("invalid state summary")
	}

	// pick one of the beacons that have been reached out
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

	// even in case of invalid summaries, other listed beacons
	// are reached for data
	assert.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == beacons.Len())
}

func TestLateResponsesFromUnresponsiveFrontiersAreNotRecorded(t *testing.T) {
	assert := assert.New(t)

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      int(beacons.Weight()),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
	}
	syncer, fullVM, sender := buildTestsObjects(&commonCfg, t)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))
	initiallyReachedOutBeaconsSize := len(contactedFrontiersProviders)
	assert.True(initiallyReachedOutBeaconsSize > 0)
	assert.True(initiallyReachedOutBeaconsSize <= maxOutstandingStateSyncRequests)

	// pick one of the beacons that have been reached out
	unresponsiveBeaconID := pickRandomFrom(contactedFrontiersProviders)
	unresponsiveBeaconReqID := contactedFrontiersProviders[unresponsiveBeaconID]

	fullVM.CantStateSyncParseSummary = true
	fullVM.StateSyncParseSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		assert.True(len(summaryBytes) == 0)
		return nil, fmt.Errorf("empty summary")
	}

	// assume timeout is reached and beacons is marked as unresponsive
	assert.NoError(syncer.GetStateSummaryFrontierFailed(
		unresponsiveBeaconID,
		unresponsiveBeaconReqID,
	))

	// unresponsiveBeacon not pending anymore
	assert.False(syncer.contactedSeeders.Contains(unresponsiveBeaconID))
	assert.True(syncer.failedSeeders.Contains(unresponsiveBeaconID))

	// even in case of timeouts, other listed beacons
	// are reached for data
	assert.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == beacons.Len())

	// mock VM to simulate an valid but late summary is returned

	fullVM.CantStateSyncParseSummary = true
	fullVM.StateSyncParseSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
			SummaryKey:   key,
			SummaryID:    summaryID,
			ContentBytes: summaryBytes,
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

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      int(beacons.Weight()),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
	}
	syncer, fullVM, sender := buildTestsObjects(&commonCfg, t)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantStateSyncParseSummary = true
	fullVM.StateSyncParseSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
			SummaryKey:   key,
			SummaryID:    summaryID,
			ContentBytes: summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted beacons respond
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

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      int(beacons.Weight()),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
	}
	syncer, fullVM, sender := buildTestsObjects(&commonCfg, t)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantStateSyncParseSummary = true
	fullVM.StateSyncParseSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
			SummaryKey:   key,
			SummaryID:    summaryID,
			ContentBytes: summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted beacons respond
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
	voterWeight, found := beacons.GetWeight(responsiveVoterID)
	assert.True(found)
	assert.True(syncer.weightedSummaries[summaryID].weight == voterWeight)

	// other listed voters are reached out
	assert.True(
		len(contactedVoters) > initiallyContactedVotersSize ||
			len(contactedVoters) == beacons.Len())
}

func TestVotesForUnknownSummariesAreDropped(t *testing.T) {
	assert := assert.New(t)

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      int(beacons.Weight()),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
	}
	syncer, fullVM, sender := buildTestsObjects(&commonCfg, t)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantStateSyncParseSummary = true
	fullVM.StateSyncParseSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
			SummaryKey:   key,
			SummaryID:    summaryID,
			ContentBytes: summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted beacons respond
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
			len(contactedVoters) == beacons.Len())
}

func TestSummaryIsPassedToVMAsMajorityOfVotesIsCastedForIt(t *testing.T) {
	assert := assert.New(t)

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      int(beacons.Weight()),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
	}
	syncer, fullVM, sender := buildTestsObjects(&commonCfg, t)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantStateSyncParseSummary = true
	fullVM.StateSyncParseSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
			SummaryKey:   key,
			SummaryID:    summaryID,
			ContentBytes: summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted beacons respond
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

	isVMStateSyncCalled := false
	fullVM.CantStateSync = true
	fullVM.StateSyncF = func(summaries []common.Summary) error {
		isVMStateSyncCalled = true
		assert.True(len(summaries) == 1)
		assert.True(bytes.Equal(summaries[0].Bytes(), summaryBytes))
		return nil
	}

	// let just a majority of voters return the summary. The rest timeout.
	cumulatedWeight := uint64(0)
	for syncer.contactedVoters.Len() != 0 {
		voterID, found := syncer.contactedVoters.Peek()
		assert.True(found)
		reqID := contactedVoters[voterID]

		if cumulatedWeight < commonCfg.Alpha {
			assert.NoError(syncer.AcceptedStateSummary(
				voterID,
				reqID,
				[]ids.ID{summaryID},
			))
			bw, _ := beacons.GetWeight(voterID)
			cumulatedWeight += bw
		} else {
			assert.NoError(syncer.GetAcceptedStateSummaryFailed(
				voterID,
				reqID,
			))
		}
	}

	// check that finally summary is passed to VM
	assert.True(isVMStateSyncCalled)
}

func TestVotingIsRestartedIfMajorityIsNotReached(t *testing.T) {
	assert := assert.New(t)

	commonCfg := common.Config{
		Ctx:                         snow.DefaultConsensusContextTest(),
		Beacons:                     beacons,
		SampleK:                     int(beacons.Weight()),
		Alpha:                       (beacons.Weight() + 1) / 2,
		StartupAlpha:                (3*beacons.Weight() + 3) / 4,
		RetryBootstrap:              true, // this enable RetryStateSyncinc too
		RetryBootstrapWarnFrequency: 1,    // this enable RetrySyncingWarnFrequency too
	}
	syncer, fullVM, sender := buildTestsObjects(&commonCfg, t)

	// set sender to track nodes reached out
	contactedFrontiersProviders := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedFrontiersProviders[nodeID] = reqID
		}
	}

	// mock VM to simulate a valid summary is returned
	fullVM.CantStateSyncParseSummary = true
	fullVM.StateSyncParseSummaryF = func(summaryBytes []byte) (common.Summary, error) {
		return &block.TestSummary{
			SummaryKey:   key,
			SummaryID:    summaryID,
			ContentBytes: summaryBytes,
		}, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl []uint64) {
		for nodeID := range ss {
			contactedVoters[nodeID] = reqID
		}
	}

	// Start syncer without errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))
	assert.True(syncer.contactedSeeders.Len() != 0)

	// let all contacted beacons respond
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

	isVMStateSyncCalled := false
	fullVM.CantStateSync = true
	fullVM.StateSyncF = func(summaries []common.Summary) error {
		isVMStateSyncCalled = true
		assert.True(len(summaries) == 1)
		assert.True(bytes.Equal(summaries[0].Bytes(), summaryBytes))
		return nil
	}

	// Let a majority of voters timeout.
	timedOutWeight := uint64(0)
	for syncer.contactedVoters.Len() != 0 {
		voterID, found := syncer.contactedVoters.Peek()
		assert.True(found)
		reqID := contactedVoters[voterID]

		if timedOutWeight <= commonCfg.Alpha {
			assert.NoError(syncer.GetAcceptedStateSummaryFailed(
				voterID,
				reqID,
			))
			bw, _ := beacons.GetWeight(voterID)
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
	assert.False(isVMStateSyncCalled)

	// instead the whole process is restared
	assert.False(syncer.contactedVoters.Len() != 0) // no voters reached
	assert.True(syncer.contactedSeeders.Len() != 0) // frontiers providers reached again
}
