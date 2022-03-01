// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"bytes"
	"fmt"
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
	beaconsIDs := []ids.ShortID{
		ids.GenerateTestShortID(),
		ids.GenerateTestShortID(),
		ids.GenerateTestShortID(),
	}
	for _, beaconID := range beaconsIDs {
		ctx.Log.AssertNoError(beacons.AddWeight(beaconID, uint64(1)))
	}
}

func TestStateSyncIsSkippedIfNoBeaconIsProvided(t *testing.T) {
	assert := assert.New(t)

	noBeacons := validators.NewSet()
	sender := &common.SenderTest{T: t}
	fullVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{T: t},
		},
		TestStateSyncableVM: &block.TestStateSyncableVM{
			TestStateSyncableVM: common.TestStateSyncableVM{T: t},
		},
	}

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      noBeacons,
		SampleK:      noBeacons.Len(),
		Alpha:        (noBeacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
		Sender:       sender,
	}
	dummyGetter, err := getter.New(fullVM, commonCfg)
	assert.NoError(err)
	dummyWeightTracker := tracker.NewWeightTracker(noBeacons, commonCfg.StartupAlpha)

	cfg, err := NewConfig(
		commonCfg,
		nil,
		dummyGetter,
		fullVM,
		dummyWeightTracker)
	assert.NoError(err)
	commonSyncer := New(cfg, func(lastReqID uint32) error { return nil })
	syncer, ok := commonSyncer.(*stateSyncer)
	assert.True(ok)
	assert.True(syncer.stateSyncVM != nil)

	// set VM to check for StateSync call
	stateSyncEmpty := false
	fullVM.CantStateSync = true
	fullVM.StateSyncF = func(s []common.Summary) error {
		if len(s) == 0 {
			stateSyncEmpty = true
		}
		return nil
	}

	// check Start returns no errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))

	// check that StateSync is called immediately with no frontiers
	assert.True(stateSyncEmpty)
}

func TestBeaconsAreReachedForFrontiersUponStartup(t *testing.T) {
	assert := assert.New(t)

	sender := &common.SenderTest{T: t}
	fullVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{T: t},
		},
		TestStateSyncableVM: &block.TestStateSyncableVM{
			TestStateSyncableVM: common.TestStateSyncableVM{T: t},
		},
	}

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      beacons.Len(),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
		Sender:       sender,
	}
	dummyGetter, err := getter.New(fullVM, commonCfg)
	assert.NoError(err)
	dummyWeightTracker := tracker.NewWeightTracker(beacons, commonCfg.StartupAlpha)

	cfg, err := NewConfig(
		commonCfg,
		nil,
		dummyGetter,
		fullVM,
		dummyWeightTracker)
	assert.NoError(err)
	commonSyncer := New(cfg, func(lastReqID uint32) error { return nil })
	syncer, ok := commonSyncer.(*stateSyncer)
	assert.True(ok)
	assert.True(syncer.stateSyncVM != nil)

	// set sender to track nodes reached out
	contactedBeacons := ids.NewShortSet(3)
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, u uint32) {
		contactedBeacons.Union(ss)
	}

	// check Start returns no errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))

	// check that all beacons are reached out for frontiers
	assert.True(beacons.Len() == len(contactedBeacons))
	for _, beacon := range beacons.List() {
		beaconID := beacon.ID()
		assert.True(contactedBeacons.Contains(beaconID))

		// check that beacon is duly marked is reached out
		assert.True(syncer.hasSeederBeenContacted(beaconID))
	}

	// check that, obviously, no summary is yet registered
	assert.True(len(syncer.weightedSummaries) == 0)
}

