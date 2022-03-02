// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/stretchr/testify/assert"
)

var (
	_ block.ChainVM         = fullVM{}
	_ block.StateSyncableVM = fullVM{}

	beacons validators.Set
)

type fullVM struct {
	*block.TestVM
	*block.TestStateSyncableVM
}

func init() {
	ctx := snow.DefaultContextTest()
	beacons = validators.NewSet()

	for idx := 0; idx < 2*maxOutstandingStateSyncRequests; idx++ {
		beaconID := ids.GenerateTestShortID()
		err := beacons.AddWeight(beaconID, uint64(1))
		ctx.Log.AssertNoError(err)
	}
}

// helper to build
func buildTestsObjects(commonCfg *common.Config, t *testing.T) (
	*stateSyncer,
	*fullVM,
	*common.SenderTest,

) {
	sender := &common.SenderTest{T: t}
	commonCfg.Sender = sender

	fullVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{T: t},
		},
		TestStateSyncableVM: &block.TestStateSyncableVM{
			TestStateSyncableVM: common.TestStateSyncableVM{T: t},
		},
	}
	dummyGetter, err := getter.New(fullVM, *commonCfg)
	assert.NoError(t, err)
	dummyWeightTracker := tracker.NewWeightTracker(commonCfg.Beacons, commonCfg.StartupAlpha)

	cfg, err := NewConfig(
		*commonCfg,
		nil,
		dummyGetter,
		fullVM,
		dummyWeightTracker)
	assert.NoError(t, err)
	commonSyncer := New(cfg, func(lastReqID uint32) error { return nil })
	syncer, ok := commonSyncer.(*stateSyncer)
	assert.True(t, ok)
	assert.True(t, syncer.stateSyncVM != nil)

	return syncer, fullVM, sender
}

func min(rhs, lhs int) int {
	if rhs <= lhs {
		return rhs
	}
	return lhs
}

