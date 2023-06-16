// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestNewSetErrorOnMetrics(t *testing.T) {
	require := require.New(t)

	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()

	require.NoError(registerer.Register(prometheus.NewCounter(prometheus.CounterOpts{
		Name: "polls",
	})))
	require.NoError(registerer.Register(prometheus.NewCounter(prometheus.CounterOpts{
		Name: "poll_duration",
	})))

	require.NotNil(NewSet(factory, log, namespace, registerer))
}

func TestCreateAndFinishPollOutOfOrder_NewerFinishesFirst(t *testing.T) {
	require := require.New(t)

	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	// create validators
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdr3 := ids.NodeID{3}

	vdrs := []ids.NodeID{vdr1, vdr2, vdr3}

	// create two polls for the two vtxs
	vdrBag := bag.Bag[ids.NodeID]{}
	vdrBag.Add(vdrs...)
	require.True(s.Add(1, vdrBag))

	vdrBag = bag.Bag[ids.NodeID]{}
	vdrBag.Add(vdrs...)
	require.True(s.Add(2, vdrBag))
	require.Equal(s.Len(), 2)

	// vote vtx1 for poll 1
	// vote vtx2 for poll 2
	vtx1 := ids.ID{1}
	vtx2 := ids.ID{2}

	var results []bag.Bag[ids.ID]

	// vote out of order
	require.Empty(s.Vote(1, vdr1, vtx1))
	require.Empty(s.Vote(2, vdr2, vtx2))
	require.Empty(s.Vote(2, vdr3, vtx2))

	// poll 2 finished
	require.Empty(s.Vote(2, vdr1, vtx2)) // expect 2 to not have finished because 1 is still pending

	require.Empty(s.Vote(1, vdr2, vtx1))

	results = s.Vote(1, vdr3, vtx1) // poll 1 finished, poll 2 should be finished as well
	require.Len(results, 2)
	require.Equal(vtx1, results[0].List()[0])
	require.Equal(vtx2, results[1].List()[0])
}

func TestCreateAndFinishPollOutOfOrder_OlderFinishesFirst(t *testing.T) {
	require := require.New(t)

	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	// create validators
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdr3 := ids.NodeID{3}

	vdrs := []ids.NodeID{vdr1, vdr2, vdr3}

	// create two polls for the two vtxs
	vdrBag := bag.Bag[ids.NodeID]{}
	vdrBag.Add(vdrs...)
	require.True(s.Add(1, vdrBag))

	vdrBag = bag.Bag[ids.NodeID]{}
	vdrBag.Add(vdrs...)
	require.True(s.Add(2, vdrBag))
	require.Equal(s.Len(), 2)

	// vote vtx1 for poll 1
	// vote vtx2 for poll 2
	vtx1 := ids.ID{1}
	vtx2 := ids.ID{2}

	var results []bag.Bag[ids.ID]

	// vote out of order
	require.Empty(s.Vote(1, vdr1, vtx1))
	require.Empty(s.Vote(2, vdr2, vtx2))
	require.Empty(s.Vote(2, vdr3, vtx2))

	require.Empty(s.Vote(1, vdr2, vtx1))

	results = s.Vote(1, vdr3, vtx1) // poll 1 finished, poll 2 still remaining
	require.Len(results, 1)         // because 1 is the oldest
	require.Equal(vtx1, results[0].List()[0])

	results = s.Vote(2, vdr1, vtx2) // poll 2 finished
	require.Len(results, 1)         // because 2 is the oldest now
	require.Equal(vtx2, results[0].List()[0])
}

func TestCreateAndFinishPollOutOfOrder_UnfinishedPollsGaps(t *testing.T) {
	require := require.New(t)

	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	// create validators
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdr3 := ids.NodeID{3}

	vdrs := []ids.NodeID{vdr1, vdr2, vdr3}

	// create three polls for the two vtxs
	vdrBag := bag.Bag[ids.NodeID]{}
	vdrBag.Add(vdrs...)
	require.True(s.Add(1, vdrBag))

	vdrBag = bag.Bag[ids.NodeID]{}
	vdrBag.Add(vdrs...)
	require.True(s.Add(2, vdrBag))

	vdrBag = bag.Bag[ids.NodeID]{}
	vdrBag.Add(vdrs...)
	require.True(s.Add(3, vdrBag))
	require.Equal(s.Len(), 3)

	// vote vtx1 for poll 1
	// vote vtx2 for poll 2
	// vote vtx3 for poll 3
	vtx1 := ids.ID{1}
	vtx2 := ids.ID{2}
	vtx3 := ids.ID{3}

	var results []bag.Bag[ids.ID]

	// vote out of order
	// 2 finishes first to create a gap of finished poll between two unfinished polls 1 and 3
	require.Empty(s.Vote(2, vdr3, vtx2))
	require.Empty(s.Vote(2, vdr2, vtx2))
	require.Empty(s.Vote(2, vdr1, vtx2))

	// 3 finishes now, 2 has already finished but 1 is not finished so we expect to receive no results still
	require.Empty(s.Vote(3, vdr2, vtx3))
	require.Empty(s.Vote(3, vdr3, vtx3))
	require.Empty(s.Vote(3, vdr1, vtx3))

	// 1 finishes now, 2 and 3 have already finished so we expect 3 items in results
	require.Empty(s.Vote(1, vdr1, vtx1))
	require.Empty(s.Vote(1, vdr2, vtx1))
	results = s.Vote(1, vdr3, vtx1)
	require.Len(results, 3)
	require.Equal(vtx1, results[0].List()[0])
	require.Equal(vtx2, results[1].List()[0])
	require.Equal(vtx3, results[2].List()[0])
}

func TestCreateAndFinishSuccessfulPoll(t *testing.T) {
	require := require.New(t)

	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	vtxID := ids.ID{1}

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2} // k = 2

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	require.Zero(s.Len())

	require.True(s.Add(0, vdrs))
	require.Equal(1, s.Len())

	require.False(s.Add(0, vdrs))
	require.Equal(1, s.Len())

	require.Empty(s.Vote(1, vdr1, vtxID))
	require.Empty(s.Vote(0, vdr1, vtxID))
	require.Empty(s.Vote(0, vdr1, vtxID))

	results := s.Vote(0, vdr2, vtxID)
	require.Len(results, 1)
	list := results[0].List()
	require.Len(list, 1)
	require.Equal(vtxID, list[0])
	require.Equal(2, results[0].Count(vtxID))
}

func TestCreateAndFinishFailedPoll(t *testing.T) {
	require := require.New(t)

	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2} // k = 2

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	require.Zero(s.Len())

	require.True(s.Add(0, vdrs))
	require.Equal(1, s.Len())

	require.False(s.Add(0, vdrs))
	require.Equal(1, s.Len())

	require.Empty(s.Drop(1, vdr1))
	require.Empty(s.Drop(0, vdr1))
	require.Empty(s.Drop(0, vdr1))

	results := s.Drop(0, vdr2)
	require.Len(results, 1)
	require.Empty(results[0].List())
}

func TestSetString(t *testing.T) {
	require := require.New(t)

	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	vdr1 := ids.NodeID{1} // k = 1

	vdrs := bag.Bag[ids.NodeID]{}
	vdrs.Add(vdr1)

	expected := `current polls: (Size = 1)
    RequestID 0:
        waiting on Bag[ids.NodeID]: (Size = 1)
            NodeID-6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt: 1
        received Bag[ids.ID]: (Size = 0)`
	require.True(s.Add(0, vdrs))
	require.Equal(expected, s.String())
}
