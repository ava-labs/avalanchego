// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

func TestCodeQueue(t *testing.T) {
	hashes := make([]common.Hash, 256)
	for i := range hashes {
		hashes[i] = crypto.Keccak256Hash([]byte{byte(i)})
	}

	tests := []struct {
		name                      string
		alreadyToFetch            set.Set[common.Hash]
		alreadyHave               map[common.Hash][]byte
		addCode                   [][]common.Hash
		want                      []common.Hash
		shutdownInsteadOfFinalize bool
		addCodeAfter              []common.Hash
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
			// Deduplication is the consumer's responsibility, not the queue's.
			addCode: [][]common.Hash{{hashes[42]}},
			want:    []common.Hash{hashes[42]},
		},
		{
			name:                      "external_shutdown",
			shutdownInsteadOfFinalize: true,
			addCodeAfter:              []common.Hash{hashes[11]},
			want:                      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

			db := rawdb.NewMemoryDatabase()
			for hash, code := range tt.alreadyHave {
				rawdb.WriteCode(db, hash, code)
			}
			for hash := range tt.alreadyToFetch {
				require.NoError(t, customrawdb.WriteCodeToFetch(db, hash))
			}

			q, err := NewQueue(db)
			require.NoError(t, err, "NewQueue()")

			recvDone := make(chan struct{})
			go func() {
				for _, add := range tt.addCode {
					require.NoErrorf(t, q.AddCode(t.Context(), add), "%T.AddCode(%v)", q, add)
				}

				if tt.shutdownInsteadOfFinalize {
					q.Shutdown()
					<-recvDone
					err := q.AddCode(t.Context(), tt.addCodeAfter)
					require.ErrorIsf(t, err, ErrQueueClosed, "%T.AddCode() after Shutdown", q)
				} else {
					require.NoErrorf(t, q.Finalize(), "%T.Finalize()", q)
				}
			}()

			var got []common.Hash
			for h := range q.CodeHashes() {
				got = append(got, h)
			}
			close(recvDone)
			// Cross-batch ordering is not guaranteed because separate
			// goroutines race. Compare as sets.
			require.ElementsMatchf(t, tt.want, got, "values received from %T.CodeHashes()", q)

			t.Run("restart_with_same_db", func(t *testing.T) {
				q, err := NewQueue(db, WithCapacity(len(tt.want)))
				require.NoError(t, err, "NewQueue([reused db])")
				require.NoErrorf(t, q.Finalize(), "%T.Finalize() immediately after creation", q)

				got := make(set.Set[common.Hash])
				for h := range q.CodeHashes() {
					got.Add(h)
				}

				// init checks for existing code when recovering from disk,
				// so already-present hashes are excluded.
				want := set.Of(tt.want...)
				for hash := range tt.alreadyHave {
					want.Remove(hash)
				}

				require.ElementsMatchf(t, want.List(), got.List(), "All received on %T.CodeHashes() after restart", q)
			})
		})
	}
}

// TestFinalizeFlushesAllHashes verifies that AddCode is non-blocking and
// Finalize waits for all background sends to complete.
func TestFinalizeFlushesAllHashes(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	const (
		capacity  = 1
		numHashes = 50
	)
	db := rawdb.NewMemoryDatabase()
	q, err := NewQueue(db, WithCapacity(capacity))
	require.NoError(t, err)

	hashes := makeHashes(numHashes)

	// AddCode returns immediately despite capacity=1.
	require.NoError(t, q.AddCode(t.Context(), hashes))

	var got []common.Hash
	go func() {
		require.NoError(t, q.Finalize())
	}()
	for h := range q.CodeHashes() {
		got = append(got, h)
	}

	require.Equal(t, hashes, got, "all hashes received in batch order")
}

// TestShutdownUnblocksGoroutines verifies that Shutdown cancels stuck
// goroutines, is idempotent with Finalize, and rejects later AddCode calls.
func TestShutdownUnblocksGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	const capacity = 1
	db := rawdb.NewMemoryDatabase()
	q, err := NewQueue(db, WithCapacity(capacity))
	require.NoError(t, err)

	// Goroutines will block on send because capacity=1 and no consumer.
	require.NoError(t, q.AddCode(t.Context(), makeHashes(100)))

	q.Shutdown()

	// Drain any items buffered before cancel.
	for range q.CodeHashes() {
	}

	// Finalize after Shutdown must not panic.
	require.NoError(t, q.Finalize())

	// AddCode after Shutdown must return ErrQueueClosed.
	err = q.AddCode(t.Context(), []common.Hash{{}})
	require.ErrorIs(t, err, ErrQueueClosed)
}

// TestShutdownAndAddCodeRace verifies no panic or goroutine leak when
// Shutdown and AddCode race against each other.
func TestShutdownAndAddCodeRace(t *testing.T) {
	for range 10_000 {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			q, err := NewQueue(rawdb.NewMemoryDatabase())
			require.NoError(t, err)

			var (
				ready    sync.WaitGroup
				finished sync.WaitGroup
			)
			ready.Add(2)
			finished.Add(2)
			start := make(chan struct{})

			go func() {
				defer finished.Done()
				ready.Done()
				<-start
				q.Shutdown()
			}()

			go func() {
				defer finished.Done()
				ready.Done()
				<-start
				// May succeed or return ErrQueueClosed depending on timing.
				_ = q.AddCode(t.Context(), []common.Hash{{}})
			}()

			ready.Wait()
			close(start)
			finished.Wait()
		})
	}
}

// TestConcurrentAddCodeAndConsume stress-tests concurrent producers and a
// single consumer on a small-capacity channel.
func TestConcurrentAddCodeAndConsume(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	const (
		numProducers      = 5
		hashesPerProducer = 100
		capacity          = 2
	)
	db := rawdb.NewMemoryDatabase()
	q, err := NewQueue(db, WithCapacity(capacity))
	require.NoError(t, err)

	var producerWg sync.WaitGroup
	producerWg.Add(numProducers)
	for i := range numProducers {
		go func() {
			defer producerWg.Done()
			hashes := make([]common.Hash, hashesPerProducer)
			for j := range hashes {
				hashes[j] = crypto.Keccak256Hash([]byte{byte(i), byte(j)})
			}
			require.NoError(t, q.AddCode(t.Context(), hashes))
		}()
	}

	var got []common.Hash
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for h := range q.CodeHashes() {
			got = append(got, h)
		}
	}()

	producerWg.Wait()
	require.NoError(t, q.Finalize())
	<-consumerDone

	require.Len(t, got, numProducers*hashesPerProducer)
}

func makeHashes(n int) []common.Hash {
	hashes := make([]common.Hash, n)
	for i := range hashes {
		hashes[i] = crypto.Keccak256Hash([]byte(fmt.Sprintf("%d", i)))
	}
	return hashes
}
