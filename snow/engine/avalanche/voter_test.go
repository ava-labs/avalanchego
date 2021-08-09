// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"testing"

	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestVotingFinishesWithAbandonedDep(t *testing.T) {
	transitive := &Transitive{}

	config := DefaultConfig()
	config.Manager = vertex.NewTestManager(t)
	err := transitive.Initialize(config)
	assert.NoError(t, err)

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
	transitive := &Transitive{}

	config := DefaultConfig()
	config.Manager = vertex.NewTestManager(t)
	err := transitive.Initialize(config)
	assert.NoError(t, err)

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

	voter2 := &voter{
		t:         transitive,
		requestID: 3,
		response:  []ids.ID{vote3},
		deps:      ids.NewSet(0),
		vdr:       vdr1,
	}

	voter3 := &voter{
		t:         transitive,
		requestID: 3,
		response:  []ids.ID{vote3},
		deps:      ids.NewSet(0),
		vdr:       vdr2,
	}

	voter2.Update()
	voter3.Update()

	// expect 3 pending polls since 2 and 1 are still pending
	assert.Equal(t, 3, transitive.polls.Len())

	// vote on request 2
	// add dependency to voter3's vote which has to be fulfilled prior to finishing
	voter3Dep := ids.GenerateTestID()
	voter3DepSet := ids.NewSet(1)
	voter3DepSet.Add(voter3Dep)

	voter1 := &voter{
		t:         transitive,
		requestID: 2,
		response:  []ids.ID{vote2},
		deps:      ids.NewSet(0),
		vdr:       vdr1,
	}

	voter3 = &voter{
		t:         transitive,
		requestID: 2,
		response:  []ids.ID{vote2},
		deps:      voter3DepSet,
		vdr:       vdr3,
	}

	voter1.Update() // does nothing because dep is unfulfilled
	voter3.Update()

	// still expect 3 pending polls since request 1 voting is still pending
	assert.Equal(t, 3, transitive.polls.Len())

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

	voter2 = &voter{
		t:         transitive,
		requestID: 1,
		response:  []ids.ID{vote1},
		deps:      ids.NewSet(0),
		vdr:       vdr2,
	}

	voter1.Update() // does nothing because the req2/voter1 dependency is still pending
	voter2.Update() // voter1 is still remaining with the pending dependency

	// abandon dep on voter3
	voter3.Abandon(voter3Dep) // voter3 abandons dep1
	voter1.Fulfill(voter1Dep)

	// expect all polls to have finished
	assert.Equal(t, 0, transitive.polls.Len())
}
