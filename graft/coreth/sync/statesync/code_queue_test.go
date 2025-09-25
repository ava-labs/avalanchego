// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
)

// Test that adding the same hash multiple times only enqueues once.
func TestCodeQueue_AllowsDuplicateEnqueues(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	q, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// Init first. AddCode should enqueue a single instance even with duplicates.
	codeBytes := utils.RandomBytes(32)
	codeHash := crypto.Keccak256Hash(codeBytes)

	// Enqueue with duplicates. Queue should allow duplicates and preserve order.
	// Auto-initialized in constructor.
	require.NoError(t, q.AddCode([]common.Hash{codeHash, codeHash}))

	// Should receive both duplicates.
	got1 := <-q.CodeHashes()
	got2 := <-q.CodeHashes()
	require.Equal(t, codeHash, got1)
	require.Equal(t, codeHash, got2)

	q.Finalize()
	// Channel should close without more values.
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}

func TestCodeQueue_Init_ResumeFromDB(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Persist a to-fetch marker prior to construction.
	codeBytes := utils.RandomBytes(20)
	want := crypto.Keccak256Hash(codeBytes)
	customrawdb.AddCodeToFetch(db, want)

	q, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// CodeQueue auto-inits and surfaces the pre-seeded DB marker.

	result := <-q.CodeHashes()
	require.Equal(t, want, result)

	q.Finalize()
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}

func TestCodeQueue_Init_AddCodeBlocks(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Prepare a hash that will be added via AddCode (which should block pre-Init).
	codeBytes := utils.RandomBytes(10)
	want := crypto.Keccak256Hash(codeBytes)

	q, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// With auto-init, AddCode should proceed and enqueue the hash.
	require.NoError(t, q.AddCode([]common.Hash{want}))

	result := <-q.CodeHashes()
	require.Equal(t, want, result)

	q.Finalize()
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}

func TestCodeQueue_AddCode_Empty(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	q, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// No input should enqueue nothing and return nil.
	require.NoError(t, q.AddCode(nil))

	select {
	case <-q.CodeHashes():
		t.Fatal("unexpected hash enqueued")
	default:
	}

	q.Finalize()
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}

func TestCodeQueue_AddCode_PresentCode_StillEnqueues(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	q, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// Prepare a sample hash and persist code locally to skip enqueue.
	codeBytes := utils.RandomBytes(33)
	h := crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(db, h, codeBytes)

	require.NoError(t, q.AddCode([]common.Hash{h}))

	// Queue now allows enqueuing even if code is already present; consumer will skip.
	got := <-q.CodeHashes()
	require.Equal(t, h, got)

	q.Finalize()
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}

func TestCodeQueue_AddCode_Duplicates_EnqueueBoth(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	q, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// Prepare a sample hash and submit duplicates - expect single enqueue.
	codeBytes := utils.RandomBytes(33)
	h := crypto.Keccak256Hash(codeBytes)

	require.NoError(t, q.AddCode([]common.Hash{h, h}))

	// Expect both duplicates in order.
	r1 := <-q.CodeHashes()
	r2 := <-q.CodeHashes()
	require.Equal(t, h, r1)
	require.Equal(t, h, r2)

	q.Finalize()
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}

// Test queuing several distinct code hashes at once preserves order and enqueues all.
func TestCodeQueue_AddCode_MultipleHashes(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	q, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	// Prepare several distinct code hashes.
	num := 10
	inputs := make([]common.Hash, 0, num)
	for i := 0; i < num; i++ {
		h := crypto.Keccak256Hash([]byte(fmt.Sprintf("code-%d", i)))
		inputs = append(inputs, h)
	}

	require.NoError(t, q.AddCode(inputs))

	// Drain exactly num items and assert order is preserved.
	results := make([]common.Hash, 0, num)
	for i := 0; i < num; i++ {
		results = append(results, <-q.CodeHashes())
	}
	require.Equal(t, inputs, results)

	// Ensure no unexpected extra value.
	select {
	case <-q.CodeHashes():
		t.Fatal("unexpected hash enqueued")
	default:
	}

	q.Finalize()
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}

// Test that Finalize closes the channel and no further sends happen.
func TestCodeQueue_FinalizeCloses(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	q, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	q.Finalize()
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}

// Test that shutdown during enqueue returns the expected error.
func TestCodeQueue_ShutdownDuringEnqueue(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Create a done channel we control and a very small buffer to force blocking.
	done := make(chan struct{})
	q, err := NewCodeQueue(db, done, WithCapacity(1))
	require.NoError(t, err)

	// Fill the buffer with one hash so the next enqueue would block if not for done.
	codeBytes1 := utils.RandomBytes(16)
	codeHash1 := crypto.Keccak256Hash(codeBytes1)
	require.NoError(t, q.AddCode([]common.Hash{codeHash1}))

	// Now close done to simulate shutdown while trying to enqueue another hash.
	close(done)

	codeBytes2 := utils.RandomBytes(16)
	codeHash2 := crypto.Keccak256Hash(codeBytes2)
	err = q.AddCode([]common.Hash{codeHash2})
	require.ErrorIs(t, err, errFailedToAddCodeHashesToQueue)
}

