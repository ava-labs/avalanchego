// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	blkID1 = ids.ID{1}
	blkID2 = ids.ID{2}
	blkID3 = ids.ID{3}
	blkID4 = ids.ID{4}

	vdr1 = ids.BuildTestNodeID([]byte{0x01})
	vdr2 = ids.BuildTestNodeID([]byte{0x02})
	vdr3 = ids.BuildTestNodeID([]byte{0x03})
	vdr4 = ids.BuildTestNodeID([]byte{0x04})
	vdr5 = ids.BuildTestNodeID([]byte{0x05}) // k = 5
)

func TestNewSetErrorOnPollsMetrics(t *testing.T) {
	require := require.New(t)

	alpha := 1
	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	log := logging.NoLog{}
	registerer := prometheus.NewRegistry()

	require.NoError(registerer.Register(prometheus.NewCounter(prometheus.CounterOpts{
		Name: "polls",
	})))

	_, err := NewSet(factory, log, registerer)
	require.ErrorIs(err, errFailedPollsMetric)
}

func TestNewSetErrorOnPollDurationMetrics(t *testing.T) {
	require := require.New(t)

	alpha := 1
	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	log := logging.NoLog{}
	registerer := prometheus.NewRegistry()

	require.NoError(registerer.Register(prometheus.NewCounter(prometheus.CounterOpts{
		Name: "poll_duration_count",
	})))

	_, err := NewSet(factory, log, registerer)
	require.ErrorIs(err, errFailedPollDurationMetrics)
}

func TestCreateAndFinishPollOutOfOrder_NewerFinishesFirst(t *testing.T) {
	require := require.New(t)

	vdrs := []ids.NodeID{vdr1, vdr2, vdr3} // k = 3
	alpha := 3

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	log := logging.NoLog{}
	registerer := prometheus.NewRegistry()
	s, err := NewSet(factory, log, registerer)
	require.NoError(err)

	// create two polls for the two blocks
	vdrBag := bag.Of(vdrs...)
	require.True(s.Add(1, vdrBag))

	vdrBag = bag.Of(vdrs...)
	require.True(s.Add(2, vdrBag))
	require.Equal(2, s.Len())

	// vote out of order
	require.Empty(s.Vote(1, vdr1, blkID1))
	require.Empty(s.Vote(2, vdr2, blkID2))
	require.Empty(s.Vote(2, vdr3, blkID2))

	// poll 2 finished
	require.Empty(s.Vote(2, vdr1, blkID2)) // expect 2 to not have finished because 1 is still pending

	require.Empty(s.Vote(1, vdr2, blkID1))

	results := s.Vote(1, vdr3, blkID1) // poll 1 finished, poll 2 should be finished as well
	require.Len(results, 2)
	require.Equal(blkID1, results[0].List()[0])
	require.Equal(blkID2, results[1].List()[0])
}

func TestCreateAndFinishPollOutOfOrder_OlderFinishesFirst(t *testing.T) {
	require := require.New(t)

	vdrs := []ids.NodeID{vdr1, vdr2, vdr3} // k = 3
	alpha := 3

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	log := logging.NoLog{}
	registerer := prometheus.NewRegistry()
	s, err := NewSet(factory, log, registerer)
	require.NoError(err)

	// create two polls for the two blocks
	vdrBag := bag.Of(vdrs...)
	require.True(s.Add(1, vdrBag))

	vdrBag = bag.Of(vdrs...)
	require.True(s.Add(2, vdrBag))
	require.Equal(2, s.Len())

	// vote out of order
	require.Empty(s.Vote(1, vdr1, blkID1))
	require.Empty(s.Vote(2, vdr2, blkID2))
	require.Empty(s.Vote(2, vdr3, blkID2))

	require.Empty(s.Vote(1, vdr2, blkID1))

	results := s.Vote(1, vdr3, blkID1) // poll 1 finished, poll 2 still remaining
	require.Len(results, 1)            // because 1 is the oldest
	require.Equal(blkID1, results[0].List()[0])

	results = s.Vote(2, vdr1, blkID2) // poll 2 finished
	require.Len(results, 1)           // because 2 is the oldest now
	require.Equal(blkID2, results[0].List()[0])
}

