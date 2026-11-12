// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/ava-labs/firewood-go/ffi"
	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database/merkle/sync"
	"github.com/ava-labs/avalanchego/database/merkle/sync/protoutils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/peertracker"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	defaultSimultaneousWorkLimit = 8

	initialRetryWait = 10 * time.Millisecond
	maxRetryWait     = time.Second
	retryWaitFactor  = 1.5
	logInterval      = time.Minute
)

var (
	_ sync.DB[*RangeProof, struct{}] = (*database)(nil)

	errTooManyBytes      = errors.New("response contains more than requested bytes")
	errNotRangeProof     = errors.New("response is not a range proof")
	errInvalidTaskLimits = errors.New("simultaneous work limit out of range")
)

// database wraps a Firewood [ffi.Database] to implement the xsync.DB interface,
// which the proof server needs.
type database struct {
	db *ffi.Database
}

type Config struct {
	SimultaneousWorkLimit int
	Log                   logging.Logger
	StateSyncNodes        []ids.NodeID
	Registerer            prometheus.Registerer
}

// Syncer syncs a Firewood database to a fixed target root. Firewood decides
// which key ranges to fetch and verifies and commits every proof; Syncer only
// fetches the range proofs Firewood asks for from peers.
type Syncer struct {
	config     Config
	db         *ffi.Database
	targetRoot ids.ID
	client     *peertracker.Client
	metrics    *metrics

	// onAccepted runs once Firewood has committed a proof, with the proof's
	// serialized bytes. A non-nil error aborts the sync.
	onAccepted func(proof []byte) error

	nodeIdx atomic.Uint32
}

func New(
	config Config,
	db *ffi.Database,
	targetRoot ids.ID,
	proofClient *peertracker.Client,
) (*Syncer, error) {
	return newSyncer(config, db, targetRoot, proofClient, nil)
}

func newSyncer(
	config Config,
	db *ffi.Database,
	targetRoot ids.ID,
	proofClient *peertracker.Client,
	onAccepted func([]byte) error,
) (*Syncer, error) {
	if config.Registerer == nil {
		config.Registerer = prometheus.NewRegistry()
	}
	if config.Log == nil {
		config.Log = logging.NoLog{}
	}
	if config.SimultaneousWorkLimit == 0 {
		config.SimultaneousWorkLimit = defaultSimultaneousWorkLimit
	}
	if config.SimultaneousWorkLimit < 0 || config.SimultaneousWorkLimit > math.MaxUint32 {
		return nil, fmt.Errorf("%w: %d", errInvalidTaskLimits, config.SimultaneousWorkLimit)
	}
	if proofClient == nil {
		return nil, sync.ErrNoProofClientProvided
	}
	m, err := newMetrics(config.Registerer)
	if err != nil {
		return nil, err
	}
	return &Syncer{
		config:     config,
		db:         db,
		targetRoot: targetRoot,
		client:     proofClient,
		metrics:    m,
		onAccepted: onAccepted,
	}, nil
}

// Sync blocks until the database matches the target root, a fatal error
// occurs, or ctx is cancelled.
func (s *Syncer) Sync(ctx context.Context) error {
	start := time.Now()
	s.config.Log.Info("starting firewood sync",
		zap.Stringer("target root", s.targetRoot),
		zap.Int("workers", s.config.SimultaneousWorkLimit),
	)

	// Firewood can't sync a non-empty database down to the empty trie.
	if s.targetRoot == ids.Empty || s.targetRoot == ids.ID(types.EmptyRootHash) {
		if _, err := s.db.Update([]ffi.BatchOp{ffi.PrefixDelete([]byte{})}); err != nil {
			return err
		}
		s.config.Log.Info("completed firewood sync to empty root")
		return nil
	}

	fwSync, err := s.db.StartSync(ffi.Hash(s.targetRoot), ffi.WithTaskLimit(uint32(s.config.SimultaneousWorkLimit)))
	if err != nil {
		return fmt.Errorf("starting firewood sync: %w", err)
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	logDone := make(chan struct{})
	go func() {
		defer close(logDone)
		s.logProgress(ctx, start)
	}()

	runErr := fwSync.Run(ctx, s.fetch(cancel))
	// An onAccepted failure cancels ctx with its cause, but Run may still
	// return nil if the sync completed concurrently.
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
		runErr = cause
	}
	cancel(nil)
	<-logDone
	if runErr != nil {
		return errors.Join(runErr, fwSync.Drop())
	}

	root, err := fwSync.Finish()
	if err != nil {
		return err
	}
	if ids.ID(root) != s.targetRoot {
		return fmt.Errorf("%w: expected %s, got %s", sync.ErrFinishedWithUnexpectedRoot, s.targetRoot, ids.ID(root))
	}
	s.config.Log.Info("completed firewood sync",
		zap.Stringer("root", s.targetRoot),
		zap.Duration("duration", time.Since(start)),
		zap.Uint64("proofs", s.metrics.accepted.Load()),
		zap.Uint64("proofBytes", s.metrics.acceptedBytes.Load()),
	)
	return nil
}

