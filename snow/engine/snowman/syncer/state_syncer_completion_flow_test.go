// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/stretchr/testify/assert"
)

func TestAtStateSyncDoneLastSummaryBlockIsRequested(t *testing.T) {
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

	stateSyncFullyDone := false
	syncer.onDoneStateSyncing = func(lastReqID uint32) error {
		stateSyncFullyDone = true
		return nil
	}

	// mock VM to return lastSummaryBlkID and be able to receive full block
	lastSummaryBlkID := ids.ID{'b', 'l', 'k', 'I', 'D'}
	fullVM.CantGetLastSummaryBlockID = true
	fullVM.GetLastSummaryBlockIDF = func() (ids.ID, error) {
		return lastSummaryBlkID, nil
	}
	fullVM.CantSetLastSummaryBlock = true
	fullVM.SetLastSummaryBlockF = func(b []byte) error { return nil }

	// mock sender to record requested blkID
	var (
		blkRequested  bool
		reachedNodeID ids.ShortID
		sentReqID     uint32
	)
	sender.CantSendGet = true
	sender.SendGetF = func(nodeID ids.ShortID, reqID uint32, reqBlkID ids.ID) {
		blkRequested = true
		reachedNodeID = nodeID
		sentReqID = reqID
		assert.True(reqBlkID == lastSummaryBlkID)
	}

	assert.NoError(syncer.Notify(common.StateSyncDone))
	assert.True(blkRequested)
	assert.False(stateSyncFullyDone)

	// if Put message is not received, block is requested again (to a random beacon)
	blkRequested = false
	assert.NoError(syncer.GetFailed(reachedNodeID, sentReqID))
	assert.True(blkRequested)
	assert.False(stateSyncFullyDone)

	// if Put message is received, state sync is declared done
	assert.NoError(syncer.Put(reachedNodeID, sentReqID, []byte{}))
	assert.True(stateSyncFullyDone)
}
