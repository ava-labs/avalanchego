// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
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

	beacons     validators.Set
	summary     []byte
	key         []byte
	hash        []byte
	unknownHash []byte
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

	summary = []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	key = []byte{'k', 'e', 'y'}
	hash = []byte{'h', 'a', 's', 'h'}
	unknownHash = []byte{'g', 'a', 'r', 'b', 'a', 'g', 'e'}
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
