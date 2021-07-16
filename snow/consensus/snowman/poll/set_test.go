// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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

func TestCreateAndFinishPollOutOfOrder(t *testing.T) {
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
	vdrBag := ids.ShortBag{}
	vdrBag.Add(vdrs...)

	// create two polls for the two vtxs
	added := s.Add(1, vdrBag)
	assert.True(t, added)

	// re init for second poll - why? 🤔
	vdrBag = ids.ShortBag{}
	vdrBag.Add(vdrs...)
	added = s.Add(2, vdrBag)
	assert.True(t, added)
	assert.Equal(t, s.Len(), 2)

	// vote vtx1 for poll 1
	// vote vtx2 for poll 2
	vtx1 := ids.ID{1}
	vtx2 := ids.ID{2}

	// vote out of order
	_, finished := s.Vote(1, vdr1, vtx1)
	assert.False(t, finished)
	_, finished = s.Vote(2, vdr2, vtx2)
	assert.False(t, finished)
	_, finished = s.Vote(2, vdr3, vtx2)
	assert.False(t, finished)

	result, finished := s.Vote(2, vdr1, vtx2) // poll 2 finished
	assert.True(t, finished)
	assert.Equal(t, result.List()[0], vtx2)

	_, finished = s.Vote(1, vdr2, vtx1)
	assert.False(t, finished)
	result, finished = s.Vote(1, vdr3, vtx1) // poll 1 finished
	assert.True(t, finished)
	assert.Equal(t, result.List()[0], vtx1)
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
	} else if _, finished := s.Vote(1, vdr1, vtxID); finished {
		t.Fatalf("Shouldn't have been able to finish a non-existent poll")
	} else if _, finished := s.Vote(0, vdr1, vtxID); finished {
		t.Fatalf("Shouldn't have been able to finish an ongoing poll")
	} else if _, finished := s.Vote(0, vdr1, vtxID); finished {
		t.Fatalf("Should have dropped a duplicated poll")
	} else if result, finished := s.Vote(0, vdr2, vtxID); !finished {
		t.Fatalf("Should have finished the")
	} else if list := result.List(); len(list) != 1 {
		t.Fatalf("Wrong number of vertices returned")
	} else if retVtxID := list[0]; retVtxID != vtxID {
		t.Fatalf("Wrong vertex returned")
	} else if result.Count(vtxID) != 2 {
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
	} else if _, finished := s.Drop(1, vdr1); finished {
		t.Fatalf("Shouldn't have been able to finish a non-existent poll")
	} else if _, finished := s.Drop(0, vdr1); finished {
		t.Fatalf("Shouldn't have been able to finish an ongoing poll")
	} else if _, finished := s.Drop(0, vdr1); finished {
		t.Fatalf("Should have dropped a duplicated poll")
	} else if result, finished := s.Drop(0, vdr2); !finished {
		t.Fatalf("Should have finished the")
	} else if list := result.List(); len(list) != 0 {
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

	expected := "current polls: (Size = 1)\n" +
		"    0: waiting on Bag: (Size = 1)\n" +
		"        ID[6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt]: Count = 1"
	if !s.Add(0, vdrs) {
		t.Fatalf("Should have been able to add a new poll")
	} else if str := s.String(); expected != str {
		t.Fatalf("Set return wrong string, Expected:\n%s\nReturned:\n%s",
			expected,
			str)
	}
}
