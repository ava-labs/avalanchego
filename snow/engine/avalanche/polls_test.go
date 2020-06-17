package avalanche

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestPollTerminatesEarlyVirtuousCase(t *testing.T) {
	alpha := 3

	vtxID := GenerateID()
	votes := []ids.ID{vtxID}

	vdr1 := ids.NewShortID([20]byte{1})
	vdr2 := ids.NewShortID([20]byte{2})
	vdr3 := ids.NewShortID([20]byte{3})
	vdr4 := ids.NewShortID([20]byte{4})
	vdr5 := ids.NewShortID([20]byte{5}) // k = 5

	vdrs := ids.ShortSet{}
	vdrs.Add(vdr1)
	vdrs.Add(vdr2)
	vdrs.Add(vdr3)
	vdrs.Add(vdr4)
	vdrs.Add(vdr5)

	poll := poll{
		votes:  make(ids.UniqueBag),
		polled: vdrs,
		alpha:  alpha,
	}

	poll.Vote(votes, vdr1)
	if poll.Finished() {
		t.Fatalf("Poll finished after less than alpha votes")
	}
	poll.Vote(votes, vdr2)
	if poll.Finished() {
		t.Fatalf("Poll finished after less than alpha votes")
	}
	poll.Vote(votes, vdr3)
	if !poll.Finished() {
		t.Fatalf("Poll did not terminate early after receiving alpha votes for one vertex and none for other vertices")
	}
}

func TestPollAccountsForSharedAncestor(t *testing.T) {
	alpha := 4

	vtxA := GenerateID()
	vtxB := GenerateID()
	vtxC := GenerateID()
	vtxD := GenerateID()

	// If validators 1-3 vote for frontier vertices
	// B, C, and D respectively, which all share the common ancestor
	// A, then we cannot terminate early with alpha = k = 4
	// If the final vote is cast for any of A, B, C, or D, then
	// vertex A will have transitively received alpha = 4 votes
	vdr1 := ids.NewShortID([20]byte{1})
	vdr2 := ids.NewShortID([20]byte{2})
	vdr3 := ids.NewShortID([20]byte{3})
	vdr4 := ids.NewShortID([20]byte{4})

	vdrs := ids.ShortSet{}
	vdrs.Add(vdr1)
	vdrs.Add(vdr2)
	vdrs.Add(vdr3)
	vdrs.Add(vdr4)

	poll := poll{
		votes:  make(ids.UniqueBag),
		polled: vdrs,
		alpha:  alpha,
	}

	votes1 := []ids.ID{vtxB}
	poll.Vote(votes1, vdr1)
	if poll.Finished() {
		t.Fatalf("Poll finished early after receiving one vote")
	}
	votes2 := []ids.ID{vtxC}
	poll.Vote(votes2, vdr2)
	if poll.Finished() {
		t.Fatalf("Poll finished early after receiving two votes")
	}
	votes3 := []ids.ID{vtxD}
	poll.Vote(votes3, vdr3)
	if poll.Finished() {
		t.Fatalf("Poll terminated early, when a shared ancestor could have received alpha votes")
	}

	votes4 := []ids.ID{vtxA}
	poll.Vote(votes4, vdr4)
	if !poll.Finished() {
		t.Fatalf("Poll did not terminate after receiving all outstanding votes")
	}
}