func (s *Syncer) logProgress(ctx context.Context, start time.Time) {
	ticker := time.NewTicker(logInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.config.Log.Info("firewood sync progress",
				zap.Duration("elapsed", time.Since(start)),
				zap.Uint64("proofs", s.metrics.accepted.Load()),
				zap.Uint64("proofBytes", s.metrics.acceptedBytes.Load()),
				zap.Uint64("rejectedProofs", s.metrics.rejected.Load()),
			)
		}
	}
}

// fetch returns the [ffi.FetchFunc] for one sync. abort cancels the sync with
// a cause, for failures discovered after a proof is accepted.
func (s *Syncer) fetch(abort context.CancelCauseFunc) ffi.FetchFunc {
	return func(ctx context.Context, startKey, endKey ffi.Maybe[[]byte], attempt int, prevErr error) ([]byte, func(error), error) {
		if prevErr != nil {
			s.config.Log.Debug("refetching range after rejected proof",
				zap.Int("attempt", attempt),
				zap.Error(prevErr),
			)
		}

		request := &pb.ProofRequest{
			Request: &pb.ProofRequest_RangeProof{
				RangeProof: &pb.RangeProofRequest{
					RootHash:   s.targetRoot[:],
					StartKey:   protoutils.MaybeToProto(toMaybe(startKey)),
					EndKey:     protoutils.MaybeToProto(toMaybe(endKey)),
					KeyLimit:   sync.DefaultRequestKeyLimit,
					BytesLimit: sync.DefaultRequestByteSizeLimit,
				},
			},
		}
		requestBytes, err := proto.Marshal(request)
		if err != nil {
			return nil, nil, err
		}

		wait := initialRetryWait
		for {
			proof, verdict, err := s.request(ctx, requestBytes)
			if err != nil {
				return nil, nil, err
			}
			if proof != nil {
				return proof, s.report(abort, proof, verdict), nil
			}

			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(wait):
			}
			wait = min(time.Duration(float64(wait)*retryWaitFactor), maxRetryWait)
		}
	}
}

// report builds the per-proof callback the pool calls once Firewood has
// judged the proof. It releases the peer's response callback with the verdict
// so the peer is scored, then runs onAccepted for accepted proofs.
func (s *Syncer) report(abort context.CancelCauseFunc, proof []byte, verdict chan<- error) func(error) {
	fetched := time.Now()
	return func(err error) {
		s.metrics.submitSeconds.Observe(time.Since(fetched).Seconds())
		verdict <- err
		if err != nil {
			s.metrics.rejected.Add(1)
			s.metrics.proofsRejected.Inc()
			return
		}
		s.metrics.accepted.Add(1)
		s.metrics.acceptedBytes.Add(uint64(len(proof)))
		s.metrics.proofsAccepted.Inc()
		s.metrics.proofBytes.Add(float64(len(proof)))
		if s.onAccepted != nil {
			if err := s.onAccepted(proof); err != nil {
				abort(err)
			}
		}
	}
}