func pickRandomFrom(population map[ids.ShortID]uint32) ids.ShortID {
	rnd := rand.Intn(len(population)) // #nosec G404
	res := ids.ShortEmpty
	for k := range population {
		if rnd == 0 {
			res = k
			break
		}
		rnd--
	}
	return res
}

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
	assert.True(len(contactedFrontiersProviders) == min(beacons.Len(), maxOutstandingStateSyncRequests))
	for beaconID := range contactedFrontiersProviders {
		// check that beacon is duly marked as reached out
		assert.True(syncer.hasSeederBeenContacted(beaconID))
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
	key := []byte{'k', 'e', 'y'}
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	hash := []byte{'h', 'a', 's', 'h'}

	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		return key, hash, nil
	}

	// pick one of the beacons that have been reached out
	responsiveBeaconID := pickRandomFrom(contactedFrontiersProviders)
	responsiveBeaconReqID := contactedFrontiersProviders[responsiveBeaconID]

	// check a response with wrong request ID is dropped
	assert.NoError(syncer.StateSummaryFrontier(
		responsiveBeaconID,
		math.MaxInt32,
		summary,
	))
	assert.True(syncer.hasSeederBeenContacted(responsiveBeaconID)) // responsiveBeacon still pending
	assert.True(len(syncer.weightedSummaries) == 0)

	// check a response from unsolicited node is dropped
	unsolicitedNodeID := ids.GenerateTestShortID()
	assert.NoError(syncer.StateSummaryFrontier(
		unsolicitedNodeID,
		responsiveBeaconReqID,
		summary,
	))
	assert.True(len(syncer.weightedSummaries) == 0)

	// check a valid response is duly recorded
	assert.NoError(syncer.StateSummaryFrontier(
		responsiveBeaconID,
		responsiveBeaconReqID,
		summary,
	))

	// responsiveBeacon not pending anymore
	assert.False(syncer.hasSeederBeenContacted(responsiveBeaconID))

	// valid summary is recorded
	ws, ok := syncer.weightedSummaries[string(hash)]
	assert.True(ok)
	assert.True(bytes.Equal(ws.Summary, summary))

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
	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		isSummaryDecoded = true
		return nil, nil, fmt.Errorf("invalid state summary")
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
	assert.False(syncer.hasSeederBeenContacted(responsiveBeaconID))

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

	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		assert.True(len(s) == 0)
		return nil, nil, fmt.Errorf("empty summary")
	}

	// assume timeout is reached and beacons is marked as unresponsive
	assert.NoError(syncer.GetStateSummaryFrontierFailed(
		unresponsiveBeaconID,
		unresponsiveBeaconReqID,
	))

	// unresponsiveBeacon not pending anymore
	assert.False(syncer.hasSeederBeenContacted(unresponsiveBeaconID))
	assert.True(syncer.failedSeeders.Contains(unresponsiveBeaconID))

	// even in case of timeouts, other listed beacons
	// are reached for data
	assert.True(
		len(contactedFrontiersProviders) > initiallyReachedOutBeaconsSize ||
			len(contactedFrontiersProviders) == beacons.Len())

	// mock VM to simulate an valid but late summary is returned
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	key := []byte{'k', 'e', 'y'}
	hash := []byte{'h', 'a', 's', 'h'}
	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		return key, hash, nil
	}

	// check a valid but late response is not recorded
	assert.NoError(syncer.StateSummaryFrontier(
		unresponsiveBeaconID,
		unresponsiveBeaconReqID,
		summary,
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
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	key := []byte{'k', 'e', 'y'}
	hash := []byte{'h', 'a', 's', 'h'}
	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		return key, hash, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl [][]byte) {
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
			summary,
		))
	}
	assert.False(syncer.anyPendingSeederResponse())

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
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	key := []byte{'k', 'e', 'y'}
	hash := []byte{'h', 'a', 's', 'h'}
	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		return key, hash, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl [][]byte) {
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
			summary,
		))
	}
	assert.False(syncer.anyPendingSeederResponse())

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	assert.True(initiallyContactedVotersSize > 0)
	assert.True(initiallyContactedVotersSize <= maxOutstandingStateSyncRequests)

	_, found := syncer.weightedSummaries[string(hash)]
	assert.True(found)

	// pick one of the voters that have been reached out
	responsiveVoterID := pickRandomFrom(contactedVoters)
	responsiveVoterReqID := contactedVoters[responsiveVoterID]

	// check a response with wrong request ID is dropped
	assert.NoError(syncer.AcceptedStateSummary(
		responsiveVoterID,
		math.MaxInt32,
		[][]byte{hash},
	))

	// responsiveVoter still pending
	assert.True(syncer.hasVoterBeenContacted(responsiveVoterID))
	assert.True(syncer.weightedSummaries[string(hash)].weight == 0)

	// check a response from unsolicited node is dropped
	unsolicitedVoterID := ids.GenerateTestShortID()
	assert.NoError(syncer.AcceptedStateSummary(
		unsolicitedVoterID,
		responsiveVoterReqID,
		[][]byte{hash},
	))
	assert.True(syncer.weightedSummaries[string(hash)].weight == 0)

	// check a valid response is duly recorded
	assert.NoError(syncer.AcceptedStateSummary(
		responsiveVoterID,
		responsiveVoterReqID,
		[][]byte{hash},
	))

	// responsiveBeacon not pending anymore
	assert.False(syncer.hasSeederBeenContacted(responsiveVoterID))
	voterWeight, found := beacons.GetWeight(responsiveVoterID)
	assert.True(found)
	assert.True(syncer.weightedSummaries[string(hash)].weight == voterWeight)

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
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	key := []byte{'k', 'e', 'y'}
	hash := []byte{'h', 'a', 's', 'h'}
	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		return key, hash, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl [][]byte) {
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
			summary,
		))
	}
	assert.False(syncer.anyPendingSeederResponse())

	// check that vote requests are issued
	initiallyContactedVotersSize := len(contactedVoters)
	assert.True(initiallyContactedVotersSize > 0)
	assert.True(initiallyContactedVotersSize <= maxOutstandingStateSyncRequests)

	_, found := syncer.weightedSummaries[string(hash)]
	assert.True(found)

	// pick one of the voters that have been reached out
	responsiveVoterID := pickRandomFrom(contactedVoters)
	responsiveVoterReqID := contactedVoters[responsiveVoterID]

	// check a response for unRequested summary is dropped
	unknownHash := []byte{'g', 'a', 'r', 'b', 'a', 'g', 'e'}
	assert.NoError(syncer.AcceptedStateSummary(
		responsiveVoterID,
		responsiveVoterReqID,
		[][]byte{unknownHash},
	))
	_, found = syncer.weightedSummaries[string(unknownHash)]
	assert.False(found)

	// check that responsiveVoter cannot cast another vote
	assert.False(syncer.hasSeederBeenContacted(responsiveVoterID))
	assert.NoError(syncer.AcceptedStateSummary(
		responsiveVoterID,
		responsiveVoterReqID,
		[][]byte{hash},
	))
	assert.True(syncer.weightedSummaries[string(hash)].weight == 0)

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
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	key := []byte{'k', 'e', 'y'}
	hash := []byte{'h', 'a', 's', 'h'}
	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		return key, hash, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl [][]byte) {
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
			summary,
		))
	}
	assert.False(syncer.anyPendingSeederResponse())

	isVMStateSyncCalled := false
	fullVM.CantStateSync = true
	fullVM.StateSyncF = func(summaries []common.Summary) error {
		isVMStateSyncCalled = true
		assert.True(len(summaries) == 1)
		assert.True(bytes.Equal(summaries[0], summary))
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
				[][]byte{hash},
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
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	key := []byte{'k', 'e', 'y'}
	hash := []byte{'h', 'a', 's', 'h'}
	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		return key, hash, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(ss ids.ShortSet, reqID uint32, sl [][]byte) {
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
			summary,
		))
	}
	assert.False(syncer.anyPendingSeederResponse())

	isVMStateSyncCalled := false
	fullVM.CantStateSync = true
	fullVM.StateSyncF = func(summaries []common.Summary) error {
		isVMStateSyncCalled = true
		assert.True(len(summaries) == 1)
		assert.True(bytes.Equal(summaries[0], summary))
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
				[][]byte{hash},
			))
		}
	}

	// No state summary is passed to VM
	assert.False(isVMStateSyncCalled)

	// instead the whole process is restared
	assert.False(syncer.anyPendingVoterResponse()) // no voters reached
	assert.True(syncer.anyPendingSeederResponse()) // frontiers providers reached again
}
