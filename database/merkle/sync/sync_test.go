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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type marshaler struct{}

type proofDouble struct {
	newRoot ids.ID
}

var _ Marshaler[*proofDouble] = marshaler{}

func (marshaler) Marshal(p *proofDouble) ([]byte, error) {
	return p.newRoot[:], nil
}

func (marshaler) Unmarshal(b []byte) (*proofDouble, error) {
	newRoot, err := ids.ToID(b)
	if err != nil {
		return nil, err
	}
	return &proofDouble{newRoot: newRoot}, nil
}

type db struct {
	id ids.ID
}

var _ DB[*proofDouble, *proofDouble] = (*db)(nil)

func (db *db) GetMerkleRoot(context.Context) (ids.ID, error) {
	return db.id, nil
}

func (*db) Clear() error {
	return nil
}

func (db *db) CommitChangeProof(ctx context.Context, end maybe.Maybe[[]byte], proof *proofDouble) (maybe.Maybe[[]byte], error) {
	if proof.newRoot == ids.Empty {
		return midPoint(end, maybe.Nothing[[]byte]()), nil
	}

	db.id = proof.newRoot
	return maybe.Nothing[[]byte](), nil
}

func (db *db) CommitRangeProof(ctx context.Context, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], proof *proofDouble) (maybe.Maybe[[]byte], error) {
	if proof.newRoot == ids.Empty {
		return midPoint(start, end), nil
	}

	db.id = proof.newRoot
	return maybe.Nothing[[]byte](), nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) GetChangeProof(ctx context.Context, startRootID ids.ID, endRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*proofDouble, error) {
	return &proofDouble{}, nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) GetRangeProofAtRoot(ctx context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*proofDouble, error) {
	return &proofDouble{}, nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) VerifyChangeProof(ctx context.Context, proof *proofDouble, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	return nil
}

//nolint:revive // unused parameter clarifies method signature
func (*db) VerifyRangeProof(ctx context.Context, proof *proofDouble, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	return nil
}

func marshalRangeProofResponse(t *testing.T, p *proofDouble) []byte {
	t.Helper()

	inner, err := marshaler{}.Marshal(p)
	require.NoError(t, err)
	b, err := proto.Marshal(&pb.ProofResponse{
		Response: &pb.ProofResponse_RangeProof{RangeProof: inner},
	})
	require.NoError(t, err)
	return b
}

func marshalChangeProofResponse(t *testing.T, p *proofDouble) []byte {
	t.Helper()

	inner, err := marshaler{}.Marshal(p)
	require.NoError(t, err)
	b, err := proto.Marshal(&pb.ProofResponse{
		Response: &pb.ProofResponse_ChangeProof{ChangeProof: inner},
	})
	require.NoError(t, err)
	return b
}

func marshalNilProofResponse(t *testing.T, _ *proofDouble) []byte {
	t.Helper()
	b, err := proto.Marshal(&pb.ProofResponse{})
	require.NoError(t, err)
	return b
}

func Test_Sync_RangeProofRequest(t *testing.T) {
	tests := []struct {
		name               string
		createTestResponse func(t *testing.T, p *proofDouble) []byte
		expectRetried      bool
	}{
		{
			name:               "range_proof_response",
			createTestResponse: marshalRangeProofResponse,
		},
		{
			name:               "change_proof_response",
			createTestResponse: marshalChangeProofResponse,
			expectRetried:      true,
		},
		{
			name:               "nil_response",
			createTestResponse: marshalNilProofResponse,
			expectRetried:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			targetRoot := ids.GenerateTestID()
			clientDB := &db{id: ids.Empty}

			var (
				alreadySent bool
				retried     bool
			)
			testResponse := tt.createTestResponse(t, &proofDouble{newRoot: targetRoot})
			finishingResponse := marshalRangeProofResponse(t, &proofDouble{newRoot: targetRoot})
			handler := p2p.TestHandler{
				AppRequestF: func(_ context.Context, _ ids.NodeID, _ time.Time, _ []byte) ([]byte, *common.AppError) {
					if alreadySent {
						// the first request must have failed.
						retried = true
						return finishingResponse, nil
					}
					alreadySent = true
					return testResponse, nil
				},
			}

			syncer, err := NewSyncer(
				clientDB,
				Config[*proofDouble, *proofDouble]{
					TargetRoot:            targetRoot,
					RangeProofMarshaler:   marshaler{},
					ChangeProofMarshaler:  marshaler{},
					ProofClient:           p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, handler),
					Log:                   logging.NoLog{},
					SimultaneousWorkLimit: 1,
				},
				prometheus.NewRegistry(),
			)
			require.NoError(t, err)
			require.NoErrorf(t, syncer.Sync(ctx), "%T.Sync()", syncer)
			require.Equal(t, tt.expectRetried, retried, "retry behavior mismatch")
		})
	}
}

