package draftreview

import "testing"

func TestFindPendingReviewForAuthor(t *testing.T) {
	t.Parallel()

	reviews := []Review{
		{ID: 1, State: "COMMENTED", User: User{Login: "maru"}},
		{ID: 2, State: reviewStatePending, User: User{Login: "someone-else"}},
		{ID: 3, State: reviewStatePending, User: User{Login: "maru"}},
	}

	review, found := FindPendingReviewForAuthor(reviews, "maru")
	if !found {
		t.Fatalf("expected to find pending review")
	}
	if review.ID != 3 {
		t.Fatalf("unexpected review id %d", review.ID)
	}
}

func TestEnsureNoPendingReviewForAuthor(t *testing.T) {
	t.Parallel()

	err := EnsureNoPendingReviewForAuthor([]Review{
		{ID: 7, State: reviewStatePending, User: User{Login: "maru"}},
	}, "maru")
	if err == nil {
		t.Fatalf("expected error")
	}
}
