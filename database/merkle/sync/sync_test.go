// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var _ Marshaler[struct{}] = marshaler{}

type marshaler struct{}

func (marshaler) Marshal(struct{}) ([]byte, error) {
	return []byte{1}, nil
}

func (marshaler) Unmarshal([]byte) (struct{}, error) {
	return struct{}{}, nil
}

var _ DB[struct{}, struct{}] = (*db)(nil)

type db struct {
	id ids.ID
}

func (db *db) GetMerkleRoot(context.Context) (ids.ID, error) {
	return db.id, nil
}

func (*db) Clear() error {
	return nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) CommitChangeProof(ctx context.Context, end maybe.Maybe[[]byte], proof struct{}) (maybe.Maybe[[]byte], error) {
	return maybe.Nothing[[]byte](), nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) CommitRangeProof(ctx context.Context, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], proof struct{}) (maybe.Maybe[[]byte], error) {
	return maybe.Nothing[[]byte](), nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) GetChangeProof(ctx context.Context, startRootID ids.ID, endRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (struct{}, error) {
	return struct{}{}, nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) GetRangeProofAtRoot(ctx context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (struct{}, error) {
	return struct{}{}, nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) VerifyChangeProof(ctx context.Context, proof struct{}, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	return nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) VerifyRangeProof(ctx context.Context, proof struct{}, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	return nil
}

func Test_Sync_BusyContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	clientDB := &db{id: ids.Empty}

	// Ensure the single thread doing work is blocked.
	ch := make(chan struct{})
	defer close(ch)
	blockingHandler := p2p.TestHandler{
		AppRequestF: func(_ context.Context, _ ids.NodeID, _ time.Time, _ []byte) ([]byte, *common.AppError) {
			cancel()
			<-ch
			return nil, nil
		},
	}

	syncer, err := NewSyncer(
		clientDB,
		Config[struct{}, struct{}]{
			TargetRoot:            ids.GenerateTestID(), // must be different from clientDB's root and [ids.Empty]
			RangeProofMarshaler:   marshaler{},
			ChangeProofMarshaler:  marshaler{},
			RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, blockingHandler),
			ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, blockingHandler),
			Log:                   logging.NoLog{},
			SimultaneousWorkLimit: 1, // ensures synchronous event handling
		},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	err = syncer.Sync(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func Test_Midpoint(t *testing.T) {
	require := require.New(t)

	mid := midPoint(maybe.Some([]byte{1, 255}), maybe.Some([]byte{2, 1}))
	require.Equal(maybe.Some([]byte{2, 0}), mid)

	mid = midPoint(maybe.Nothing[[]byte](), maybe.Some([]byte{255, 255, 0}))
	require.Equal(maybe.Some([]byte{127, 255, 128}), mid)

	mid = midPoint(maybe.Some([]byte{255, 255, 255}), maybe.Some([]byte{255, 255}))
	require.Equal(maybe.Some([]byte{255, 255, 127, 128}), mid)

	mid = midPoint(maybe.Nothing[[]byte](), maybe.Some([]byte{255}))
	require.Equal(maybe.Some([]byte{127, 127}), mid)

	mid = midPoint(maybe.Some([]byte{1, 255}), maybe.Some([]byte{255, 1}))
	require.Equal(maybe.Some([]byte{128, 128}), mid)

	mid = midPoint(maybe.Some([]byte{140, 255}), maybe.Some([]byte{141, 0}))
	require.Equal(maybe.Some([]byte{140, 255, 127}), mid)

	mid = midPoint(maybe.Some([]byte{126, 255}), maybe.Some([]byte{127}))
	require.Equal(maybe.Some([]byte{126, 255, 127}), mid)

	mid = midPoint(maybe.Nothing[[]byte](), maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{127}), mid)

	low := midPoint(maybe.Nothing[[]byte](), mid)
	require.Equal(maybe.Some([]byte{63, 127}), low)

	high := midPoint(mid, maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{191}), high)

	mid = midPoint(maybe.Some([]byte{255, 255}), maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{255, 255, 127, 127}), mid)

	mid = midPoint(maybe.Some([]byte{255}), maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{255, 127, 127}), mid)

	for i := 0; i < 5000; i++ {
		r := rand.New(rand.NewSource(int64(i)))

		start := make([]byte, r.Intn(99)+1)
		_, err := r.Read(start)
		require.NoError(err)

		end := make([]byte, r.Intn(99)+1)
		_, err = r.Read(end)
		require.NoError(err)

		for bytes.Equal(start, end) {
			_, err = r.Read(end)
			require.NoError(err)
		}

		if bytes.Compare(start, end) == 1 {
			start, end = end, start
		}

		mid = midPoint(maybe.Some(start), maybe.Some(end))
		require.Equal(-1, bytes.Compare(start, mid.Value()))
		require.Equal(-1, bytes.Compare(mid.Value(), end))
	}
}
