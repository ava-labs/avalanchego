// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
)

func TestVotingFinishesWithAbandonedDep(t *testing.T) {
	_, _, engCfg := DefaultConfig()
	mngr := vertex.NewTestManager(t)
	engCfg.Manager = mngr
	transitive, err := newTransitive(engCfg)
	require.NoError(t, err)
	require.NoError(t, transitive.Start( /*startReqID*/ 0))

	// prepare 3 validators
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdr3 := ids.NodeID{3}

	vdrs := ids.NodeIDBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	// add poll for request 1
	transitive.polls.Add(1, vdrs)

	vdrs = ids.NodeIDBag{}
	vdrs.Add(
		vdr1,
		vdr3,
	)

	// add poll for request 2
	transitive.polls.Add(2, vdrs)

	// expect 2 pending polls
	require.Equal(t, 2, transitive.polls.Len())

	// vote on request 2 first
	vote1 := ids.GenerateTestID()
	vote2 := ids.GenerateTestID()

	voter1 := &voter{
		t:         transitive,
		requestID: 2,
		response:  []ids.ID{vote2},
		deps:      ids.NewSet(0),
		vdr:       vdr1,
	}

	voter3 := &voter{
		t:         transitive,
		requestID: 2,
		response:  []ids.ID{vote2},
		deps:      ids.NewSet(0),
		vdr:       vdr3,
	}

	voter1.Update(context.Background())
	voter3.Update(context.Background())

	// still expect 2 pending polls since request 1 voting is still pending
	require.Equal(t, 2, transitive.polls.Len())

	// vote on request 1
	// add dependency to voter1's vote which has to be fulfilled prior to finishing
	voter1Dep := ids.GenerateTestID()
	voter1DepSet := ids.NewSet(1)
	voter1DepSet.Add(voter1Dep)

	voter1 = &voter{
		t:         transitive,
		requestID: 1,
		response:  []ids.ID{vote1},
		deps:      voter1DepSet,
		vdr:       vdr1,
	}

	voter2 := &voter{
		t:         transitive,
		requestID: 1,
		response:  []ids.ID{vote1},
		deps:      ids.NewSet(0),
		vdr:       vdr2,
	}

	voter1.Update(context.Background()) // does nothing because the dependency is still pending
	voter2.Update(context.Background()) // voter1 is still remaining with the pending dependency

	voter1.Abandon(context.Background(), voter1Dep) // voter1 abandons dep1

	// expect all polls to have finished
	require.Equal(t, 0, transitive.polls.Len())
}

func TestVotingFinishesWithAbandonDepMiddleRequest(t *testing.T) {
	_, _, engCfg := DefaultConfig()
	mngr := vertex.NewTestManager(t)
	engCfg.Manager = mngr
	transitive, err := newTransitive(engCfg)
	require.NoError(t, err)
	require.NoError(t, transitive.Start( /*startReqID*/ 0))

	// prepare 3 validators
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdr3 := ids.NodeID{3}

	vdrs := ids.NodeIDBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	// add poll for request 1
	transitive.polls.Add(1, vdrs)

	vdrs = ids.NodeIDBag{}
	vdrs.Add(
		vdr1,
		vdr3,
	)

	// add poll for request 2
	transitive.polls.Add(2, vdrs)

	vdrs = ids.NodeIDBag{}
	vdrs.Add(
		vdr2,
		vdr3,
	)

	// add poll for request 3
	transitive.polls.Add(3, vdrs)

	// expect 3 pending polls
	require.Equal(t, 3, transitive.polls.Len())

	vote1 := ids.GenerateTestID()
	vote2 := ids.GenerateTestID()
	vote3 := ids.GenerateTestID()

	// vote on request 3 first
	req3Voter1 := &voter{
		t:         transitive,
		requestID: 3,
		response:  []ids.ID{vote3},
		deps:      ids.NewSet(0),
		vdr:       vdr3,
	}

	req3Voter2 := &voter{
		t:         transitive,
		requestID: 3,
		response:  []ids.ID{vote3},
		deps:      ids.NewSet(0),
		vdr:       vdr2,
	}

	req3Voter1.Update(context.Background())
	req3Voter2.Update(context.Background())

	// expect 3 pending polls since 2 and 1 are still pending
	require.Equal(t, 3, transitive.polls.Len())

	// vote on request 2
	// add dependency to req2/voter3's vote which has to be fulfilled prior to finishing
	req2Voter2Dep := ids.GenerateTestID()
	req2Voter2DepSet := ids.NewSet(1)
	req2Voter2DepSet.Add(req2Voter2Dep)

	req2Voter1 := &voter{
		t:         transitive,
		requestID: 2,
		response:  []ids.ID{vote2},
		deps:      ids.NewSet(0),
		vdr:       vdr1,
	}

	req2Voter2 := &voter{
		t:         transitive,
		requestID: 2,
		response:  []ids.ID{vote2},
		deps:      req2Voter2DepSet,
		vdr:       vdr3,
	}

	req2Voter1.Update(context.Background()) // does nothing because dep is unfulfilled
	req2Voter2.Update(context.Background())

	// still expect 3 pending polls since request 1 voting is still pending
	require.Equal(t, 3, transitive.polls.Len())

	// vote on request 1
	// add dependency to voter1's vote which has to be fulfilled prior to finishing
	req1Voter1Dep := ids.GenerateTestID()
	req1Voter1DepSet := ids.NewSet(1)
	req1Voter1DepSet.Add(req1Voter1Dep)
	req1Voter1 := &voter{
		t:         transitive,
		requestID: 1,
		response:  []ids.ID{vote1},
		deps:      req1Voter1DepSet,
		vdr:       vdr1,
	}

	req1Voter2 := &voter{
		t:         transitive,
		requestID: 1,
		response:  []ids.ID{vote1},
		deps:      ids.NewSet(0),
		vdr:       vdr2,
	}

	req1Voter1.Update(context.Background()) // does nothing because the req2/voter1 dependency is still pending
	req1Voter2.Update(context.Background()) // voter1 is still remaining with the pending dependency

	// abandon dep on voter3
	req2Voter2.Abandon(context.Background(), req2Voter2Dep) // voter3 abandons dep1

	// expect polls to be pending as req1/voter1's dep is still unfulfilled
	require.Equal(t, 3, transitive.polls.Len())

	req1Voter1.Abandon(context.Background(), req1Voter1Dep)

	// expect all polls to have finished
	require.Equal(t, 0, transitive.polls.Len())
}

