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
)

type fullVM struct {
	*block.TestVM
	*block.TestStateSyncableVM
}

func TestStateSyncIsSkippedIfNoBeaconIsProvided(t *testing.T) {
	assert := assert.New(t)

	emptyBeaconsList := validators.NewSet()
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
		Ctx:     snow.DefaultConsensusContextTest(),
		Beacons: emptyBeaconsList,
		SampleK: emptyBeaconsList.Len(),
		Alpha:   (emptyBeaconsList.Weight() + 1) / 2,
		Sender:  sender,
	}
	dummyGetter, err := getter.New(fullVM, commonCfg)
	assert.NoError(err)
	dummyWeightTracker := tracker.NewWeightTracker(emptyBeaconsList, commonCfg.StartupAlpha)

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

func TestBeaconsAreReachedForFrontierUponStartup(t *testing.T) {
	assert := assert.New(t)

	beacons := validators.NewSet()
	beaconsIDs := []ids.ShortID{
		ids.GenerateTestShortID(),
		ids.GenerateTestShortID(),
		ids.GenerateTestShortID(),
	}
	for _, beaconID := range beaconsIDs {
		assert.NoError(beacons.AddWeight(beaconID, uint64(1)))
	}

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
		Ctx:     snow.DefaultConsensusContextTest(),
		Beacons: beacons,
		SampleK: beacons.Len(),
		Alpha:   (beacons.Weight() + 1) / 2,
		Sender:  sender,
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

	// check that all the beacons are reached out for frontiers
	assert.True(len(beaconsIDs) == len(contactedBeacons))
	for _, beaconID := range beaconsIDs {
		assert.True(contactedBeacons.Contains(beaconID))

		// check that beacon is duly marked is reached out
		assert.True(syncer.hasSeederBeenContacted(beaconID))
	}

	// check that, obviously, no summary is yet registered
	assert.True(len(syncer.weightedSummaries) == 0)
}

func TestUnRequestedStateSummaryFrontiersAreDropped(t *testing.T) {
	assert := assert.New(t)

	beacons := validators.NewSet()
	beaconsIDs := []ids.ShortID{
		ids.GenerateTestShortID(),
		ids.GenerateTestShortID(),
		ids.GenerateTestShortID(),
	}
	for _, beaconID := range beaconsIDs {
		assert.NoError(beacons.AddWeight(beaconID, uint64(1)))
	}

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
		Ctx:     snow.DefaultConsensusContextTest(),
		Beacons: beacons,
		SampleK: beacons.Len(),
		Alpha:   (beacons.Weight() + 1) / 2,
		Sender:  sender,
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
	responsiveBeaconID := beaconsIDs[0]
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

	beacons := validators.NewSet()
	beaconsIDs := []ids.ShortID{
		ids.GenerateTestShortID(),
	}
	for _, beaconID := range beaconsIDs {
		assert.NoError(beacons.AddWeight(beaconID, uint64(1)))
	}

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
		Ctx:     snow.DefaultConsensusContextTest(),
		Beacons: beacons,
		SampleK: beacons.Len(),
		Alpha:   (beacons.Weight() + 1) / 2,
		Sender:  sender,
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
	responsiveBeaconID := beaconsIDs[0]
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
