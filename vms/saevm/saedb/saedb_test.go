// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"testing"
)

func TestTrieDBCommitHeights(t *testing.T) {
	const e = CommitTrieDBEvery

	for num, want := range map[uint64]bool{
		e - 1:   false,
		e:       true,
		e + 1:   false,
		2*e - 1: false,
		2 * e:   true,
		2*e + 1: false,
	} {
		if got := ShouldCommitTrieDB(num); got != want {
			t.Errorf("CommitTrieDB(%d) got %t want %t", num, got, want)
		}
	}

	for num, want := range map[uint64]uint64{
		0:       0,
		e - 1:   0,
		e:       e,
		e + 1:   e,
		2*e - 1: e,
		2 * e:   2 * e,
		2*e + 1: 2 * e,
		3*e - 1: 2 * e,
	} {
		if got := LastCommittedTrieDBHeight(num); got != want {
			t.Errorf("LastCommitedTrieDBHeight(%d) got %d; want %d", num, got, want)
		}
	}

	var last uint64
	for num := range uint64(20 * e) {
		if ShouldCommitTrieDB(num) {
			last = num
		}
		if got, want := LastCommittedTrieDBHeight(num), last; got != want {
			t.Errorf("LastCommitedTrieDBHeight(%d) got %d; want %d", num, got, want)
		}
	}
}
