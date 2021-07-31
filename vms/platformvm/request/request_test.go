package request

import (
	"math/rand"
	"testing"
	"time"
)

func TestMempool_AppRequestIDHandling(t *testing.T) {
	// show that multiple outstanding requests can be issued, with increasing reqID
	// response can arrive in any order. Requests with unknown reqID are dropped

	reqIDHandler := NewHandler()

	// show that reqID increase starting from zero
	issueRounds := []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for _, i := range issueRounds {
		currentID := reqIDHandler.IssueID()
		if currentID != i {
			t.Fatal("unexpected reqID")
		}
	}

	// show that unknown reqID results in error
	unknownID := uint32(len(issueRounds)) + 1
	if err := reqIDHandler.ReclaimID(unknownID); err == nil {
		t.Fatalf("should not be possible to consume unknownID")
	}

	// show response can come in any order
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(issueRounds), func(i, j int) {
		issueRounds[i], issueRounds[j] = issueRounds[j], issueRounds[i]
	})

	for it, i := range issueRounds {
		if err := reqIDHandler.ReclaimID(i); err != nil {
			t.Fatalf("consume: err %v at iteration %d, index %d, permutation %v", err, it, i, issueRounds)
		}

		// re-consuming same reqID should fail
		if err := reqIDHandler.ReclaimID(i); err == nil {
			t.Fatalf("re-consume: err %v at iteration %d, index %d, permutation %v", err, it, i, issueRounds)
		}
	}

	// show that consumed reqIDs are recycled
	firstID := reqIDHandler.IssueID()
	secondID := reqIDHandler.IssueID()
	thirdID := reqIDHandler.IssueID()

	if err := reqIDHandler.ReclaimID(secondID); err != nil {
		t.Fatalf("could not consume secondID")
	}
	if err := reqIDHandler.ReclaimID(thirdID); err != nil {
		t.Fatalf("could not consume thirdID")
	}

	fourthID := reqIDHandler.IssueID()
	if fourthID != secondID {
		t.Fatalf("could not reuse consumed ID")
	}

	if err := reqIDHandler.ReclaimID(fourthID); err != nil {
		t.Fatalf("could not consume fourthID")
	}
	if err := reqIDHandler.ReclaimID(firstID); err != nil {
		t.Fatalf("could not consume firstID")
	}

	if lastID := reqIDHandler.IssueID(); lastID != firstID {
		t.Fatalf("could not reuse consumed ID")
	}
}