func Test_Sync_ChangeProofRequest(t *testing.T) {
	tests := []struct {
		name               string
		createTestResponse func(t *testing.T, p *proofDouble) []byte
		expectRetried      bool
	}{
		{
			name:               "change_proof_response",
			createTestResponse: marshalChangeProofResponse,
		},
		{
			name:               "range_proof_response",
			createTestResponse: marshalRangeProofResponse,
		},
		{
			name:               "nil_response",
			createTestResponse: marshalNilProofResponse,
			expectRetried:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			clientDB := &db{id: ids.Empty}
			originalTarget := ids.GenerateTestID()
			newTarget := ids.GenerateTestID()

			var syncer *Syncer[*proofDouble, *proofDouble]
			callCount := 0
			retried := false
			setupResponse := marshalRangeProofResponse(t, &proofDouble{newRoot: originalTarget}) // enqueues change proof work
			testResponse := tt.createTestResponse(t, &proofDouble{newRoot: newTarget})           // actual test
			finishingResponse := marshalChangeProofResponse(t, &proofDouble{newRoot: newTarget}) // graceful finishing sync
			handler := p2p.TestHandler{
				AppRequestF: func(_ context.Context, _ ids.NodeID, _ time.Time, _ []byte) ([]byte, *common.AppError) {
					callCount++
					switch callCount {
					case 1:
						// The first request is always a range proof. Answer it
						// correctly then update the sync target so the completed work
						// is stale. The syncer re-enqueues the range with
						// originalTarget as localRootID, triggering two new work items.
						// Note: [Syncer.enqueueWork] splits the original work, so that's why it's two work items.
						assert.NoErrorf(t, syncer.UpdateSyncTarget(newTarget), "%T.UpdateSyncTarget()", syncer)
						return setupResponse, nil
					case 2, 3:
						// These requests would normally finish the sync, so we test the response handling here.
						return testResponse, nil
					default:
						// This indicates that the test interceptor didn't finish the sync.
						// Allow correct proofs to finish.
						retried = true
						return finishingResponse, nil
					}
				},
			}

			var err error
			syncer, err = NewSyncer(
				clientDB,
				Config[*proofDouble, *proofDouble]{
					TargetRoot:            originalTarget,
					RangeProofMarshaler:   marshaler{},
					ChangeProofMarshaler:  marshaler{},
					ProofClient:           p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, handler),
					Log:                   logging.NoLog{},
					SimultaneousWorkLimit: 1,
				},
				prometheus.NewRegistry(),
			)
			require.NoError(t, err)

			require.NoErrorf(t, syncer.Sync(ctx), "%T.Sync()", syncer)
			require.Equal(t, tt.expectRetried, retried, "unexpected retry of failed change proof request")
		})
	}
}

func Test_Sync_BusyContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	clientDB := &db{id: ids.Empty}

	// Ensure the single thread doing work is blocked.
	// This will cause the syncer to wait for more incoming work on the cond var,
	// testing a path where the response handler is never called.
	ch := make(chan struct{})
	defer close(ch) // avoid leaking the goroutine
	blockingHandler := p2p.TestHandler{
		AppRequestF: func(_ context.Context, _ ids.NodeID, _ time.Time, _ []byte) ([]byte, *common.AppError) {
			cancel()
			<-ch
			return nil, nil
		},
	}

	syncer, err := NewSyncer(
		clientDB,
		Config[*proofDouble, *proofDouble]{
			TargetRoot:            ids.GenerateTestID(), // must be different from clientDB's root and [ids.Empty]
			RangeProofMarshaler:   marshaler{},
			ChangeProofMarshaler:  marshaler{},
			ProofClient:           p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, blockingHandler),
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

	for i := range 5000 {
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
