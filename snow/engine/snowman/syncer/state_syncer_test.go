// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"testing"

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