// Test that Finalize waits for in-flight AddCode calls to complete before closing the channel.
func TestCodeQueue_FinalizeWaitsForInflightAddCodeCalls(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	q, err := NewCodeQueue(db, make(chan struct{}), WithCapacity(1))
	require.NoError(t, err)

	numHashes := 3

	// Prepare multiple distinct hashes to exceed the buffer and cause AddCode to block on enqueue.
	hashes := make([]common.Hash, 0, numHashes)
	for i := 0; i < numHashes; i++ {
		hashes = append(hashes, crypto.Keccak256Hash([]byte(fmt.Sprintf("code-%d", i))))
	}

	addDone := make(chan error, 1)
	go func() {
		addDone <- q.AddCode(hashes)
	}()

	// Read the first enqueued hash to ensure AddCode is actively enqueuing and will block on the next send.
	first := <-q.CodeHashes()

	// Call Finalize concurrently - it should block until AddCode returns.
	finalized := make(chan struct{}, 1)
	go func() {
		q.Finalize()
		close(finalized)
	}()

	// Finalize should not complete yet because AddCode is still enqueuing (buffer=1 and we haven't drained).
	select {
	case <-finalized:
		t.Fatal("Finalize returned before in-flight AddCode completed")
	default:
	}

	// Drain remaining enqueued hashes; this will unblock AddCode so it can finish.
	result := make([]common.Hash, 0, numHashes)
	result = append(result, first)
	for i := 1; i < len(hashes); i++ {
		result = append(result, <-q.CodeHashes())
	}
	require.Equal(t, hashes, result)

	// Now AddCode should complete without error, and Finalize should return and close the channel.
	require.NoError(t, <-addDone)

	// Wait for finalize to complete and close the channel.
	<-finalized
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}

// Test that if hashes are added and the process shuts down abruptly, the
// "to-fetch" markers remain in the DB and are recovered on restart.
func TestCodeQueue_PersistsMarkersAcrossRestart(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// First run: add a handful of hashes and do not finalize or consume.
	q1, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	num := 5
	inputs := make([]common.Hash, 0, num)
	for i := 0; i < num; i++ {
		inputs = append(inputs, crypto.Keccak256Hash([]byte(fmt.Sprintf("persist-%d", i))))
	}
	require.NoError(t, q1.AddCode(inputs))

	// Assert markers exist in the DB.
	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	seen := make(map[common.Hash]struct{}, num)
	for it.Next() {
		h := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])
		seen[h] = struct{}{}
	}
	require.NoError(t, it.Error())

	for _, h := range inputs {
		if _, ok := seen[h]; !ok {
			t.Fatalf("missing marker for hash %v", h)
		}
	}

	// Restart: construct a new queue over the same DB. It should recover
	// the outstanding markers and enqueue them on Init.
	//
	// NOTE: This actually simulates a restart at the component level by discarding the first
	// CodeRequestQueue (which holds all in-memory state: channels, outstanding, etc.)
	// and constructing a new one over the same DB handle. Given that the DB is the single source
	// of truth for the "to-fetch" markers, the new queue immediately recovers the markers and
	// enqueues them on Init.
	q2, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	recovered := make(map[common.Hash]struct{}, num)
	for i := 0; i < num; i++ {
		recovered[<-q2.CodeHashes()] = struct{}{}
	}

	for _, h := range inputs {
		if _, ok := recovered[h]; !ok {
			t.Fatalf("missing recovered hash %v", h)
		}
	}

	q2.Finalize()
	_, ok := <-q2.CodeHashes()
	require.False(t, ok)
}

// Early quit closes the output channel and Finalize reports early-exit error.
func TestCodeQueue_EarlyQuitClosesOutput(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	quit := make(chan struct{})
	q, err := NewCodeQueue(db, quit)
	require.NoError(t, err)

	// Trigger early shutdown before Finalize.
	close(quit)

	// CodeHashes channel should close eventually; non-blocking poll until closed or timeout.
	done := make(chan struct{})
	go func() {
		// Drain until channel closes to observe closure on early quit.
		for range q.CodeHashes() {
		}
		close(done)
	}()

	select {
	case <-done:
		// proceed
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for CodeHashes channel to close on early quit")
	}

	// Finalize should report early-exit error since quit already closed.
	err = q.Finalize()
	require.ErrorIs(t, err, errFailedToFinalizeCodeQueue)

	// AddCode should fail after shutdown.
	codeBytes := utils.RandomBytes(8)
	h := crypto.Keccak256Hash(codeBytes)
	err = q.AddCode([]common.Hash{h})
	require.ErrorIs(t, err, errFailedToAddCodeHashesToQueue)
}

// Clean Finalize closes the output channel and rejects further AddCode calls appropriately.
func TestCodeQueue_CleanFinalizeClosesOutput(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	q, err := NewCodeQueue(db, make(chan struct{}))
	require.NoError(t, err)

	require.NoError(t, q.Finalize())
	_, ok := <-q.CodeHashes()
	require.False(t, ok)
}
