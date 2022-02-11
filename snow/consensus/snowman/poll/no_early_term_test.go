// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestNoEarlyTermResults(t *testing.T) {
	vtxID := ids.ID{1}

	vdr1 := ids.ShortID{1} // k = 1

	vdrs := ids.ShortBag{}
	vdrs.Add(vdr1)

	factory := NewNoEarlyTermFactory()
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)
	if !poll.Finished() {
		t.Fatalf("Poll did not terminate after receiving k votes")
	}

	result := poll.Result()
	if list := result.List(); len(list) != 1 {
		t.Fatalf("Wrong number of vertices returned")
	} else if retVtxID := list[0]; retVtxID != vtxID {
		t.Fatalf("Wrong vertex returned")
	} else if result.Count(vtxID) != 1 {
		t.Fatalf("Wrong number of votes returned")
	}
}

func TestNoEarlyTermString(t *testing.T) {
	vtxID := ids.ID{1}

	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2} // k = 2

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	factory := NewNoEarlyTermFactory()
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)

	expected := `waiting on Bag: (Size = 1)
    ID[BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp]: Count = 1
received Bag: (Size = 1)
    ID[SYXsAycDPUu4z2ZksJD5fh5nTDcH3vCFHnpcVye5XuJ2jArg]: Count = 1`
	if result := poll.String(); expected != result {
		t.Fatalf("Poll should have returned %s but returned %s", expected, result)
	}
}

func TestNoEarlyTermDropsDuplicatedVotes(t *testing.T) {
	vtxID := ids.ID{1}

	vdr1 := ids.ShortID{1}
	vdr2 := ids.ShortID{2} // k = 2

	vdrs := ids.ShortBag{}
	vdrs.Add(
		vdr1,
		vdr2,
	)

	factory := NewNoEarlyTermFactory()
	poll := factory.New(vdrs)

	poll.Vote(vdr1, vtxID)
	if poll.Finished() {
		t.Fatalf("Poll finished after less than alpha votes")
	}
	poll.Vote(vdr1, vtxID)
	if poll.Finished() {
		t.Fatalf("Poll finished after getting a duplicated vote")
	}
	poll.Drop(vdr1)
	if poll.Finished() {
		t.Fatalf("Poll finished after getting a duplicated vote")
	}
	poll.Vote(vdr2, vtxID)
	if !poll.Finished() {
		t.Fatalf("Poll did not terminate after receiving k votes")
	}
}
