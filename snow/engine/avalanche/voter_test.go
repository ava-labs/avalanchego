// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"testing"

	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestVotingFinishesWithAbandonedDep(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	mngr := vertex.NewTestManager(t)
	bootCfg.Manager = mngr
	engCfg.Manager = mngr
	transitive, err := newTransitive(engCfg)
	assert.NoError(t, err)
	assert.NoError(t, transitive.Start( /*startReqID*/ 0))

	// prepare 3 validators
	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2}
	vdr3 := ids.ShortID{3}

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	// add poll for request 1
	transitive.polls.Add(1, vdrs)

	vdrs = ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr3,
	)

	// add poll for request 2
	transitive.polls.Add(2, vdrs)

	// expect 2 pending polls
	assert.Equal(t, 2, transitive.polls.Len())

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

	voter1.Update()
	voter3.Update()

	// still expect 2 pending polls since request 1 voting is still pending
	assert.Equal(t, 2, transitive.polls.Len())

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

	voter1.Update() // does nothing because the dependency is still pending
	voter2.Update() // voter1 is still remaining with the pending dependency

	voter1.Abandon(voter1Dep) // voter1 abandons dep1

	// expect all polls to have finished
	assert.Equal(t, 0, transitive.polls.Len())
}

func TestVotingFinishesWithAbandonDepMiddleRequest(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	mngr := vertex.NewTestManager(t)
	bootCfg.Manager = mngr
	engCfg.Manager = mngr
	transitive, err := newTransitive(engCfg)
	assert.NoError(t, err)
	assert.NoError(t, transitive.Start( /*startReqID*/ 0))

	// prepare 3 validators
	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2}
	vdr3 := ids.ShortID{3}

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	// add poll for request 1
	transitive.polls.Add(1, vdrs)

	vdrs = ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr3,
	)

	// add poll for request 2
	transitive.polls.Add(2, vdrs)

	vdrs = ids.ShortBag{}
	vdrs.Add(
		vdr2,
		vdr3,
	)

	// add poll for request 3
	transitive.polls.Add(3, vdrs)

	// expect 3 pending polls
	assert.Equal(t, 3, transitive.polls.Len())

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

	req3Voter1.Update()
	req3Voter2.Update()

	// expect 3 pending polls since 2 and 1 are still pending
	assert.Equal(t, 3, transitive.polls.Len())

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

	req2Voter1.Update() // does nothing because dep is unfulfilled
	req2Voter2.Update()

	// still expect 3 pending polls since request 1 voting is still pending
	assert.Equal(t, 3, transitive.polls.Len())

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

	req1Voter1.Update() // does nothing because the req2/voter1 dependency is still pending
	req1Voter2.Update() // voter1 is still remaining with the pending dependency

	// abandon dep on voter3
	req2Voter2.Abandon(req2Voter2Dep) // voter3 abandons dep1

	// expect polls to be pending as req1/voter1's dep is still unfulfilled
	assert.Equal(t, 3, transitive.polls.Len())

	req1Voter1.Abandon(req1Voter1Dep)

	// expect all polls to have finished
	assert.Equal(t, 0, transitive.polls.Len())
}

func TestSharedDependency(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	mngr := vertex.NewTestManager(t)
	bootCfg.Manager = mngr
	engCfg.Manager = mngr
	transitive, err := newTransitive(engCfg)
	assert.NoError(t, err)
	assert.NoError(t, transitive.Start( /*startReqID*/ 0))

	// prepare 3 validators
	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2}
	vdr3 := ids.ShortID{3}

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	// add poll for request 1
	transitive.polls.Add(1, vdrs)

	vdrs = ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr3,
	)

	// add poll for request 2
	transitive.polls.Add(2, vdrs)

	vdrs = ids.ShortBag{}
	vdrs.Add(
		vdr2,
		vdr3,
	)

	// add poll for request 3
	transitive.polls.Add(3, vdrs)

	// expect 3 pending polls
	assert.Equal(t, 3, transitive.polls.Len())

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

	req3Voter1.Update()

	req3Voter2 := &voter{
		t:         transitive,
		requestID: 3,
		response:  []ids.ID{vote3},
		deps:      ids.NewSet(0),
		vdr:       vdr2,
	}

	req3Voter2.Update()

	// 3 polls pending because req 2 and 1 have not voted
	assert.Equal(t, 3, transitive.polls.Len())

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
	req2Voter1.Update()

	req2Voter2 := &voter{
		t:         transitive,
		requestID: 2,
		response:  []ids.ID{vote2},
		deps:      ids.NewSet(0),
		vdr:       vdr3,
	}

	req2Voter2.Update()

	// 3 polls pending as req 2 dependency is unfulfilled and 1 has not voted
	assert.Equal(t, 3, transitive.polls.Len())

	req1Voter1 := &voter{
		t:         transitive,
		requestID: 1,
		response:  []ids.ID{vote1},
		deps:      depSet,
		vdr:       vdr1,
	}

	// does nothing because dependency is unfulfilled
	req1Voter1.Update()

	req1Voter2 := &voter{
		t:         transitive,
		requestID: 1,
		response:  []ids.ID{vote1},
		deps:      ids.NewSet(0),
		vdr:       vdr2,
	}

	req1Voter2.Update()

	// 3 polls pending as req2 and req 1 dependencies are unfulfilled
	assert.Equal(t, 3, transitive.polls.Len())

	// abandon dependency
	req1Voter1.Abandon(dep)
	req2Voter1.Abandon(dep)

	// expect no pending polls
	assert.Equal(t, 0, transitive.polls.Len())
}
