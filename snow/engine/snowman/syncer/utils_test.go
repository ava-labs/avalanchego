// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/stretchr/testify/assert"
)

var (
	_ block.ChainVM         = fullVM{}
	_ block.StateSyncableVM = fullVM{}

	key          uint64
	summaryID    ids.ID
	summaryBytes []byte

	minorityKey          uint64
	minoritySummaryID    ids.ID
	minoritySummaryBytes []byte

	unknownSummaryID ids.ID
	emptySummary     *block.TestSummary
)

type fullVM struct {
	*block.TestVM
	*block.TestStateSyncableVM
}

func init() {
	var err error

	key = uint64(2022)
	summaryBytes = []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	summaryID, err = ids.ToID(hashing.ComputeHash256(summaryBytes))
	if err != nil {
		panic(err)
	}

	minorityKey = uint64(2000)
	minoritySummaryBytes = []byte{'m', 'i', 'n', 'o', 'r', 'i', 't', 'y'}
	minoritySummaryID, err = ids.ToID(hashing.ComputeHash256(minoritySummaryBytes))
	if err != nil {
		panic(err)
	}

	unknownSummaryID = ids.ID{'g', 'a', 'r', 'b', 'a', 'g', 'e'}

	emptySummary = &block.TestSummary{
		HeightV: 0,
		IDV:     ids.Empty,
		BytesV:  nil,
	}
}

// helper to build
func buildTestPeers(t *testing.T) validators.Set {
	vdrs := validators.NewSet()
	for idx := 0; idx < 2*maxOutstandingStateSyncRequests; idx++ {
		beaconID := ids.GenerateTestShortID()
		assert.NoError(t, vdrs.AddWeight(beaconID, uint64(1)))
	}
	return vdrs
}

func buildTestsObjects(t *testing.T, commonCfg *common.Config) (
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
			T: t,
		},
	}
	dummyGetter, err := getter.New(fullVM, *commonCfg)
	assert.NoError(t, err)

	cfg, err := NewConfig(*commonCfg, nil, dummyGetter, fullVM)
	assert.NoError(t, err)
	commonSyncer := New(cfg, func(lastReqID uint32) error { return nil })
	syncer, ok := commonSyncer.(*stateSyncer)
	assert.True(t, ok)
	assert.True(t, syncer.stateSyncVM != nil)

	fullVM.GetOngoingSyncStateSummaryF = func() (block.Summary, error) {
		emptySummary.T = t
		return emptySummary, nil
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
