// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	key         uint64 = 2022
	minorityKey uint64 = 2000
)

var (
	_ block.ChainVM         = fullVM{}
	_ block.StateSyncableVM = fullVM{}

	unknownSummaryID = ids.ID{'g', 'a', 'r', 'b', 'a', 'g', 'e'}

	summaryBytes = []byte{'s', 'u', 'm', 'm', 'a', 'r', 'y'}
	summaryID    ids.ID

	minoritySummaryBytes = []byte{'m', 'i', 'n', 'o', 'r', 'i', 't', 'y'}
	minoritySummaryID    ids.ID
)

func init() {
	var err error
	summaryID, err = ids.ToID(hashing.ComputeHash256(summaryBytes))
	if err != nil {
		panic(err)
	}

	minoritySummaryID, err = ids.ToID(hashing.ComputeHash256(minoritySummaryBytes))
	if err != nil {
		panic(err)
	}
}

type fullVM struct {
	*block.TestVM
	*block.TestStateSyncableVM
}

func buildTestPeers(t *testing.T) validators.Set {
	// we consider more than common.MaxOutstandingBroadcastRequests peers
	// so to test the effect of cap on number of requests sent out
	vdrs := validators.NewSet()
	for idx := 0; idx < 2*common.MaxOutstandingBroadcastRequests; idx++ {
		beaconID := ids.GenerateTestNodeID()
		require.NoError(t, vdrs.AddWeight(beaconID, uint64(1)))
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
	require.NoError(t, err)

	cfg, err := NewConfig(*commonCfg, nil, dummyGetter, fullVM)
	require.NoError(t, err)
	commonSyncer := New(cfg, func(lastReqID uint32) error { return nil })
	syncer, ok := commonSyncer.(*stateSyncer)
	require.True(t, ok)
	require.True(t, syncer.stateSyncVM != nil)

	fullVM.GetOngoingSyncStateSummaryF = func() (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}

	return syncer, fullVM, sender
}

func pickRandomFrom(nodes map[ids.NodeID]uint32) ids.NodeID {
	for node := range nodes {
		return node
	}
	return ids.EmptyNodeID
}
