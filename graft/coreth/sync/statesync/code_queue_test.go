// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
)

func TestCodeQueue(t *testing.T) {
	hashes := make([]common.Hash, 256)
	for i := range hashes {
		hashes[i] = crypto.Keccak256Hash([]byte{byte(i)})
	}

	tests := []struct {
		name                  string
		alreadyToFetch        set.Set[common.Hash]
		alreadyHave           map[common.Hash][]byte
		addCode               [][]common.Hash
		want                  []common.Hash
		quitInsteadOfFinalize bool
		addCodeAfter          []common.Hash
	}{
		{
			name: "multiple_calls_to_addcode",
			addCode: [][]common.Hash{
				hashes[:20],
				hashes[20:35],
				hashes[35:42],
				hashes[42:],
			},
			want: hashes,
		},
		{
			name:    "allow_duplicates",
			addCode: [][]common.Hash{{hashes[0], hashes[0]}},
			want:    []common.Hash{hashes[0], hashes[0]},
		},
		{
			name:    "AddCode_empty",
			addCode: [][]common.Hash{{}},
			want:    nil,
		},
		{
			name:           "init_resumes_from_db",
			alreadyToFetch: set.Of(hashes[1]),
			want:           []common.Hash{hashes[1]},
		},
		{
			name:        "deduplication_in_consumer",
			alreadyHave: map[common.Hash][]byte{hashes[42]: {42}},
			// It is the consumer's responsibility, not the queue's, to check
			// the database.
			addCode: [][]common.Hash{{hashes[42]}},
			want:    []common.Hash{hashes[42]},
		},
		{
			name:                  "external_shutdown_via_quit_channel",
			quitInsteadOfFinalize: true,
			addCodeAfter:          []common.Hash{hashes[11]},
			want:                  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

			db := rawdb.NewMemoryDatabase()
			for hash, code := range tt.alreadyHave {
				rawdb.WriteCode(db, hash, code)
			}
			for hash := range tt.alreadyToFetch {
				customrawdb.AddCodeToFetch(db, hash)
			}

			quit := make(chan struct{})
			q, err := NewCodeQueue(db, quit)
			require.NoError(t, err, "NewCodeQueue()")

			recvDone := make(chan struct{})
			go func() {
				for _, add := range tt.addCode {
					require.NoErrorf(t, q.AddCode(add), "%T.AddCode(%v)", q, add)
				}

				if tt.quitInsteadOfFinalize {
					close(quit)
					<-recvDone
					require.ErrorIsf(t, q.AddCode(tt.addCodeAfter), errFailedToAddCodeHashesToQueue, "%T.AddCode() after `quit` channel closed", q)
				} else {
					require.NoErrorf(t, q.Finalize(), "%T.Finalize()", q)
					// Avoid leaking the internal goroutine
					close(quit)
				}
			}()

			var got []common.Hash
			for h := range q.CodeHashes() {
				got = append(got, h)
			}
			close(recvDone)
			require.Emptyf(t, cmp.Diff(tt.want, got), "Diff (-want +got) of values received from %T.CodeHashes()", q)

			t.Run("restart_with_same_db", func(t *testing.T) {
				q, err := NewCodeQueue(db, nil, WithCapacity(len(tt.want)))
				require.NoError(t, err, "NewCodeQueue([reused db])")
				require.NoErrorf(t, q.Finalize(), "%T.Finalize() immediately after creation", q)

				got := make(set.Set[common.Hash])
				for h := range q.CodeHashes() {
					got.Add(h)
				}

				// Unlike newly added code hashes, the initialisation function
				// checks for existing code when recovering from the database.
				// The order can't be maintained.
				want := set.Of(tt.want...)
				for hash := range tt.alreadyHave {
					want.Remove(hash)
				}

				require.ElementsMatchf(t, want.List(), got.List(), "All received on %T.CodeHashes() after restart", q)
			})
		})
	}
}

// Test that Finalize waits for in-flight AddCode calls to complete before closing the channel.
func TestCodeQueue_FinalizeWaitsForInflightAddCodeCalls(t *testing.T) {
	const capacity = 1
	db := rawdb.NewMemoryDatabase()
	q, err := NewCodeQueue(db, nil, WithCapacity(capacity))
	require.NoError(t, err, "NewCodeQueue()")

	// Prepare multiple distinct hashes to exceed the buffer and cause AddCode to block on enqueue.
	hashes := make([]common.Hash, capacity+2)
	for i := range hashes {
		hashes[i] = crypto.Keccak256Hash([]byte(fmt.Sprintf("code-%d", i)))
	}

	addDone := make(chan error, 1)
	go func() {
		addDone <- q.AddCode(hashes)
	}()

	// Read the first enqueued hash to ensure AddCode is actively enqueuing and will block on the next send.
	var got []common.Hash //nolint:prealloc
	got = append(got, <-q.CodeHashes())

	// Call Finalize concurrently - it should block until AddCode returns.
	finalized := make(chan struct{})
	go func() {
		require.NoError(t, q.Finalize(), "Finalize()")
		close(finalized)
	}()

	// Finalize should not complete yet because AddCode is still enqueuing (buffer=1 and we haven't drained).
	select {
	case <-finalized:
		t.Fatal("Finalize returned before in-flight AddCode completed")
	case <-addDone:
		t.Fatal("AddCode returned before enqueuing all hashes")
	case <-time.After(100 * time.Millisecond):
		// TODO(powerslider) once we're using Go 1.25 and the `synctest` package
		// is generally available, use it here instead of an arbitrary amount of
		// time. Without this, we have no way to guarantee that Finalize() and
		// AddCode() are actually blocked.
	}

	// Drain remaining enqueued hashes; this will unblock AddCode so it can finish.
	for h := range q.CodeHashes() {
		got = append(got, h)
	}
	require.Equal(t, hashes, got)

	// Now AddCode should complete without error, and Finalize should return and close the channel.
	require.NoError(t, <-addDone, "AddCode()")
	<-finalized
}