func TestCreateAndFinishPollOutOfOrder_UnfinishedPollsGaps(t *testing.T) {
	require := require.New(t)

	vdrs := []ids.NodeID{vdr1, vdr2, vdr3} // k = 3
	alpha := 3

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	log := logging.NoLog{}
	registerer := prometheus.NewRegistry()
	s, err := NewSet(factory, log, registerer)
	require.NoError(err)

	// create three polls for the two blocks
	vdrBag := bag.Of(vdrs...)
	require.True(s.Add(1, vdrBag))

	vdrBag = bag.Of(vdrs...)
	require.True(s.Add(2, vdrBag))

	vdrBag = bag.Of(vdrs...)
	require.True(s.Add(3, vdrBag))
	require.Equal(3, s.Len())

	// vote out of order
	// 2 finishes first to create a gap of finished poll between two unfinished polls 1 and 3
	require.Empty(s.Vote(2, vdr3, blkID2))
	require.Empty(s.Vote(2, vdr2, blkID2))
	require.Empty(s.Vote(2, vdr1, blkID2))

	// 3 finishes now, 2 has already finished but 1 is not finished so we expect to receive no results still
	require.Empty(s.Vote(3, vdr2, blkID3))
	require.Empty(s.Vote(3, vdr3, blkID3))
	require.Empty(s.Vote(3, vdr1, blkID3))

	// 1 finishes now, 2 and 3 have already finished so we expect 3 items in results
	require.Empty(s.Vote(1, vdr1, blkID1))
	require.Empty(s.Vote(1, vdr2, blkID1))
	results := s.Vote(1, vdr3, blkID1)
	require.Len(results, 3)
	require.Equal(blkID1, results[0].List()[0])
	require.Equal(blkID2, results[1].List()[0])
	require.Equal(blkID3, results[2].List()[0])
}

func TestCreateAndFinishSuccessfulPoll(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2) // k = 2
	alpha := 2

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	log := logging.NoLog{}
	registerer := prometheus.NewRegistry()
	s, err := NewSet(factory, log, registerer)
	require.NoError(err)

	require.Zero(s.Len())

	require.True(s.Add(0, vdrs))
	require.Equal(1, s.Len())

	require.False(s.Add(0, vdrs))
	require.Equal(1, s.Len())

	require.Empty(s.Vote(1, vdr1, blkID1))
	require.Empty(s.Vote(0, vdr1, blkID1))
	require.Empty(s.Vote(0, vdr1, blkID1))

	results := s.Vote(0, vdr2, blkID1)
	require.Len(results, 1)
	list := results[0].List()
	require.Len(list, 1)
	require.Equal(blkID1, list[0])
	require.Equal(2, results[0].Count(blkID1))
}

func TestCreateAndFinishFailedPoll(t *testing.T) {
	require := require.New(t)

	vdrs := bag.Of(vdr1, vdr2) // k = 2
	alpha := 1

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	log := logging.NoLog{}
	registerer := prometheus.NewRegistry()
	s, err := NewSet(factory, log, registerer)
	require.NoError(err)

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

	vdrs := bag.Of(vdr1) // k = 1
	alpha := 1

	factory := newEarlyTermNoTraversalTestFactory(require, alpha)
	log := logging.NoLog{}
	registerer := prometheus.NewRegistry()
	s, err := NewSet(factory, log, registerer)
	require.NoError(err)

	expected := `current polls: (Size = 1)
    RequestID 0:
        waiting on Bag[ids.NodeID]: (Size = 1)
            NodeID-6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt: 1
        received Bag[ids.ID]: (Size = 0)`
	require.True(s.Add(0, vdrs))
	require.Equal(expected, s.String())
}