func TestUnRequestedStateSummaryFrontiersAreDropped(t *testing.T) {
	assert := assert.New(t)

	sender := &common.SenderTest{T: t}
	fullVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{T: t},
		},
		TestStateSyncableVM: &block.TestStateSyncableVM{
			TestStateSyncableVM: common.TestStateSyncableVM{T: t},
		},
	}

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      beacons.Len(),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
		Sender:       sender,
	}
	dummyGetter, err := getter.New(fullVM, commonCfg)
	assert.NoError(err)
	dummyWeightTracker := tracker.NewWeightTracker(beacons, commonCfg.StartupAlpha)

	cfg, err := NewConfig(
		commonCfg,
		nil,
		dummyGetter,
		fullVM,
		dummyWeightTracker)
	assert.NoError(err)
	commonSyncer := New(cfg, func(lastReqID uint32) error { return nil })
	syncer, ok := commonSyncer.(*stateSyncer)
	assert.True(ok)
	assert.True(syncer.stateSyncVM != nil)

	// set sender to track nodes reached out
	contactedBeacons := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedBeacons[nodeID] = reqID
		}
	}

	// check Start returns no errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))

	// mock VM to simulate a valid summary is returned
	key := []byte{'k', 'e', 'y'}
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	hash := []byte{'h', 'a', 's', 'h'}

	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		return key, hash, nil
	}

	// pick one of the beacons that have been reached out
	responsiveBeaconID := beacons.List()[0].ID()
	responsiveBeaconReqID := contactedBeacons[responsiveBeaconID]

	// check a response with wrong request ID is dropped
	assert.NoError(syncer.StateSummaryFrontier(
		responsiveBeaconID,
		responsiveBeaconReqID+1,
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
}

func TestMalformedStateSummaryFrontiersAreDropped(t *testing.T) {
	assert := assert.New(t)

	sender := &common.SenderTest{T: t}
	fullVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{T: t},
		},
		TestStateSyncableVM: &block.TestStateSyncableVM{
			TestStateSyncableVM: common.TestStateSyncableVM{T: t},
		},
	}

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      beacons.Len(),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
		Sender:       sender,
	}
	dummyGetter, err := getter.New(fullVM, commonCfg)
	assert.NoError(err)
	dummyWeightTracker := tracker.NewWeightTracker(beacons, commonCfg.StartupAlpha)

	cfg, err := NewConfig(
		commonCfg,
		nil,
		dummyGetter,
		fullVM,
		dummyWeightTracker)
	assert.NoError(err)
	commonSyncer := New(cfg, func(lastReqID uint32) error { return nil })
	syncer, ok := commonSyncer.(*stateSyncer)
	assert.True(ok)
	assert.True(syncer.stateSyncVM != nil)

	// set sender to track nodes reached out
	contactedBeacons := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedBeacons[nodeID] = reqID
		}
	}

	// check Start returns no errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))

	// mock VM to simulate an invalid summary is returned
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	isSummaryDecoded := false
	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		isSummaryDecoded = true
		return nil, nil, fmt.Errorf("invalid state summary")
	}

	// pick one of the beacons that have been reached out
	responsiveBeaconID := beacons.List()[0].ID()
	responsiveBeaconReqID := contactedBeacons[responsiveBeaconID]

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
}

func TestLateResponsesFromUnresponsiveFrontiersAreNotRecorded(t *testing.T) {
	assert := assert.New(t)

	sender := &common.SenderTest{T: t}
	fullVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{T: t},
		},
		TestStateSyncableVM: &block.TestStateSyncableVM{
			TestStateSyncableVM: common.TestStateSyncableVM{T: t},
		},
	}

	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      beacons.Len(),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
		Sender:       sender,
	}
	dummyGetter, err := getter.New(fullVM, commonCfg)
	assert.NoError(err)
	dummyWeightTracker := tracker.NewWeightTracker(beacons, commonCfg.StartupAlpha)

	cfg, err := NewConfig(
		commonCfg,
		nil,
		dummyGetter,
		fullVM,
		dummyWeightTracker)
	assert.NoError(err)
	commonSyncer := New(cfg, func(lastReqID uint32) error { return nil })
	syncer, ok := commonSyncer.(*stateSyncer)
	assert.True(ok)
	assert.True(syncer.stateSyncVM != nil)

	// set sender to track nodes reached out
	contactedBeacons := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedBeacons[nodeID] = reqID
		}
	}

	// check Start returns no errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))

	// pick one of the beacons that have been reached out
	unresponsiveBeaconID := beacons.List()[0].ID()
	unresponsiveBeaconReqID := contactedBeacons[unresponsiveBeaconID]

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

