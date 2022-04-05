// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
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

	beacons          validators.Set
	summaryBytes     []byte
	key              uint64
	summaryID        ids.ID
	unknownSummaryID ids.ID
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

	summaryBytes = []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	key = uint64(2022)
	summaryID = ids.ID{'h', 'a', 's', 'h'}
	unknownSummaryID = ids.ID{'g', 'a', 'r', 'b', 'a', 'g', 'e'}
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

	fullVM.GetOngoingStateSyncSummaryF = func() (common.Summary, error) {
		return nil, common.ErrNoStateSyncOngoing
	}

	return syncer, fullVM, sender
}

func pickRandomFrom(population map[ids.ShortID]uint32) ids.ShortID {
	res := ids.ShortEmpty
	for k := range population {
		res = k
		break
	}
	return res
}
