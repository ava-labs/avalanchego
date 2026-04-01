package draftreview

import "github.com/ava-labs/avalanchego/tests/fixture/stacktrace"

func FindPendingReviewForAuthor(reviews []Review, login string) (Review, bool) {
	for _, review := range reviews {
		if review.User.Login == login && review.State == reviewStatePending {
			return review, true
		}
	}
	return Review{}, false
}

func EnsureNoPendingReviewForAuthor(reviews []Review, login string) error {
	if review, found := FindPendingReviewForAuthor(reviews, login); found {
		return stacktrace.Errorf("refusing to create a new pending review because %s already has pending review %d", login, review.ID)
	}
	return nil
}