// once all beacons respond, with possibly only a minority
// having timeout, votes on collected frontiers are requested
func TestVotingRoundStart(t *testing.T) {
	assert := assert.New(t)

	sender := &common.SenderTest{T: t}
	fullVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{T: t},
		},
		TestStateSyncableVM: &block.TestStateSyncableVM{
			TestStateSyncableVM: common.TestStateSyncableVM{T: t},
		},
	}
	commonCfg := common.Config{
		Ctx:          snow.DefaultConsensusContextTest(),
		Beacons:      beacons,
		SampleK:      beacons.Len(),
		Alpha:        (beacons.Weight() + 1) / 2,
		StartupAlpha: (3*beacons.Weight() + 3) / 4,
		Sender:       sender,
	}
	dummyGetter, err := getter.New(fullVM, commonCfg)
	assert.NoError(err)
	dummyWeightTracker := tracker.NewWeightTracker(beacons, commonCfg.StartupAlpha)
	cfg, err := NewConfig(
		commonCfg,
		nil,
		dummyGetter,
		fullVM,
		dummyWeightTracker)
	assert.NoError(err)

	commonSyncer := New(cfg, func(lastReqID uint32) error { return nil })
	syncer, ok := commonSyncer.(*stateSyncer)
	assert.True(ok)
	assert.True(syncer.stateSyncVM != nil)

	// set sender to track nodes reached out
	contactedBeacons := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetStateSummaryFrontier = true
	sender.SendGetStateSummaryFrontierF = func(ss ids.ShortSet, reqID uint32) {
		for nodeID := range ss {
			contactedBeacons[nodeID] = reqID
		}
	}

	// check Start returns no errors
	assert.NoError(syncer.Start(uint32(0) /*startReqID*/))

	// mock VM to simulate a valid summary is returned
	summary := []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	fullVM.CantStateSyncGetKeyHash = true
	fullVM.StateSyncGetKeyHashF = func(s common.Summary) (common.SummaryKey, common.SummaryHash, error) {
		key := []byte{'k', 'e', 'y'}
		hash := []byte{'h', 'a', 's', 'h'}
		return key, hash, nil
	}

	contactedVoters := make(map[ids.ShortID]uint32) // nodeID -> reqID map
	sender.CantSendGetAcceptedStateSummary = true
	sender.SendGetAcceptedStateSummaryF = func(votersIDs ids.ShortSet, reqID uint32, sumList [][]byte) {
		for nodeID := range votersIDs {
			contactedVoters[nodeID] = reqID
		}
	}

	for idx, beacon := range beacons.List() {
		beaconID := beacon.ID()
		reqID := contactedBeacons[beaconID]

		// a majority of beacons returns its frontier
		if idx <= int(commonCfg.Alpha-1) {
			assert.NoError(syncer.StateSummaryFrontier(
				beaconID,
				reqID,
				summary,
			))
			continue
		}

		// the rest of the beacons timeout
		assert.NoError(syncer.GetStateSummaryFrontierFailed(
			beaconID,
			reqID,
		))
	}

	// once frontier is received from a majority of beacons,
	// all beacons are reached out for votes
	assert.True(beacons.Len() == len(contactedBeacons))
	for _, beacon := range beacons.List() {
		beaconID := beacon.ID()
		_, ok := contactedBeacons[beaconID]
		assert.True(ok)

		// check that beacon is duly marked is reached out
		assert.True(syncer.hasVoterBeenContacted(beaconID))
	}
}
