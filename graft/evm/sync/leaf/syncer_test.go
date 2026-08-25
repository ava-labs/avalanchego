// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leaf

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
)

var (
	errFetch = errors.New("peer unreachable")
	errStart = errors.New("task refused to start")
)

// syncTask reads neither the task channel nor the worker count.
func newTestSyncer(f Fetcher) *CallbackSyncer {
	return NewCallbackSyncer(f, nil, &SyncerConfig{RequestSize: 16})
}

// batchFetcher serves the same batch twice, more set on the first.
type batchFetcher struct {
	keys, vals [][]byte
	err        error
	starts     [][]byte
}

func (f *batchFetcher) FetchLeaves(_ context.Context, req evmstate.LeafRange) (evmstate.Leaves, bool, error) {
	f.starts = append(f.starts, common.CopyBytes(req.Start))
	if f.err != nil {
		return evmstate.Leaves{}, false, f.err
	}
	return evmstate.Leaves{
		Keys: f.keys,
		Vals: f.vals,
	}, len(f.starts) == 1, nil
}

type recordingTask struct {
	end      []byte
	skip     bool
	startErr error

	gotKeys  [][]byte
	gotVals  [][]byte
	finished bool
}

func (*recordingTask) Root() common.Hash        { return common.Hash{0x01} }
func (*recordingTask) Account() common.Hash     { return common.Hash{} }
func (*recordingTask) Start() []byte            { return []byte{0x00} }
func (t *recordingTask) End() []byte            { return t.end }
func (t *recordingTask) OnStart() (bool, error) { return t.skip, t.startErr }

func (t *recordingTask) OnFinish(context.Context) error {
	t.finished = true
	return nil
}

func (t *recordingTask) OnLeafs(_ context.Context, keys, vals [][]byte) error {
	t.gotKeys = append(t.gotKeys, keys...)
	t.gotVals = append(t.gotVals, vals...)
	return nil
}

// mutatingTask retains and increments the last key, as [evmstate.trieSegment] does.
type mutatingTask struct {
	recordingTask
	pos []byte
}

func (t *mutatingTask) OnLeafs(_ context.Context, keys, _ [][]byte) error {
	if len(keys) > 0 {
		t.pos = keys[len(keys)-1]
		utils.IncrOne(t.pos)
	}
	return nil
}

func TestSyncTaskAdvancesOnePastLastKey(t *testing.T) {
	t.Parallel()

	fetcher := &batchFetcher{
		keys: [][]byte{{0x01}, {0x02}},
		vals: [][]byte{{0x0a}, {0x0b}},
	}

	require.NoError(t, newTestSyncer(fetcher).syncTask(t.Context(), &mutatingTask{}))
	require.Equal(t, [][]byte{{0x00}, {0x03}}, fetcher.starts)
}

// Only a segmented trie has a non-nil End, so nothing else reaches this path.
func TestSyncTaskTruncatesAtEnd(t *testing.T) {
	t.Parallel()

	var (
		keys = [][]byte{{0x01}, {0x02}, {0x03}, {0x04}}
		vals = [][]byte{{0x0a}, {0x0b}, {0x0c}, {0x0d}}
	)

	tests := []struct {
		name      string
		end       []byte
		wantKeys  [][]byte
		wantVals  [][]byte
		wantCalls int
	}{
		{
			name:      "cuts_mid_batch",
			end:       []byte{0x02},
			wantKeys:  [][]byte{{0x01}, {0x02}},
			wantVals:  [][]byte{{0x0a}, {0x0b}},
			wantCalls: 1,
		},
		{
			name:      "cuts_every_key",
			end:       []byte{0x00},
			wantCalls: 1,
		},
		{
			name:      "keeps_the_key_equal_to_end",
			end:       []byte{0x04},
			wantKeys:  slices.Concat(keys, keys),
			wantVals:  slices.Concat(vals, vals),
			wantCalls: 2,
		},
		{
			name:      "cuts_nothing_and_keeps_going",
			end:       []byte{0xff},
			wantKeys:  slices.Concat(keys, keys),
			wantVals:  slices.Concat(vals, vals),
			wantCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fetcher := &batchFetcher{
				keys: keys,
				vals: vals,
			}
			task := &recordingTask{end: tt.end}

			require.NoError(t, newTestSyncer(fetcher).syncTask(t.Context(), task))
			require.Equal(t, tt.wantKeys, task.gotKeys)
			require.Equal(t, tt.wantVals, task.gotVals)
			require.Len(t, fetcher.starts, tt.wantCalls)
			require.True(t, task.finished)
		})
	}
}

func TestSyncTaskRejectsMoreWithoutKeys(t *testing.T) {
	t.Parallel()

	err := newTestSyncer(&batchFetcher{}).syncTask(t.Context(), &recordingTask{})
	require.ErrorIs(t, err, ErrMoreWithoutKeys)
}

// The atomic syncer matches this sentinel to detect an interrupted sync.
func TestSyncTaskWrapsFetchError(t *testing.T) {
	t.Parallel()

	err := newTestSyncer(&batchFetcher{err: errFetch}).syncTask(t.Context(), &recordingTask{})
	require.ErrorIs(t, err, ErrFailedToFetchLeafs)
	require.ErrorIs(t, err, errFetch)
}

func TestSyncTaskOnStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		skip     bool
		startErr error
		wantErr  error
	}{
		{
			name: "skip_completes_the_task",
			skip: true,
		},
		{
			name:     "error_stops_the_task",
			startErr: errStart,
			wantErr:  errStart,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fetcher := &batchFetcher{
				keys: [][]byte{{0x01}},
				vals: [][]byte{{0x0a}},
			}
			task := &recordingTask{
				skip:     tt.skip,
				startErr: tt.startErr,
			}

			require.ErrorIs(t, newTestSyncer(fetcher).syncTask(t.Context(), task), tt.wantErr)
			require.Empty(t, fetcher.starts)
			require.Empty(t, task.gotKeys)
			require.False(t, task.finished)
		})
	}
}

func TestSyncTaskStopsOnCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	fetcher := &batchFetcher{
		keys: [][]byte{{0x01}},
		vals: [][]byte{{0x0a}},
	}
	require.ErrorIs(t, newTestSyncer(fetcher).syncTask(ctx, &recordingTask{}), context.Canceled)
	require.Empty(t, fetcher.starts)
}
