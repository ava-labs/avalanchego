// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func TestNewSetErrorOnMetrics(t *testing.T) {
	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(prometheus.NewCounter(prometheus.CounterOpts{
			Name: "polls",
		})),
		registerer.Register(prometheus.NewCounter(prometheus.CounterOpts{
			Name: "poll_duration",
		})),
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

	s := NewSet(factory, log, namespace, registerer)
	if s == nil {
		t.Fatalf("shouldn't have failed due to a metrics initialization err")
	}
}

func TestCreateAndFinishPollOutOfOrder_NewerFinishesFirst(t *testing.T) {
	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	// create validators
	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2}
	vdr3 := ids.ShortID{3}

	vdrs := []ids.ShortID{vdr1, vdr2, vdr3}

	// create two polls for the two vtxs
	vdrBag := ids.ShortBag{}
	vdrBag.Add(vdrs...)
	added := s.Add(1, vdrBag)
	assert.True(t, added)

	vdrBag = ids.ShortBag{}
	vdrBag.Add(vdrs...)
	added = s.Add(2, vdrBag)
	assert.True(t, added)
	assert.Equal(t, s.Len(), 2)

	// vote vtx1 for poll 1
	// vote vtx2 for poll 2
	vtx1 := ids.ID{1}
	vtx2 := ids.ID{2}

	var results []ids.Bag

	// vote out of order
	results = s.Vote(1, vdr1, vtx1)
	assert.Len(t, results, 0)
	results = s.Vote(2, vdr2, vtx2)
	assert.Len(t, results, 0)
	results = s.Vote(2, vdr3, vtx2)
	assert.Len(t, results, 0)

	results = s.Vote(2, vdr1, vtx2) // poll 2 finished
	assert.Len(t, results, 0)       // expect 2 to not have finished because 1 is still pending

	results = s.Vote(1, vdr2, vtx1)
	assert.Len(t, results, 0)

	results = s.Vote(1, vdr3, vtx1) // poll 1 finished, poll 2 should be finished as well
	assert.Len(t, results, 2)
	assert.Equal(t, vtx1, results[0].List()[0])
	assert.Equal(t, vtx2, results[1].List()[0])
}

func TestCreateAndFinishPollOutOfOrder_OlderFinishesFirst(t *testing.T) {
	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	// create validators
	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2}
	vdr3 := ids.ShortID{3}

	vdrs := []ids.ShortID{vdr1, vdr2, vdr3}

	// create two polls for the two vtxs
	vdrBag := ids.ShortBag{}
	vdrBag.Add(vdrs...)
	added := s.Add(1, vdrBag)
	assert.True(t, added)

	vdrBag = ids.ShortBag{}
	vdrBag.Add(vdrs...)
	added = s.Add(2, vdrBag)
	assert.True(t, added)
	assert.Equal(t, s.Len(), 2)

	// vote vtx1 for poll 1
	// vote vtx2 for poll 2
	vtx1 := ids.ID{1}
	vtx2 := ids.ID{2}

	var results []ids.Bag

	// vote out of order
	results = s.Vote(1, vdr1, vtx1)
	assert.Len(t, results, 0)
	results = s.Vote(2, vdr2, vtx2)
	assert.Len(t, results, 0)
	results = s.Vote(2, vdr3, vtx2)
	assert.Len(t, results, 0)

	results = s.Vote(1, vdr2, vtx1)
	assert.Len(t, results, 0)

	results = s.Vote(1, vdr3, vtx1) // poll 1 finished, poll 2 still remaining
	assert.Len(t, results, 1)       // because 1 is the oldest
	assert.Equal(t, vtx1, results[0].List()[0])

	results = s.Vote(2, vdr1, vtx2) // poll 2 finished
	assert.Len(t, results, 1)       // because 2 is the oldest now
	assert.Equal(t, vtx2, results[0].List()[0])
}

