// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindPendingReviewForAuthor(t *testing.T) {
	t.Parallel()

	reviews := []Review{
		{ID: 1, State: "COMMENTED", User: User{Login: "maru"}},
		{ID: 2, State: reviewStatePending, User: User{Login: "someone-else"}},
		{ID: 3, State: reviewStatePending, User: User{Login: "maru"}},
	}

	review, found := FindPendingReviewForAuthor(reviews, "maru")
	require.True(t, found)
	require.Equal(t, int64(3), review.ID)
}

func TestEnsureNoPendingReviewForAuthor(t *testing.T) {
	t.Parallel()

	err := EnsureNoPendingReviewForAuthor([]Review{
		{ID: 7, State: reviewStatePending, User: User{Login: "maru"}},
	}, "maru")
	require.EqualError(t, err, "refusing to create a new pending review because maru already has pending review 7")
}
