// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leaf

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
)

// recordingTask records the leaves handed to it.
type recordingTask struct {
	root     common.Hash
	keys     [][]byte
	finished int
}

func (r *recordingTask) Root() common.Hash  { return r.root }
func (*recordingTask) Account() common.Hash { return common.Hash{} }
func (*recordingTask) Start() []byte        { return nil }
func (*recordingTask) End() []byte          { return nil }

func (r *recordingTask) OnLeaves(_ context.Context, leaves evmstate.Leaves) error {
	r.keys = append(r.keys, leaves.Keys...)
	return nil
}

func (r *recordingTask) OnFinish(context.Context) error {
	r.finished++
	return nil
}

// moreWithoutKeysFetcher reports leaves remaining but returns none, which would
// advance the range nowhere and loop forever.
type moreWithoutKeysFetcher struct{ calls int }

func (f *moreWithoutKeysFetcher) FetchLeaves(context.Context, evmstate.LeafRange) (evmstate.Leaves, bool, error) {
	f.calls++
	return evmstate.Leaves{}, true, nil
}

// runLeafTask drives one Task through a single worker.
func runLeafTask(t *testing.T, ctx context.Context, fetcher types.LeafFetcher, tk Task, opts ...Option) error {
	t.Helper()
	tasks := make(chan Task, 1)
	tasks <- tk
	close(tasks)

	// A fresh slice, because the caller's may be shared across parallel subtests.
	opts = append([]Option{WithNumWorkers(1)}, opts...)
	return NewSyncer(fetcher, tasks, opts...).Sync(ctx)
}

func TestLeafFetch_Batching(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		numKeys      int
		wantRequests int
	}{
		{name: "single_batch", numKeys: 50, wantRequests: 1},
		{name: "exact_limit", numKeys: defaultRequestSize, wantRequests: 1},
		{name: "multiple_batches", numKeys: defaultRequestSize + 50, wantRequests: 2},
		{name: "many_batches", numKeys: 5000, wantRequests: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			trieDB := synctest.NewTrieDB()
			root, keys, _ := synctest.FillTrie(t, trieDB, tt.numKeys)
			recorder := synctest.RecordLeaves(t, ctx, trieDB)

			tk := &recordingTask{root: root}
			require.NoError(t, runLeafTask(t, ctx, recorder, tk))

			require.Len(t, recorder.Requests(), tt.wantRequests)
			require.Equal(t, keys, tk.keys, "every leaf must be fetched in key order")
			require.Equal(t, 1, tk.finished, "the Task must finish exactly once")
		})
	}
}

func TestLeafFetch_MoreWithoutKeys(t *testing.T) {
	t.Parallel()
	fetcher := &moreWithoutKeysFetcher{}

	err := runLeafTask(t, t.Context(), fetcher, &recordingTask{})

	require.ErrorIs(t, err, ErrMoreWithoutKeys)
	require.Equal(t, 1, fetcher.calls, "must stop at the first offending response")
}

func TestLeafFetch_ContextCancelled(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 10)

	ctx, cancel := context.WithCancel(t.Context())
	fetcher := synctest.ServeLeaves(t, ctx, trieDB)
	cancel()

	require.ErrorIs(t, runLeafTask(t, ctx, fetcher, &recordingTask{root: root}), context.Canceled)
}

// limitFetcher records the Limit of every range it is asked for.
type limitFetcher struct{ limits []uint16 }

func (f *limitFetcher) FetchLeaves(_ context.Context, req evmstate.LeafRange) (evmstate.Leaves, bool, error) {
	f.limits = append(f.limits, req.Limit)
	return evmstate.Leaves{Keys: [][]byte{{1}}, Vals: [][]byte{{1}}}, false, nil
}

func TestLeafFetch_RequestSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts []Option
		want uint16
	}{
		{name: "defaults", want: defaultRequestSize},
		{name: "overridden", opts: []Option{WithRequestSize(7)}, want: 7},
		{name: "zero_keeps_default", opts: []Option{WithRequestSize(0)}, want: defaultRequestSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fetcher := &limitFetcher{}
			require.NoError(t, runLeafTask(t, t.Context(), fetcher, &recordingTask{}, tt.opts...))
			require.Equal(t, []uint16{tt.want}, fetcher.limits)
		})
	}
}
