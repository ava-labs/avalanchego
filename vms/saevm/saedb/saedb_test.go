// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import "testing"

func FuzzTrieDBCommitHeights(f *testing.F) {
	f.Fuzz(func(t *testing.T, e uint64) {
		// Don't include testing for archival mode (committing at every height).
		e = max(e, 2)
		// Avoid iterating over very large spaces.
		e = min(e, 100_000)

		for num, want := range map[uint64]bool{
			e - 1:   false,
			e:       true,
			e + 1:   false,
			2*e - 1: false,
			2 * e:   true,
			2*e + 1: false,
		} {
			if got := ShouldCommitTrieDB(num, e); got != want {
				t.Errorf("ShouldCommitTrieDB(%d, %d) got %t want %t", num, e, got, want)
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
			if got := LastCommittedTrieDBHeight(num, e); got != want {
				t.Errorf("LastCommittedTrieDBHeight(%d, %d) got %d; want %d", num, e, got, want)
			}
		}

		var last uint64
		for num := range 20 * e {
			if ShouldCommitTrieDB(num, e) {
				last = num
			}
			if got, want := LastCommittedTrieDBHeight(num, e), last; got != want {
				t.Errorf("LastCommittedTrieDBHeight(%d, %d) got %d; want %d", num, e, got, want)
			}
		}
	})
}
