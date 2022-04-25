// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"testing"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/stretchr/testify/assert"
)

func TestAtStateSyncDoneLastSummaryBlockIsRequested(t *testing.T) {
	// TODO(darioush): Fix/replace test
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

	// if Put message is received, state sync is declared done
	// fullVM.ParseStateSyncableBlockF = successfulParseSyncableBlockBlkMock
	// assert.NoError(syncer.Put(reachedNodeID, sentReqID, []byte{}))
	// assert.True(stateSyncFullyDone)
}