// request sends one range-proof request and waits for the response. It
// returns a nil proof when the response is unusable and should be retried.
//
// For a usable response, the peer's response callback stays blocked until
// the returned verdict channel receives Firewood's judgement, so a peer that
// serves an invalid proof is scored as a failure.
func (s *Syncer) request(ctx context.Context, requestBytes []byte) ([]byte, chan<- error, error) {
	responses := make(chan []byte, 1)
	verdict := make(chan error, 1)
	onResponse := func(_ context.Context, nodeID ids.NodeID, responseBytes []byte, appErr error) bool {
		proof, err := parseResponse(responseBytes, appErr)
		if err != nil {
			s.metrics.requestsFailed.Inc()
			s.config.Log.Debug("dropping proof response", zap.Stringer("nodeID", nodeID), zap.Error(err))
			responses <- nil
			return false
		}
		s.metrics.requestsSucceeded.Inc()
		responses <- proof
		select {
		case err := <-verdict:
			return err == nil
		case <-ctx.Done():
			// Our own shutdown is not the peer's fault.
			return true
		}
	}

	if err := s.send(ctx, requestBytes, onResponse); err != nil {
		return nil, nil, err
	}
	s.metrics.requestsMade.Inc()

	select {
	case proof := <-responses:
		return proof, verdict, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (s *Syncer) send(ctx context.Context, requestBytes []byte, onResponse peertracker.ResponseVerifier) error {
	if len(s.config.StateSyncNodes) == 0 {
		return s.client.AppRequestAny(ctx, requestBytes, onResponse)
	}
	// Rotate through the configured nodes so retries go to a different peer.
	idx := s.nodeIdx.Add(1)
	nodeID := s.config.StateSyncNodes[idx%uint32(len(s.config.StateSyncNodes))]
	return s.client.AppRequest(ctx, set.Of(nodeID), requestBytes, onResponse)
}

func parseResponse(responseBytes []byte, appErr error) ([]byte, error) {
	if appErr != nil {
		return nil, appErr
	}
	if len(responseBytes) > sync.DefaultRequestByteSizeLimit {
		return nil, fmt.Errorf("%w: %d > %d", errTooManyBytes, len(responseBytes), sync.DefaultRequestByteSizeLimit)
	}
	var response pb.ProofResponse
	if err := proto.Unmarshal(responseBytes, &response); err != nil {
		return nil, err
	}
	proof := response.GetRangeProof()
	if proof == nil {
		return nil, errNotRangeProof
	}
	return proof, nil
}

func toMaybe(m ffi.Maybe[[]byte]) maybe.Maybe[[]byte] {
	if m == nil || !m.HasValue() {
		return maybe.Nothing[[]byte]()
	}
	return maybe.Some(m.Value())
}

func (db *database) GetMerkleRoot(context.Context) (ids.ID, error) {
	return ids.ID(db.db.Root()), nil
}

func (db *database) GetRangeProofAtRoot(_ context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error) {
	proof, err := db.db.RangeProof(ffi.Hash(rootID), start, end, uint32(maxLength))
	if err != nil {
		return nil, err
	}

	return &RangeProof{
		rp: proof,
	}, nil
}

func (*database) VerifyRangeProof(_ context.Context, proof *RangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	// Extra data must be provided at commit time.
	// TODO: remove this once the FFI no longer requires it.
	proof.root = expectedEndRootID
	proof.maxLength = maxLength

	return proof.rp.Verify(ffi.Hash(expectedEndRootID), start, end, uint32(maxLength))
}

func (db *database) CommitRangeProof(_ context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error) {
	_, err := db.db.VerifyAndCommitRangeProof(proof.rp, start, end, ffi.Hash(proof.root), uint32(proof.maxLength))
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	nextKeyRange, err := proof.rp.FindNextKey()
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}
	// No error indicates the range is complete.
	if nextKeyRange == nil {
		return maybe.Nothing[[]byte](), nil
	}

	return maybe.Some(nextKeyRange.StartKey), nil
}

// TODO: Use change proofs to optimize syncing.
// Returning the sentinel error suggests to the server handler to serve a full range proof instead.
func (*database) GetChangeProof(context.Context, ids.ID, ids.ID, maybe.Maybe[[]byte], maybe.Maybe[[]byte], int) (struct{}, error) {
	return struct{}{}, sync.ErrInsufficientHistory
}

// TODO: implement this method.
func (*database) VerifyChangeProof(context.Context, struct{}, maybe.Maybe[[]byte], maybe.Maybe[[]byte], ids.ID, int) error {
	return errors.New("change proofs are not implemented")
}

// TODO: implement this method.
func (*database) CommitChangeProof(context.Context, maybe.Maybe[[]byte], struct{}) (maybe.Maybe[[]byte], error) {
	return maybe.Nothing[[]byte](), errors.New("change proofs are not implemented")
}

func (db *database) Clear() error {
	// Prefix delete key of length 0.
	_, err := db.db.Update([]ffi.BatchOp{ffi.PrefixDelete([]byte{})})
	return err
}