func TestSharedDependency(t *testing.T) {
	_, _, engCfg := DefaultConfig()
	mngr := vertex.NewTestManager(t)
	engCfg.Manager = mngr
	transitive, err := newTransitive(engCfg)
	require.NoError(t, err)
	require.NoError(t, transitive.Start( /*startReqID*/ 0))

	// prepare 3 validators
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdr3 := ids.NodeID{3}

	vdrs := ids.NodeIDBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	// add poll for request 1
	transitive.polls.Add(1, vdrs)

	vdrs = ids.NodeIDBag{}
	vdrs.Add(
		vdr1,
		vdr3,
	)

	// add poll for request 2
	transitive.polls.Add(2, vdrs)

	vdrs = ids.NodeIDBag{}
	vdrs.Add(
		vdr2,
		vdr3,
	)

	// add poll for request 3
	transitive.polls.Add(3, vdrs)

	// expect 3 pending polls
	require.Equal(t, 3, transitive.polls.Len())

	vote1 := ids.GenerateTestID()
	vote2 := ids.GenerateTestID()
	vote3 := ids.GenerateTestID()

	// req3 voters all vote

	req3Voter1 := &voter{
		t:         transitive,
		requestID: 3,
		response:  []ids.ID{vote3},
		deps:      ids.NewSet(0),
		vdr:       vdr3,
	}

	req3Voter1.Update(context.Background())

	req3Voter2 := &voter{
		t:         transitive,
		requestID: 3,
		response:  []ids.ID{vote3},
		deps:      ids.NewSet(0),
		vdr:       vdr2,
	}

	req3Voter2.Update(context.Background())

	// 3 polls pending because req 2 and 1 have not voted
	require.Equal(t, 3, transitive.polls.Len())

	// setup common dependency
	dep := ids.GenerateTestID()
	depSet := ids.NewSet(1)
	depSet.Add(dep)

	req2Voter1 := &voter{
		t:         transitive,
		requestID: 2,
		response:  []ids.ID{vote2},
		deps:      depSet,
		vdr:       vdr1,
	}

	// does nothing because dependency is unfulfilled
	req2Voter1.Update(context.Background())

	req2Voter2 := &voter{
		t:         transitive,
		requestID: 2,
		response:  []ids.ID{vote2},
		deps:      ids.NewSet(0),
		vdr:       vdr3,
	}

	req2Voter2.Update(context.Background())

	// 3 polls pending as req 2 dependency is unfulfilled and 1 has not voted
	require.Equal(t, 3, transitive.polls.Len())

	req1Voter1 := &voter{
		t:         transitive,
		requestID: 1,
		response:  []ids.ID{vote1},
		deps:      depSet,
		vdr:       vdr1,
	}

	// does nothing because dependency is unfulfilled
	req1Voter1.Update(context.Background())

	req1Voter2 := &voter{
		t:         transitive,
		requestID: 1,
		response:  []ids.ID{vote1},
		deps:      ids.NewSet(0),
		vdr:       vdr2,
	}

	req1Voter2.Update(context.Background())

	// 3 polls pending as req2 and req 1 dependencies are unfulfilled
	require.Equal(t, 3, transitive.polls.Len())

	// abandon dependency
	req1Voter1.Abandon(context.Background(), dep)
	req2Voter1.Abandon(context.Background(), dep)

	// expect no pending polls
	require.Equal(t, 0, transitive.polls.Len())
}