func TestCreateAndFinishPollOutOfOrder_UnfinishedPollsGaps(t *testing.T) {
	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	// create validators
	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2}
	vdr3 := ids.ShortID{3}

	vdrs := []ids.ShortID{vdr1, vdr2, vdr3}

	// create three polls for the two vtxs
	vdrBag := ids.ShortBag{}
	vdrBag.Add(vdrs...)
	added := s.Add(1, vdrBag)
	assert.True(t, added)

	vdrBag = ids.ShortBag{}
	vdrBag.Add(vdrs...)
	added = s.Add(2, vdrBag)
	assert.True(t, added)

	vdrBag = ids.ShortBag{}
	vdrBag.Add(vdrs...)
	added = s.Add(3, vdrBag)
	assert.True(t, added)
	assert.Equal(t, s.Len(), 3)

	// vote vtx1 for poll 1
	// vote vtx2 for poll 2
	// vote vtx3 for poll 3
	vtx1 := ids.ID{1}
	vtx2 := ids.ID{2}
	vtx3 := ids.ID{3}

	var results []ids.Bag

	// vote out of order
	// 2 finishes first to create a gap of finished poll between two unfinished polls 1 and 3
	results = s.Vote(2, vdr3, vtx2)
	assert.Len(t, results, 0)
	results = s.Vote(2, vdr2, vtx2)
	assert.Len(t, results, 0)
	results = s.Vote(2, vdr1, vtx2)
	assert.Len(t, results, 0)

	// 3 finishes now, 2 has already finished but 1 is not finished so we expect to receive no results still
	results = s.Vote(3, vdr2, vtx3)
	assert.Len(t, results, 0)
	results = s.Vote(3, vdr3, vtx3)
	assert.Len(t, results, 0)
	results = s.Vote(3, vdr1, vtx3)
	assert.Len(t, results, 0)

	// 1 finishes now, 2 and 3 have already finished so we expect 3 items in results
	results = s.Vote(1, vdr1, vtx1)
	assert.Len(t, results, 0)
	results = s.Vote(1, vdr2, vtx1)
	assert.Len(t, results, 0)
	results = s.Vote(1, vdr3, vtx1)
	assert.Len(t, results, 3)
	assert.Equal(t, vtx1, results[0].List()[0])
	assert.Equal(t, vtx2, results[1].List()[0])
	assert.Equal(t, vtx3, results[2].List()[0])
}

func TestCreateAndFinishSuccessfulPoll(t *testing.T) {
	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	vtxID := ids.ID{1}

	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2} // k = 2

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	if s.Len() != 0 {
		t.Fatalf("Shouldn't have any active polls yet")
	} else if !s.Add(0, vdrs) {
		t.Fatalf("Should have been able to add a new poll")
	} else if s.Len() != 1 {
		t.Fatalf("Should only have one active poll")
	} else if s.Add(0, vdrs) {
		t.Fatalf("Shouldn't have been able to add a duplicated poll")
	} else if s.Len() != 1 {
		t.Fatalf("Should only have one active poll")
	} else if results := s.Vote(1, vdr1, vtxID); len(results) > 0 {
		t.Fatalf("Shouldn't have been able to finish a non-existent poll")
	} else if results = s.Vote(0, vdr1, vtxID); len(results) > 0 {
		t.Fatalf("Shouldn't have been able to finish an ongoing poll")
	} else if results = s.Vote(0, vdr1, vtxID); len(results) > 0 {
		t.Fatalf("Should have dropped a duplicated poll")
	} else if results = s.Vote(0, vdr2, vtxID); len(results) == 0 {
		t.Fatalf("Should have finished the")
	} else if len(results) != 1 {
		t.Fatalf("Wrong number of results returned")
	} else if list := results[0].List(); len(list) != 1 {
		t.Fatalf("Wrong number of vertices returned")
	} else if retVtxID := list[0]; retVtxID != vtxID {
		t.Fatalf("Wrong vertex returned")
	} else if results[0].Count(vtxID) != 2 {
		t.Fatalf("Wrong number of votes returned")
	}
}

func TestCreateAndFinishFailedPoll(t *testing.T) {
	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2} // k = 2

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	if s.Len() != 0 {
		t.Fatalf("Shouldn't have any active polls yet")
	} else if !s.Add(0, vdrs) {
		t.Fatalf("Should have been able to add a new poll")
	} else if s.Len() != 1 {
		t.Fatalf("Should only have one active poll")
	} else if s.Add(0, vdrs) {
		t.Fatalf("Shouldn't have been able to add a duplicated poll")
	} else if s.Len() != 1 {
		t.Fatalf("Should only have one active poll")
	} else if results := s.Drop(1, vdr1); len(results) > 0 {
		t.Fatalf("Shouldn't have been able to finish a non-existent poll")
	} else if results = s.Drop(0, vdr1); len(results) > 0 {
		t.Fatalf("Shouldn't have been able to finish an ongoing poll")
	} else if results = s.Drop(0, vdr1); len(results) > 0 {
		t.Fatalf("Should have dropped a duplicated poll")
	} else if results = s.Drop(0, vdr2); len(results) == 0 {
		t.Fatalf("Should have finished the")
	} else if list := results[0].List(); len(list) != 0 {
		t.Fatalf("Wrong number of vertices returned")
	}
}

func TestSetString(t *testing.T) {
	factory := NewNoEarlyTermFactory()
	log := logging.NoLog{}
	namespace := ""
	registerer := prometheus.NewRegistry()
	s := NewSet(factory, log, namespace, registerer)

	vdr1 := ids.ShortID{1} // k = 1

	vdrs := ids.ShortBag{}
	vdrs.Add(vdr1)

	expected := `current polls: (Size = 1)
    RequestID 0:
        waiting on Bag: (Size = 1)
            ID[6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt]: Count = 1
        received Bag: (Size = 0)`
	if !s.Add(0, vdrs) {
		t.Fatalf("Should have been able to add a new poll")
	} else if str := s.String(); expected != str {
		t.Fatalf("Set return wrong string, Expected:\n%s\nReturned:\n%s",
			expected,
			str)
	}
}
