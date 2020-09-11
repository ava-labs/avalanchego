// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/ava-labs/avalanche-go/ids"
)

func TestEarlyTermNoTraversalResults(t *testing.T) {
	alpha := 1

	vtxID := ids.NewID([32]byte{1})
	votes := []ids.ID{vtxID}

	vdr1 := ids.NewShortID([20]byte{1}) // k = 1

	vdrs := ids.ShortBag{}
	vdrs.Add(vdr1)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, votes)
	if !poll.Finished() {
		t.Fatalf("Poll did not terminate after receiving k votes")
	}

	result := poll.Result()
	if list := result.List(); len(list) != 1 {
		t.Fatalf("Wrong number of vertices returned")
	} else if retVtxID := list[0]; !retVtxID.Equals(vtxID) {
		t.Fatalf("Wrong vertex returned")
	} else if set := result.GetSet(vtxID); set.Len() != 1 {
		t.Fatalf("Wrong number of votes returned")
	}
}

func TestEarlyTermNoTraversalString(t *testing.T) {
	alpha := 2

	vtxID := ids.NewID([32]byte{1})
	votes := []ids.ID{vtxID}

	vdr1 := ids.NewShortID([20]byte{1})
	vdr2 := ids.NewShortID([20]byte{2}) // k = 2

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, votes)

	expected := "waiting on Bag: (Size = 1)\n" +
		"    ID[BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp]: Count = 1"
	if result := poll.String(); expected != result {
		t.Fatalf("Poll should have returned %s but returned %s", expected, result)
	}
}

func TestEarlyTermNoTraversalDropsDuplicatedVotes(t *testing.T) {
	alpha := 2

	vtxID := ids.NewID([32]byte{1})
	votes := []ids.ID{vtxID}

	vdr1 := ids.NewShortID([20]byte{1})
	vdr2 := ids.NewShortID([20]byte{2}) // k = 2

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, votes)
	if poll.Finished() {
		t.Fatalf("Poll finished after less than alpha votes")
	}
	poll.Vote(vdr1, votes)
	if poll.Finished() {
		t.Fatalf("Poll finished after getting a duplicated vote")
	}
	poll.Vote(vdr2, votes)
	if !poll.Finished() {
		t.Fatalf("Poll did not terminate after receiving k votes")
	}
}

func TestEarlyTermNoTraversalTerminatesEarly(t *testing.T) {
	alpha := 3

	vtxID := ids.NewID([32]byte{1})
	votes := []ids.ID{vtxID}

	vdr1 := ids.NewShortID([20]byte{1})
	vdr2 := ids.NewShortID([20]byte{2})
	vdr3 := ids.NewShortID([20]byte{3})
	vdr4 := ids.NewShortID([20]byte{4})
	vdr5 := ids.NewShortID([20]byte{5}) // k = 5

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
		vdr3,
		vdr4,
		vdr5,
	)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, votes)
	if poll.Finished() {
		t.Fatalf("Poll finished after less than alpha votes")
	}
	poll.Vote(vdr2, votes)
	if poll.Finished() {
		t.Fatalf("Poll finished after less than alpha votes")
	}
	poll.Vote(vdr3, votes)
	if !poll.Finished() {
		t.Fatalf("Poll did not terminate early after receiving alpha votes for one vertex and none for other vertices")
	}
}

func TestEarlyTermNoTraversalForSharedAncestor(t *testing.T) {
	alpha := 4

	vtxA := ids.NewID([32]byte{1})
	vtxB := ids.NewID([32]byte{2})
	vtxC := ids.NewID([32]byte{3})
	vtxD := ids.NewID([32]byte{4})

	// If validators 1-3 vote for frontier vertices
	// B, C, and D respectively, which all share the common ancestor
	// A, then we cannot terminate early with alpha = k = 4
	// If the final vote is cast for any of A, B, C, or D, then
	// vertex A will have transitively received alpha = 4 votes
	vdr1 := ids.NewShortID([20]byte{1})
	vdr2 := ids.NewShortID([20]byte{2})
	vdr3 := ids.NewShortID([20]byte{3})
	vdr4 := ids.NewShortID([20]byte{4})

	vdrs := ids.ShortBag{}
	vdrs.Add(vdr1)
	vdrs.Add(vdr2)
	vdrs.Add(vdr3)
	vdrs.Add(vdr4)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, []ids.ID{vtxB})
	if poll.Finished() {
		t.Fatalf("Poll finished early after receiving one vote")
	}
	poll.Vote(vdr2, []ids.ID{vtxC})
	if poll.Finished() {
		t.Fatalf("Poll finished early after receiving two votes")
	}
	poll.Vote(vdr3, []ids.ID{vtxD})
	if poll.Finished() {
		t.Fatalf("Poll terminated early, when a shared ancestor could have received alpha votes")
	}
	poll.Vote(vdr4, []ids.ID{vtxA})
	if !poll.Finished() {
		t.Fatalf("Poll did not terminate after receiving all outstanding votes")
	}
}

func TestEarlyTermNoTraversalWithFastDrops(t *testing.T) {
	alpha := 2

	vdr1 := ids.NewShortID([20]byte{1})
	vdr2 := ids.NewShortID([20]byte{2})
	vdr3 := ids.NewShortID([20]byte{3}) // k = 3

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
		vdr3,
	)

	factory := NewEarlyTermNoTraversalFactory(alpha)
	poll := factory.New(vdrs)

	poll.Vote(vdr1, nil)
	if poll.Finished() {
		t.Fatalf("Poll finished early after dropping one vote")
	}
	poll.Vote(vdr2, nil)
	if !poll.Finished() {
		t.Fatalf("Poll did not terminate after dropping two votes")
	}
}
